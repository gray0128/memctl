package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/cli"
)

func TestRunVersionAndHelp(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		version    string
		wantCode   int
		wantStdout string
	}{
		{
			name:       "version subcommand",
			args:       []string{"version"},
			version:    "v2.0.0-dev",
			wantCode:   cli.ExitOK,
			wantStdout: "memctl v2.0.0-dev\n",
		},
		{
			name:       "version flag long",
			args:       []string{"--version"},
			version:    "v2.0.0-custom",
			wantCode:   cli.ExitOK,
			wantStdout: "memctl v2.0.0-custom\n",
		},
		{
			name:       "version flag short",
			args:       []string{"-v"},
			version:    "",
			wantCode:   cli.ExitOK,
			wantStdout: "memctl v2.0.0-dev\n",
		},
		{
			name:       "help subcommand",
			args:       []string{"help"},
			version:    "v2.0.0-dev",
			wantCode:   cli.ExitOK,
			wantStdout: "Usage:\n  memctl <subcommand> [flags] [-- command...]",
		},
		{
			name:       "help flag long",
			args:       []string{"--help"},
			version:    "v2.0.0-dev",
			wantCode:   cli.ExitOK,
			wantStdout: "Available Commands:",
		},
		{
			name:       "help flag short",
			args:       []string{"-h"},
			version:    "v2.0.0-dev",
			wantCode:   cli.ExitOK,
			wantStdout: "Available Commands:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(tc.args, strings.NewReader(""), &stdout, &stderr, tc.version)
			if code != tc.wantCode {
				t.Fatalf("got exit code %d, want %d (stderr=%q)", code, tc.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Fatalf("stdout %q does not contain %q", stdout.String(), tc.wantStdout)
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected empty stderr, got %q", stderr.String())
			}
		})
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "missing subcommand",
			args:       nil,
			wantStderr: "missing subcommand",
		},
		{
			name:       "unknown subcommand",
			args:       []string{"nonexistent-cmd"},
			wantStderr: `unknown command "nonexistent-cmd"`,
		},
		{
			name:       "unknown top-level flag",
			args:       []string{"--unknown"},
			wantStderr: `unknown flag "--unknown"`,
		},
		{
			name:       "version with extra argument",
			args:       []string{"version", "--json"},
			wantStderr: "unexpected arguments",
		},
		{
			name:       "help with extra argument",
			args:       []string{"help", "extra"},
			wantStderr: "unexpected arguments",
		},
		{
			name:       "read without path",
			args:       []string{"read"},
			wantStderr: "missing required <path> argument",
		},
		{
			name:       "read with extra path",
			args:       []string{"read", "A.md", "B.md"},
			wantStderr: "unexpected argument",
		},
		{
			name:       "read with unknown flag",
			args:       []string{"read", "--raw"},
			wantStderr: `unknown flag "--raw"`,
		},
		{
			name:       "write without path",
			args:       []string{"write"},
			wantStderr: "missing required <path> argument",
		},
		{
			name:       "write without base",
			args:       []string{"write", "A.md", "--stdin"},
			wantStderr: "missing required --base revision",
		},
		{
			name:       "write without stdin flag",
			args:       []string{"write", "A.md", "--base", "absent"},
			wantStderr: "missing required --stdin flag",
		},
		{
			name:       "write base without value",
			args:       []string{"write", "A.md", "--base"},
			wantStderr: "--base requires a value",
		},
		{
			name:       "recover without a mode",
			args:       []string{"recover"},
			wantStderr: "exactly one of --status or --rollback is required",
		},
		{
			name:       "recover with both modes",
			args:       []string{"recover", "--status", "--rollback"},
			wantStderr: "exactly one of --status or --rollback is required",
		},
		{
			name:       "sync unknown flag",
			args:       []string{"sync", "--force"},
			wantStderr: `unknown argument "--force"`,
		},
		{
			name:       "init with confirmation but no URL",
			args:       []string{"init", "--confirm-private"},
			wantStderr: "--confirm-private requires a remote URL",
		},
		{
			name:       "init with unknown flag",
			args:       []string{"init", "--force"},
			wantStderr: `unknown flag "--force"`,
		},
		{
			name:       "init with two positionals",
			args:       []string{"init", "git@example.com:a/b.git", "extra"},
			wantStderr: "unexpected argument",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(tc.args, strings.NewReader(""), &stdout, &stderr, "v2.0.0-dev")
			if code != cli.ExitUsageError {
				t.Fatalf("got exit code %d, want %d", code, cli.ExitUsageError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected empty stdout on usage error, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("stderr %q does not contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

func TestPlannedCommandsRejectPlaceholderSuccess(t *testing.T) {
	planned := []string{
		"ui", "dream", "install",
	}

	for _, sub := range planned {
		t.Run(sub, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{sub}, strings.NewReader(""), &stdout, &stderr, "v2.0.0-dev")
			if code == cli.ExitOK {
				t.Fatalf("planned command %q must not return placeholder success (0)", sub)
			}
			if code != cli.ExitError {
				t.Fatalf("planned command %q returned %d, want %d", sub, code, cli.ExitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected empty stdout for unimplemented command %q, got %q", sub, stdout.String())
			}
			if !strings.Contains(stderr.String(), "UNSUPPORTED") {
				t.Fatalf("expected UNSUPPORTED reason in stderr for %q, got %q", sub, stderr.String())
			}
		})
	}
}

// TestDoctorRejectsUnsupportedFix verifies that the read-only doctor does not
// pretend to apply the deterministic repairs that belong to stage C-1.
func TestDoctorRejectsUnsupportedFix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"doctor", "--fix"}, strings.NewReader(""), &stdout, &stderr, "v2.0.0-dev")
	if code != cli.ExitError {
		t.Fatalf("doctor --fix exit code = %d, want %d (stderr=%q)", code, cli.ExitError, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("doctor --fix stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "UNSUPPORTED") {
		t.Fatalf("doctor --fix stderr = %q, want UNSUPPORTED", stderr.String())
	}
}

// TestInitRemoteRequiresConfirmation verifies that connecting a remote needs
// the explicit private-ownership confirmation.
func TestInitRemoteRequiresConfirmation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_MEMORY_DIR", filepath.Join(t.TempDir(), "memory"))

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"init", "file:///nonexistent/memory.git"}, strings.NewReader(""), &stdout, &stderr, "v2.0.0-dev")
	if code != cli.ExitUsageError {
		t.Fatalf("init without --confirm-private returned %d, want %d (stderr=%q)", code, cli.ExitUsageError, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--confirm-private") {
		t.Fatalf("expected a --confirm-private hint, got %q", stderr.String())
	}
}

// TestInitRemoteFailureIsNotReportedAsSuccess verifies that an unreachable
// remote fails instead of being treated as an empty remote, and leaves no
// initialized repository behind.
func TestInitRemoteFailureIsNotReportedAsSuccess(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	t.Setenv("HOME", home)
	t.Setenv("AGENT_MEMORY_DIR", repo)
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"init", "file:///nonexistent/memory.git", "--confirm-private"}, strings.NewReader(""), &stdout, &stderr, "v2.0.0-dev")
	if code != cli.ExitError {
		t.Fatalf("init with an unreachable remote returned %d, want %d (stdout=%q stderr=%q)", code, cli.ExitError, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "GIT_FAILED") && !strings.Contains(stderr.String(), "NETWORK_UNAVAILABLE") && !strings.Contains(stderr.String(), "AUTH_FAILED") {
		t.Fatalf("expected a classified remote failure, got %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed remote init created a repository, stat err = %v", err)
	}
}
