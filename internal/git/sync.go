package git

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// DefaultRemote picks the remote to synchronize with: origin when present,
// otherwise the first configured remote. It reports whether any exists.
func DefaultRemote(ctx context.Context, dir string) (string, bool, error) {
	names, err := Remotes(ctx, dir)
	if err != nil {
		return "", false, err
	}
	if len(names) == 0 {
		return "", false, nil
	}
	for _, name := range names {
		if name == "origin" {
			return name, true, nil
		}
	}
	return names[0], true, nil
}

// Remotes lists the configured remote names.
func Remotes(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "remote")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var names []string
	for _, name := range strings.Split(out, "\n") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// RemoteURLs returns every fetch and push URL configured for one remote. The
// approval record binds this set so a changed target invalidates it.
func RemoteURLs(ctx context.Context, dir, name string) ([]string, error) {
	urls := map[string]bool{}
	for _, args := range [][]string{
		{"remote", "get-url", "--all", name},
		{"remote", "get-url", "--push", "--all", name},
	} {
		out, err := Run(ctx, dir, args...)
		if err != nil {
			return nil, err
		}
		for _, url := range strings.Split(out, "\n") {
			if url = strings.TrimSpace(url); url != "" {
				urls[url] = true
			}
		}
	}
	result := make([]string, 0, len(urls))
	for url := range urls {
		result = append(result, url)
	}
	sort.Strings(result)
	return result, nil
}

// AddRemote configures a new remote.
func AddRemote(ctx context.Context, dir, name, url string) error {
	_, err := Run(ctx, dir, "remote", "add", name, url)
	return err
}

// RemoveRemote deletes a remote configuration. It rolls back a remote memctl
// added before the connection could be validated.
func RemoveRemote(ctx context.Context, dir, name string) error {
	_, err := Run(ctx, dir, "remote", "remove", name)
	return err
}

// LsRemote returns the refs advertised by a remote or URL. An empty result
// means the remote exists but has no refs; a failure is an access problem and
// must never be read as an empty remote.
func LsRemote(ctx context.Context, dir string, opts Options, target string) (string, error) {
	out, _, err := runGit(ctx, dir, opts, "ls-remote", target)
	return out, err
}

// RemoteDefaultBranch reads the branch the remote advertises as HEAD through
// the symref record. It returns an error when the remote declares none.
func RemoteDefaultBranch(ctx context.Context, dir string, opts Options, target string) (string, error) {
	out, _, err := runGit(ctx, dir, opts, "ls-remote", "--symref", target)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			return strings.TrimPrefix(fields[1], "refs/heads/"), nil
		}
	}
	return "", memerr.New(memerr.NoUpstream,
		"the remote does not advertise a default branch through ls-remote --symref")
}

// Fetch updates remote-tracking refs for one remote with a total deadline.
func Fetch(ctx context.Context, dir string, opts Options, remote string) error {
	_, _, err := runGit(ctx, dir, opts, "fetch", remote)
	return err
}

// FetchRef fetches exactly one remote ref into one local ref. The Dreaming
// result transport uses it so a fetch never rewrites the tracking refs of the
// branches the user is working on. updateRef names a fully qualified local ref.
func FetchRef(ctx context.Context, dir string, opts Options, remote, remoteRef, updateRef string) error {
	_, _, err := runGit(ctx, dir, opts, "fetch", "--no-tags", remote, remoteRef+":"+updateRef)
	return err
}

// PushRef publishes one local object as a remote branch without touching the
// local branch or its upstream. src is a commit hash or any revision.
func PushRef(ctx context.Context, dir string, opts Options, remote, src, remoteRef string) error {
	_, _, err := runGit(ctx, dir, opts, "push", "--no-verify", remote, src+":"+remoteRef)
	return err
}

// DeleteRemoteRef deletes one remote branch. The caller treats a missing ref as
// success, so cleanup after a consumed proposal is idempotent.
func DeleteRemoteRef(ctx context.Context, dir string, opts Options, remote, remoteRef string) error {
	_, _, err := runGit(ctx, dir, opts, "push", "--no-verify", "--delete", remote, remoteRef)
	return err
}

// UpdateRef points one local ref at a revision, or deletes it when rev is
// empty. It is the cleanup counterpart of FetchRef.
func UpdateRef(ctx context.Context, dir, ref, rev string) error {
	if rev == "" {
		_, err := Run(ctx, dir, "update-ref", "-d", ref)
		return err
	}
	_, err := Run(ctx, dir, "update-ref", ref, rev)
	return err
}

// SourceFiles reads the regular non-`.git` files of one commit, as a commit
// object rather than as a checked-out tree. The Dreaming worker materializes
// them in a dependency-free temporary checkout so it never runs a branch
// checkout of untrusted history in the user's repository.
func SourceFiles(ctx context.Context, dir, commit string) (map[string][]byte, error) {
	out, _, err := runGitRaw(ctx, dir, Options{}, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}
		mode, kind, blob := fields[0], fields[1], fields[2]
		// Only regular blobs of the working tree are memory content. A
		// submodule commit and a symbolic link are refused by the caller's
		// range checks instead of being resolved here.
		if kind != "blob" || (mode != "100644" && mode != "100755") {
			continue
		}
		content, _, err := runGitRaw(ctx, dir, Options{}, "cat-file", "blob", blob)
		if err != nil {
			return nil, err
		}
		files[path] = []byte(content)
	}
	return files, nil
}

// CommitMessageOf returns the full message of one commit.
func CommitMessageOf(ctx context.Context, dir, commit string) (string, error) {
	return Run(ctx, dir, "log", "-1", "--format=%B", commit)
}

// RevParseOrEmpty resolves a revision and reports an empty string when it does
// not exist, so "the ref is not there yet" is distinguishable from a repository
// failure.
func RevParseOrEmpty(ctx context.Context, dir, rev string) (string, error) {
	out, _, err := runGit(ctx, dir, Options{NonInteractive: true}, "rev-parse", "--verify", "-q", rev)
	if err != nil {
		return "", nil
	}
	return out, nil
}

// UpstreamRef returns the tracking ref of the current branch ("origin/main")
// and whether one is configured.
func UpstreamRef(ctx context.Context, dir string) (string, bool, error) {
	out, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		if contractErr, ok := memerr.As(err); ok && contractErr.Code == memerr.GitFailed {
			return "", false, nil
		}
		return "", false, err
	}
	ref := strings.TrimSpace(out)
	if ref == "" {
		return "", false, nil
	}
	return ref, true, nil
}

// HeadBranch returns the current branch name and fails on a detached HEAD.
func HeadBranch(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if branch == "" {
		return "", memerr.New(memerr.GitFailed, "HEAD is detached; check out a branch before syncing")
	}
	return branch, nil
}

// AheadBehind counts the commits only the local branch has (ahead) and only
// the upstream has (behind).
func AheadBehind(ctx context.Context, dir, upstream string) (ahead, behind int, err error) {
	out, err := Run(ctx, dir, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, memerr.New(memerr.GitFailed, "unexpected rev-list output %q", out)
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, memerr.New(memerr.GitFailed, "unexpected rev-list output %q", out)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, memerr.New(memerr.GitFailed, "unexpected rev-list output %q", out)
	}
	return ahead, behind, nil
}

// HasMergeCommits reports whether the range contains a merge commit, which the
// first version does not replay automatically.
func HasMergeCommits(ctx context.Context, dir, rangeSpec string) (bool, error) {
	out, err := Run(ctx, dir, "rev-list", "--merges", "--count", rangeSpec)
	if err != nil {
		return false, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return false, memerr.New(memerr.GitFailed, "unexpected rev-list output %q", out)
	}
	return count > 0, nil
}

// MergeBase returns the common ancestor of two revisions.
func MergeBase(ctx context.Context, dir, left, right string) (string, error) {
	return Run(ctx, dir, "merge-base", left, right)
}

// FastForward advances the current branch to ref without creating a merge
// commit. Autostash stays disabled so external edits are never hidden.
func FastForward(ctx context.Context, dir, ref string) error {
	_, err := Run(ctx, dir, "-c", "merge.autostash=false", "merge", "--ff-only", "--no-edit", ref)
	return err
}

// Rebase replays local commits on top of ref with autostash disabled. A
// stopped rebase is reported by the caller as a content conflict.
func Rebase(ctx context.Context, dir, ref string) error {
	_, err := Run(ctx, dir, "-c", "rebase.autostash=false", "rebase", ref)
	return err
}

// RebaseContinue resumes a stopped rebase without opening an editor.
func RebaseContinue(ctx context.Context, dir string) error {
	_, err := runWithEnv(ctx, dir, []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"},
		"-c", "rebase.autostash=false", "rebase", "--continue")
	return err
}

// RebaseAbort aborts the rebase this process started.
func RebaseAbort(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "-c", "rebase.autostash=false", "rebase", "--abort")
	return err
}

// RebaseHead returns the commit currently being replayed by a stopped rebase.
func RebaseHead(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "REBASE_HEAD")
}

// Push sends the refspec to the remote. setUpstream adds -u so the first push
// of a new branch establishes tracking.
func Push(ctx context.Context, dir string, opts Options, remote, refspec string, setUpstream bool) error {
	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	args = append(args, remote, refspec)
	_, _, err := runGit(ctx, dir, opts, args...)
	return err
}

// RevParse resolves one revision.
func RevParse(ctx context.Context, dir, rev string) (string, error) {
	return Run(ctx, dir, "rev-parse", rev)
}

// Clone creates a working copy at target from url. The caller must ensure the
// target does not exist or is empty.
func Clone(ctx context.Context, opts Options, url, target string) error {
	_, _, err := runGit(ctx, "", opts, "clone", url, target)
	return err
}

// CheckoutTracking attaches an existing remote branch as the local branch and
// sets its upstream without guessing a name.
func CheckoutTracking(ctx context.Context, dir, branch, remote string) error {
	_, err := Run(ctx, dir, "checkout", "-B", branch, "--track", remote+"/"+branch)
	return err
}

// IndexEntry is one staged index record. Stage 1/2/3 records for the same path
// describe a conflict's base/onto/replayed sides.
type IndexEntry struct {
	Path   string
	Stage  int
	Mode   string
	BlobID string
}

// IndexEntries returns every staged index entry in path/stage order.
func IndexEntries(ctx context.Context, dir string) ([]IndexEntry, error) {
	out, _, err := runGitRaw(ctx, dir, Options{}, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	var entries []IndexEntry
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		// <mode> <object> <stage>\t<path>
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		entries = append(entries, IndexEntry{Path: path, Stage: stage, Mode: fields[0], BlobID: fields[1]})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Stage < entries[j].Stage
	})
	return entries, nil
}

// UnmergedPaths lists the distinct conflicted paths in the index.
func UnmergedPaths(ctx context.Context, dir string) ([]string, error) {
	entries, err := IndexEntries(ctx, dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, entry := range entries {
		if entry.Stage != 0 && !seen[entry.Path] {
			seen[entry.Path] = true
			paths = append(paths, entry.Path)
		}
	}
	return paths, nil
}

// CatFileBlob reads one blob's raw content without trimming it.
func CatFileBlob(ctx context.Context, dir, blobID string) (string, error) {
	out, _, err := runGitRaw(ctx, dir, Options{}, "cat-file", "-p", blobID)
	if err != nil {
		return "", err
	}
	return out, nil
}
