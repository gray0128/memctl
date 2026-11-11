package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TreeEntry describes one entry in a committed Git tree object.
type TreeEntry struct {
	Mode   string
	Type   string
	Object string
	Size   int64
	Path   string
}

// SnapshotCommit resolves the commit hash of the current branch reference
// without acquiring the repository lock or touching the index. When HEAD is
// detached during an active `git rebase`, it reads the original branch
// reference from `.git/rebase-merge/head-name` or `.git/rebase-apply/head-name`
// so callers always observe the stable pre-rebase branch tip. An unborn
// repository with no commits returns an empty string and no error.
func SnapshotCommit(ctx context.Context, dir, gitDir string) (string, error) {
	if gitDir == "" {
		resolved, err := GitDir(ctx, dir)
		if err != nil {
			return "", err
		}
		gitDir = resolved
	}

	var candidates []string
	addCandidate := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		for _, existing := range candidates {
			if existing == ref {
				return
			}
		}
		candidates = append(candidates, ref)
	}

	for _, rel := range []string{
		filepath.Join("rebase-merge", "head-name"),
		filepath.Join("rebase-apply", "head-name"),
	} {
		data, err := os.ReadFile(filepath.Join(gitDir, rel))
		if err != nil {
			continue
		}
		ref := strings.TrimSpace(string(data))
		if ref == "" {
			continue
		}
		if !strings.HasPrefix(ref, "refs/") {
			ref = "refs/heads/" + ref
		}
		addCandidate(ref)
	}

	if symRef, _, err := runGit(ctx, dir, Options{NonInteractive: true}, "--no-optional-locks", "symbolic-ref", "-q", "HEAD"); err == nil && symRef != "" {
		addCandidate(symRef)
	}

	addCandidate("refs/heads/main")
	addCandidate("HEAD")

	for _, ref := range candidates {
		commit, _, err := runGit(ctx, dir, Options{NonInteractive: true}, "--no-optional-locks", "rev-parse", "--verify", "-q", ref+"^{commit}")
		if err == nil && commit != "" {
			return commit, nil
		}
	}
	return "", nil
}

// LookupTreeEntry inspects one root-relative path in the tree of commit without
// descending into subdirectories. A directory is returned with Type="tree" and
// Mode="040000"; a missing path returns ok=false and no error.
func LookupTreeEntry(ctx context.Context, dir, commit, relPath string) (TreeEntry, bool, error) {
	out, _, err := runGitRaw(ctx, dir, Options{NonInteractive: true}, "--no-optional-locks", "ls-tree", "-l", "-z", "--full-tree", commit, "--", relPath)
	if err != nil {
		return TreeEntry{}, false, err
	}
	for _, entry := range parseTreeEntries(out) {
		if entry.Path == relPath {
			return entry, true, nil
		}
	}
	return TreeEntry{}, false, nil
}

// ListTreeEntries returns all recursive tree entries of commit.
func ListTreeEntries(ctx context.Context, dir, commit string) ([]TreeEntry, error) {
	out, _, err := runGitRaw(ctx, dir, Options{NonInteractive: true}, "--no-optional-locks", "ls-tree", "-r", "-l", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	return parseTreeEntries(out), nil
}

// ReadBlob returns the exact raw bytes of one Git blob object.
func ReadBlob(ctx context.Context, dir, object string) ([]byte, error) {
	out, _, err := runGitRaw(ctx, dir, Options{NonInteractive: true}, "--no-optional-locks", "cat-file", "blob", object)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// ReadBlobBounded returns up to limit raw bytes of one Git blob object.
func ReadBlobBounded(ctx context.Context, dir, object string, limit int) ([]byte, error) {
	out, _, _, err := runGitBounded(ctx, dir, Options{NonInteractive: true}, limit, "--no-optional-locks", "cat-file", "blob", object)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func parseTreeEntries(raw string) []TreeEntry {
	var entries []TreeEntry
	for _, record := range strings.Split(raw, "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		var size int64
		if len(fields) >= 4 && fields[3] != "-" {
			if parsed, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
				size = parsed
			}
		}
		entries = append(entries, TreeEntry{
			Mode:   fields[0],
			Type:   fields[1],
			Object: fields[2],
			Size:   size,
			Path:   path,
		})
	}
	return entries
}
