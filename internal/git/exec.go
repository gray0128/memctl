package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// Options controls how one Git subprocess is started. Network commands need
// either an interactive terminal or an explicitly non-interactive environment
// so that credential helpers and SSH never block a caller without a terminal.
// See docs/03-workflows-and-dataflows.md section 4.3.
type Options struct {
	// NonInteractive forbids Git, SSH and askpass from prompting. Standard
	// input is closed and askpass fallbacks are removed for this child only.
	NonInteractive bool
	// Interactive gives Git and SSH the caller's terminal so they can run
	// their native prompts. It is ignored when NonInteractive is set.
	Interactive bool
	// ExtraEnv is appended after the scrubbed environment.
	ExtraEnv []string
}

// Error carries the raw Git stderr next to the contract error so the sync
// workflow can classify a failure from evidence instead of a formatted string.
type Error struct {
	Code     memerr.Code
	Args     string
	Stderr   string
	TimedOut bool
	contract *memerr.Error
}

func (e *Error) Error() string { return e.contract.Error() }

func (e *Error) Unwrap() error { return e.contract }

func newError(code memerr.Code, args, stderr string, cause error) *Error {
	detail := redact(firstLine(stderr))
	if detail == "" {
		detail = redact(firstLine(cause.Error()))
	}
	return &Error{
		Code:     code,
		Args:     args,
		Stderr:   stderr,
		TimedOut: code == memerr.Timeout,
		contract: memerr.Wrap(code, cause, "git %s failed: %s", redact(args), detail),
	}
}

// runGit starts one Git command, waits for it and terminates its process group
// when the context ends. It returns the trimmed stdout and the raw stderr.
func runGit(ctx context.Context, dir string, opts Options, args ...string) (string, string, error) {
	stdout, stderr, err := runGitRaw(ctx, dir, opts, args...)
	return strings.TrimSpace(stdout), stderr, err
}

// runGitRaw behaves like runGit but preserves the exact stdout bytes, which
// plumbing commands such as cat-file and ls-files -z require.
func runGitRaw(ctx context.Context, dir string, opts Options, args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = childEnv(opts)
	if opts.Interactive && !opts.NonInteractive {
		cmd.Stdin = os.Stdin
	} else {
		cmd.Stdin = nil
	}
	if file := inheritedFile(ctx); file != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, file)
	}

	configureProcessGroup(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", "", newError(memerr.GitFailed, strings.Join(args, " "), err.Error(), err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return stdout.String(), stderr.String(),
				newError(memerr.GitFailed, strings.Join(args, " "), stderr.String(), err)
		}
		return stdout.String(), stderr.String(), nil
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-done
		return stdout.String(), stderr.String(),
			newError(memerr.Timeout, strings.Join(args, " "), stderr.String(), ctx.Err())
	}
}

// childEnv builds the environment of one Git child from the single credential
// and network whitelist in env.go: repository redirection variables and every
// other caller variable are excluded, and non-interactive callers additionally
// have their prompting fallbacks suppressed.
func childEnv(opts Options) []string {
	env := SyncEnv(os.Environ())
	if opts.NonInteractive && !opts.Interactive {
		env = suppressPrompts(env)
	}
	return append(env, opts.ExtraEnv...)
}

// suppressPrompts removes the variables that would let Git or SSH open a
// prompt without a terminal. Agent sockets, key files and non-interactive
// credential helpers remain available.
func suppressPrompts(env []string) []string {
	blocked := map[string]bool{
		"GIT_ASKPASS":         true,
		"SSH_ASKPASS":         true,
		"SSH_ASKPASS_REQUIRE": true,
		"GIT_TERMINAL_PROMPT": true,
		"DISPLAY":             true,
	}
	kept := make([]string, 0, len(env)+2)
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && blocked[name] {
			continue
		}
		kept = append(kept, entry)
	}
	return append(kept,
		"GIT_TERMINAL_PROMPT=0",
		"SSH_ASKPASS_REQUIRE=never",
	)
}
