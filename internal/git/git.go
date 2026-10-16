// Package git runs Git with explicit argv and a scrubbed environment. It
// reports Git failures; it never decides to abort an operation or to ignore
// an error on behalf of the application service.
package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// redirectedEnv lists variables that would silently point Git at another
// repository, index or object store than the one bound by the caller.
var redirectedEnv = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	"GIT_INDEX_FILE":                   true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_COMMON_DIR":                   true,
	"GIT_NAMESPACE":                    true,
	"GIT_PREFIX":                       true,
	"GIT_CEILING_DIRECTORIES":          true,
}

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
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = scrubEnv(os.Environ())
	cmd.Stdin = nil

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := firstLine(stderr.String())
		if detail == "" {
			detail = firstLine(err.Error())
		}
		return strings.TrimSpace(stdout.String()), memerr.Wrap(memerr.GitFailed, err,
			"git %s failed: %s", redact(strings.Join(args, " ")), detail)
	}
	return strings.TrimSpace(stdout.String()), nil
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
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(scrubEnv(os.Environ()), extra...)
	cmd.Stdin = nil

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := firstLine(stderr.String())
		if detail == "" {
			detail = firstLine(err.Error())
		}
		return strings.TrimSpace(stdout.String()), memerr.Wrap(memerr.GitFailed, err,
			"git %s failed: %s", redact(strings.Join(args, " ")), detail)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func scrubEnv(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && redirectedEnv[name] {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
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
