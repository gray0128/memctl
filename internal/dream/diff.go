package dream

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// workFile is one regular text file found in the runner's working directory.
type workFile struct {
	content []byte
	fsMode  fs.FileMode
}

// buildChanges compares the exported working tree against the fixed baseline
// and returns the ordered change list plus the bounded unified diff. Every
// out-of-scope path, non-text file, Git configuration change and over-limit
// result is rejected instead of being propagated into a proposal. See
// docs/03-workflows-and-dataflows.md section 6 step 4.
func buildChanges(base *baseline, work map[string]workFile) ([]Change, string, []string, error) {
	paths := make([]string, 0, len(base.files)+len(work))
	seen := make(map[string]struct{}, len(base.files)+len(work))
	for rel := range base.files {
		seen[rel] = struct{}{}
		paths = append(paths, rel)
	}
	for rel := range work {
		if _, ok := seen[rel]; !ok {
			seen[rel] = struct{}{}
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)

	changes := make([]Change, 0, len(paths))
	total := 0
	for _, rel := range paths {
		old, hasOld := base.files[rel]
		_, hasNew := work[rel]
		if !hasNew && !hasOld {
			continue
		}
		if base.skipped[rel] {
			return nil, "", nil, memerr.New(memerr.Unsupported,
				"the proposal changes %q, which is not part of the exported supported memory", rel)
		}
		new, _ := work[rel]

		change := Change{Path: rel}
		switch {
		case hasOld && !hasNew:
			change.Action = ChangeDelete
			change.BaseRevision = old.revision
			change.PlannedRevision = vault.AbsentRevision
			change.Patch = unifiedDiff("a/"+rel, "/dev/null", string(old.content), "")
		case !hasOld && hasNew:
			change.Action = ChangeCreate
			change.BaseRevision = vault.AbsentRevision
			change.PlannedRevision = vault.FileRevision(vault.GitMode(new.fsMode), new.content)
			change.Patch = unifiedDiff("/dev/null", "b/"+rel, "", string(new.content))
		default:
			if bytes.Equal(old.content, new.content) && old.fsMode == new.fsMode {
				continue
			}
			change.Action = ChangeUpdate
			change.BaseRevision = old.revision
			change.PlannedRevision = vault.FileRevision(vault.GitMode(new.fsMode), new.content)
			change.Patch = unifiedDiff("a/"+rel, "b/"+rel, string(old.content), string(new.content))
		}
		changes = append(changes, change)
		if change.Action != ChangeDelete {
			total += len(new.content)
			if total > MaxChangeBytes {
				return nil, "", nil, memerr.New(memerr.LimitExceeded,
					"the proposal content exceeds the %d byte limit", MaxChangeBytes)
			}
		}
		if len(changes) > MaxChanges {
			return nil, "", nil, memerr.New(memerr.LimitExceeded,
				"the proposal changes more than %d files", MaxChanges)
		}
	}

	if len(changes) == 0 {
		return changes, "", []string{"the runner made no changes"}, nil
	}

	var diff strings.Builder
	for _, change := range changes {
		if diff.Len()+len(change.Patch) > MaxPatchBytes {
			diff.WriteString("... diff truncated at the " + fmt.Sprint(MaxPatchBytes) + " byte proposal limit; the ordered change list is complete\n")
			break
		}
		diff.WriteString(change.Patch)
	}

	return changes, diff.String(), linkWarnings(work, changes), nil
}

// linkWarnings parses the changed files against the resulting file set and
// reports every link that does not resolve to exactly one file. It validates
// format and links only; it cannot prove the absence of semantic loss.
func linkWarnings(work map[string]workFile, changes []Change) []string {
	paths := make([]string, 0, len(work))
	for rel := range work {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	index := vault.NewLinkIndex(paths)

	var warnings []string
	for _, change := range changes {
		if change.Action == ChangeDelete {
			continue
		}
		file, ok := work[change.Path]
		if !ok {
			continue
		}
		parsed := vault.ParseFile(change.Path, change.PlannedRevision, file.content)
		_, diagnostics := parsed.ResolveLinks(index)
		for _, diagnostic := range diagnostics {
			warnings = append(warnings, fmt.Sprintf("%s %s:%d [[%s]]",
				diagnostic.Code, diagnostic.SourceFile, diagnostic.LineNumber, diagnostic.RawTarget))
		}
	}
	sort.Strings(warnings)
	return warnings
}

// scanWorkTree walks the exported working tree and returns every regular text
// file. Symbolic links, special files, out-of-scope paths and binary or
// oversized content fail the whole proposal.
func scanWorkTree(workDir string) (map[string]workFile, error) {
	root := repoDir(workDir)
	files := make(map[string]workFile)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return memerr.Wrap(memerr.IOFailed, walkErr, "cannot walk the exported tree")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot resolve %s", path)
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if entry.Type()&fs.ModeSymlink != 0 {
			return memerr.New(memerr.Unsupported,
				"the proposal contains a symbolic link at %q; links are not supported memory", slash)
		}
		if protectedPath(slash) {
			return memerr.New(memerr.Unsupported,
				"the proposal modifies Git or host configuration at %q", slash)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return memerr.New(memerr.Unsupported,
				"the proposal contains a non-regular file at %q", slash)
		}
		if _, err := vault.ValidateRelPath(slash); err != nil {
			return err
		}
		content, mode, err := readWorkFile(path)
		if err != nil {
			return err
		}
		files[slash] = workFile{content: content, fsMode: mode}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// readWorkFile reads one bounded UTF-8 regular file.
func readWorkFile(path string) ([]byte, fs.FileMode, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, 0, memerr.Wrap(memerr.IOFailed, err, "cannot open %s", path)
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, 0, memerr.Wrap(memerr.IOFailed, err, "cannot inspect %s", path)
	}
	content, err := io.ReadAll(io.LimitReader(handle, vault.MaxTextFileBytes+1))
	if err != nil {
		return nil, 0, memerr.Wrap(memerr.IOFailed, err, "cannot read %s", path)
	}
	if len(content) > vault.MaxTextFileBytes {
		return nil, 0, memerr.New(memerr.LimitExceeded,
			"the proposal file %q exceeds the %d byte text limit", path, vault.MaxTextFileBytes)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, 0, memerr.New(memerr.Unsupported,
			"the proposal contains binary content at %q", path)
	}
	return content, info.Mode().Perm(), nil
}

// patchHash binds the ordered change list and every planned revision. Applying
// a proposal must quote exactly this hash, so a modified proposal cannot be
// applied under its original hash. See docs/03-workflows-and-dataflows.md
// section 6 step 6.
func patchHash(changes []Change) string {
	hash := sha256.New()
	writeHashField(hash, []byte("memctl-dream-patch-v1"))
	for _, change := range changes {
		writeHashField(hash, []byte(change.Action))
		writeHashField(hash, []byte(change.Path))
		writeHashField(hash, []byte(change.BaseRevision))
		writeHashField(hash, []byte(change.PlannedRevision))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// writeHashField writes a length-prefixed field so concatenations cannot
// collide by construction.
func writeHashField(w io.Writer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = w.Write(length[:])
	_, _ = w.Write(value)
}
