package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/config"
)

// TestScrubEnvDropsRepositoryRedirects guards the rule that memctl always
// binds the repository it was configured with, never a redirected one.
func TestScrubEnvDropsRepositoryRedirects(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"GIT_DIR=/tmp/other/.git",
		"GIT_WORK_TREE=/tmp/other",
		"GIT_INDEX_FILE=/tmp/other/index",
		"GIT_OBJECT_DIRECTORY=/tmp/other/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/tmp/other/alt",
		"GIT_COMMON_DIR=/tmp/other/common",
	}

	kept := scrubEnv(env)
	joined := strings.Join(kept, "\n")
	for _, dropped := range []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_OBJECT_DIRECTORY="} {
		if strings.Contains(joined, dropped) {
			t.Fatalf("scrubEnv kept %s: %v", dropped, kept)
		}
	}
	for _, keptEntry := range []string{"PATH=/usr/bin", "HOME=/tmp/home"} {
		if !strings.Contains(joined, keptEntry) {
			t.Fatalf("scrubEnv dropped %s: %v", keptEntry, kept)
		}
	}
}

func TestRedactHidesCredentialURLs(t *testing.T) {
	got := redact("git fetch https://user:secret@example.com/repo.git failed")
	if strings.Contains(got, "secret") {
		t.Fatalf("redact leaked a credential: %q", got)
	}
	if !strings.Contains(got, "https://***@example.com/repo.git") {
		t.Fatalf("redact = %q, want the credential replaced", got)
	}
}

// TestSuppressPrompts covers the non-interactive credential environment: the
// askpass fallbacks are removed, the agent socket is kept and Git is told not
// to prompt.
func TestSuppressPrompts(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
		"GIT_ASKPASS=/tmp/askpass",
		"SSH_ASKPASS=/tmp/ssh-askpass",
		"GIT_TERMINAL_PROMPT=1",
		"DISPLAY=:0",
	}
	kept := suppressPrompts(env)
	joined := strings.Join(kept, "\n")
	for _, dropped := range []string{"GIT_ASKPASS=", "SSH_ASKPASS=", "DISPLAY=", "GIT_TERMINAL_PROMPT=1"} {
		if strings.Contains(joined, dropped) {
			t.Fatalf("suppressPrompts kept %s: %v", dropped, kept)
		}
	}
	for _, keptEntry := range []string{"SSH_AUTH_SOCK=/tmp/agent.sock", "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never"} {
		if !strings.Contains(joined, keptEntry) {
			t.Fatalf("suppressPrompts dropped %s: %v", keptEntry, kept)
		}
	}
}

// TestRunIgnoresInheritedRedirection proves the scrubbing with a real Git
// invocation: an inherited GIT_DIR must not redirect the command.
func TestRunIgnoresInheritedRedirection(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	repo := t.TempDir()
	ctx := context.Background()
	if err := Init(ctx, repo); err != nil {
		t.Fatalf("Init: %v", err)
	}

	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "nonexistent", ".git"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "index"))

	top, err := TopLevel(ctx, repo)
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	if config.Canonical(top) != config.Canonical(repo) {
		t.Fatalf("TopLevel = %q, want %q", top, repo)
	}
}
