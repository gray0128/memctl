package cli_test

import (
	"bytes"
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
			wantStdout: "Available Commands (Stage A-2):",
		},
		{
			name:       "help flag short",
			args:       []string{"-h"},
			version:    "v2.0.0-dev",
			wantCode:   cli.ExitOK,
			wantStdout: "Available Commands (Stage A-2):",
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
		"start", "context", "pull", "sync", "end",
		"tag", "wrap", "write", "resolve", "recover",
		"ui", "doctor", "dream", "install",
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

// TestInitRemoteURLIsRejectedLocally verifies that stage A-2 does not create a
// repository, or touch any path, when a remote URL is requested before A-4.
func TestInitRemoteURLIsRejectedLocally(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_MEMORY_DIR", filepath.Join(t.TempDir(), "memory"))

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"init", "git@example.com:user/memory.git", "--confirm-private"}, strings.NewReader(""), &stdout, &stderr, "v2.0.0-dev")
	if code != cli.ExitError {
		t.Fatalf("init with remote URL returned %d, want %d (stderr=%q)", code, cli.ExitError, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "UNSUPPORTED") {
		t.Fatalf("expected UNSUPPORTED reason, got %q", stderr.String())
	}
}
