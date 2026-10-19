package dream

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// workFile is one regular text file of the worker result tree.
type workFile struct {
	content []byte
	fsMode  fs.FileMode
}

// buildChanges compares the worker result tree against the fixed baseline
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
	var modeWarnings []string
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

		// A managed apply preserves the baseline mode and creates files as
		// 100644, so a worker mode change cannot be committed. Normalize it away
		// and warn instead of producing a ready proposal that apply would reject.
		plannedMode := "100644"
		if hasOld {
			plannedMode = old.gitMode
		}
		if hasNew && !hasOld {
			if wanted := vault.GitMode(new.fsMode); wanted != plannedMode {
				modeWarnings = append(modeWarnings, fmt.Sprintf(
					"the proposal marks %s executable; a managed apply creates it as 100644", rel))
			}
		}
		if hasOld && hasNew {
			if wanted := vault.GitMode(new.fsMode); wanted != plannedMode {
				modeWarnings = append(modeWarnings, fmt.Sprintf(
					"the proposal changes the file mode of %s; a managed apply keeps %s", rel, plannedMode))
			}
		}

		if hasOld && !hasNew && rel == vault.EntryPointName {
			return nil, "", nil, memerr.New(memerr.Unsupported,
				"the proposal deletes %s; the entry file must always exist", vault.EntryPointName)
		}
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
			change.PlannedRevision = vault.FileRevision(plannedMode, new.content)
			change.Patch = unifiedDiff("/dev/null", "b/"+rel, "", string(new.content))
		default:
			if bytes.Equal(old.content, new.content) && plannedMode == old.gitMode {
				continue
			}
			change.Action = ChangeUpdate
			change.BaseRevision = old.revision
			change.PlannedRevision = vault.FileRevision(plannedMode, new.content)
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
		return changes, "", append(modeWarnings, "the worker made no changes"), nil
	}

	var diff strings.Builder
	for _, change := range changes {
		if diff.Len()+len(change.Patch) > MaxPatchBytes {
			diff.WriteString("... diff truncated at the " + fmt.Sprint(MaxPatchBytes) + " byte proposal limit; the ordered change list is complete\n")
			break
		}
		diff.WriteString(change.Patch)
	}

	warnings := append(modeWarnings, linkWarnings(work, changes)...)
	return changes, diff.String(), warnings, nil
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
