// Package git runs Git with explicit argv and a scrubbed environment. It
// reports Git failures; it never decides to abort an operation or to ignore
// an error on behalf of the application service.
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

var credentialURL = regexp.MustCompile(`://[^/@\s]*@`)

// Available reports whether a Git executable can be found before any
// repository work starts.
func Available() error {
	if _, err := exec.LookPath("git"); err != nil {
		return memerr.Wrap(memerr.GitFailed, err, "Git executable not found in PATH")
	}
	return nil
}

// Run executes one Git command inside dir and returns its trimmed stdout.
// It uses the caller's environment without prompting.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, _, err := runGit(ctx, dir, Options{}, args...)
	return out, err
}

// Init creates a new repository whose initial branch is main.
func Init(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "init", "-q", "-b", "main")
	return err
}

// Add stages exactly the given repository-relative paths, using literal
// pathspec semantics so that names such as ":!x" cannot widen the scope.
func Add(ctx context.Context, dir string, paths ...string) error {
	if len(paths) == 0 {
		return memerr.New(memerr.InvalidArgument, "git add requires at least one path")
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := runWithEnv(ctx, dir, []string{"GIT_LITERAL_PATHSPECS=1"}, args...)
	return err
}

// Commit creates a commit and returns the resulting HEAD hash.
func Commit(ctx context.Context, dir, message string) (string, error) {
	if _, err := Run(ctx, dir, "commit", "-q", "-m", message); err != nil {
		return "", err
	}
	return Head(ctx, dir)
}

// Head returns the current HEAD hash and fails when HEAD is unborn.
func Head(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "HEAD")
}

// UnbornHead reports whether the repository has no commit yet.
func UnbornHead(ctx context.Context, dir string) bool {
	_, err := Head(ctx, dir)
	return err != nil
}

// TopLevel returns the canonical working-tree root that contains dir.
func TopLevel(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--show-toplevel")
}

// GitDir returns the absolute Git directory of the repository containing dir.
func GitDir(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--absolute-git-dir")
}

// RepoState describes Git intermediate states that block a normal local
// transaction. The application service decides what to do with them; this
// package only reports what Git actually contains.
// See docs/05-validation-and-delivery.md G06 and G07.
type RepoState struct {
	// Operation is the in-progress Git operation, if any: merge, rebase,
	// cherry-pick or revert.
	Operation string
	// Sequencer reports an interactive rebase or cherry-pick sequencer.
	Sequencer bool
	// Unmerged reports unmerged index entries, which also cover a rebase
	// that left no rebase directory behind.
	Unmerged bool
	// IndexLocked reports an existing index.lock, meaning another Git
	// process may be writing the index.
	IndexLocked bool
}

// Active reports whether the state blocks ordinary writes and automatic sync.
func (s RepoState) Active() bool {
	return s.Operation != "" || s.Sequencer || s.Unmerged || s.IndexLocked
}

// Inspect reports Git intermediate states. It does not modify the repository.
func Inspect(ctx context.Context, dir string) (RepoState, error) {
	gitDir, err := GitDir(ctx, dir)
	if err != nil {
		return RepoState{}, err
	}

	state := RepoState{}
	operations := []struct {
		name string
		kind string
		path string
	}{
		{"rebase-merge", "rebase", "rebase-merge"},
		{"rebase-apply", "rebase", "rebase-apply"},
		{"MERGE_HEAD", "merge", "MERGE_HEAD"},
		{"CHERRY_PICK_HEAD", "cherry-pick", "CHERRY_PICK_HEAD"},
		{"REVERT_HEAD", "revert", "REVERT_HEAD"},
	}
	for _, operation := range operations {
		if pathExists(gitDir, operation.path) {
			state.Operation = operation.kind
			break
		}
	}
	state.Sequencer = pathExists(gitDir, "sequencer")
	state.IndexLocked = pathExists(gitDir, "index.lock")

	unmerged, err := Run(ctx, dir, "ls-files", "--unmerged")
	if err != nil {
		return RepoState{}, err
	}
	state.Unmerged = strings.TrimSpace(unmerged) != ""
	return state, nil
}

// StatusPorcelain returns the raw `git status --porcelain -uall` output. An
// empty string means the worktree, the index and untracked files are all
// clean relative to HEAD.
func StatusPorcelain(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "status", "--porcelain", "-uall")
}

// ChangedPaths returns every path with a staged, unstaged or untracked change.
// It lists paths explicitly so a later `capture` can stage exactly that set
// instead of widening to `git add -A`.
func ChangedPaths(ctx context.Context, dir string) ([]string, error) {
	seen := map[string]struct{}{}
	queries := [][]string{
		{"diff", "--cached", "--name-only", "-z", "--no-renames"},
		{"diff", "--name-only", "-z", "--no-renames"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	}
	for _, query := range queries {
		out, err := Run(ctx, dir, query...)
		if err != nil {
			return nil, err
		}
		for _, path := range strings.Split(out, "\x00") {
			if path != "" {
				seen[path] = struct{}{}
			}
		}
	}
	return sortedKeys(seen), nil
}

// StagedPaths returns the paths currently staged in the index.
func StagedPaths(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "diff", "--cached", "--name-only", "-z", "--no-renames")
	if err != nil {
		return nil, err
	}
	return splitNul(out), nil
}

// CommittedPaths returns the paths changed by one commit.
func CommittedPaths(ctx context.Context, dir, commit string) ([]string, error) {
	out, err := Run(ctx, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--no-renames", commit)
	if err != nil {
		return nil, err
	}
	return splitNul(out), nil
}

// HeadMessage returns the full commit message of HEAD.
func HeadMessage(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "log", "-1", "--format=%B")
}

// ResetPaths unstages exactly the given paths, leaving the worktree untouched.
func ResetPaths(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"reset", "-q", "--"}, paths...)
	_, err := runWithEnv(ctx, dir, []string{"GIT_LITERAL_PATHSPECS=1"}, args...)
	return err
}

func pathExists(gitDir, name string) bool {
	_, err := os.Lstat(filepath.Join(gitDir, name))
	return err == nil
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func splitNul(out string) []string {
	var paths []string
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// RequireIdentity fails with an actionable message when Git cannot resolve a
// commit identity. The caller must not fall back to writing global config.
func RequireIdentity(ctx context.Context, dir string) error {
	for _, variable := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		out, err := Run(ctx, dir, "var", variable)
		if err == nil && strings.TrimSpace(out) != "" {
			continue
		}
		reason := ""
		if contractErr, ok := memerr.As(err); ok {
			reason = contractErr.Message
		} else if err != nil {
			reason = err.Error()
		} else {
			reason = variable + " is empty"
		}
		return memerr.New(memerr.GitFailed,
			"Git identity is unavailable; configure user.name and user.email for this repository (memctl does not modify global Git config): %s", reason)
	}
	return nil
}

func runWithEnv(ctx context.Context, dir string, extra []string, args ...string) (string, error) {
	out, _, err := runGit(ctx, dir, Options{ExtraEnv: extra}, args...)
	return out, err
}

func redact(text string) string {
	return credentialURL.ReplaceAllString(text, "://***@")
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[:idx]
	}
	const maxLen = 300
	if len(text) > maxLen {
		text = text[:maxLen] + "..."
	}
	return text
}
