package cli_test

import (
	"bytes"
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
			wantStdout: "Available Commands (Stage A-1):",
		},
		{
			name:       "help flag short",
			args:       []string{"-h"},
			version:    "v2.0.0-dev",
			wantCode:   cli.ExitOK,
			wantStdout: "Available Commands (Stage A-1):",
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
		"init", "start", "context", "pull", "sync", "end",
		"tag", "wrap", "read", "write", "resolve", "recover",
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
