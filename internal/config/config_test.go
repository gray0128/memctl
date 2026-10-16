package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/memerr"
)

func TestLoadDefaults(t *testing.T) {
	home := t.TempDir()
	getenv := func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantRepo := filepath.Join(config.Canonical(home), "memory")
	if cfg.RepoPath != wantRepo {
		t.Fatalf("RepoPath = %q, want %q", cfg.RepoPath, wantRepo)
	}
	wantState := filepath.Join(config.Canonical(home), ".agent-memory")
	if cfg.StateDir != wantState {
		t.Fatalf("StateDir = %q, want %q", cfg.StateDir, wantState)
	}
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "custom-memory")
	state := filepath.Join(home, "custom-state")
	getenv := func(key string) string {
		switch key {
		case "HOME":
			return home
		case config.EnvRepoDir:
			return repo
		case config.EnvStateDir:
			return state
		}
		return ""
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoPath != config.Canonical(repo) {
		t.Fatalf("RepoPath = %q, want %q", cfg.RepoPath, config.Canonical(repo))
	}
	if cfg.StateDir != config.Canonical(state) {
		t.Fatalf("StateDir = %q, want %q", cfg.StateDir, config.Canonical(state))
	}
}

func TestLoadWithoutHomeFails(t *testing.T) {
	_, err := config.Load(func(string) string { return "" })
	contractErr, ok := memerr.As(err)
	if !ok || contractErr.Code != memerr.InvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %v", err)
	}
}

func TestRepoIDIsStableAndPathSpecific(t *testing.T) {
	first := config.RepoID("/tmp/memctl-repo-a")
	second := config.RepoID("/tmp/memctl-repo-a")
	other := config.RepoID("/tmp/memctl-repo-b")

	if first != second {
		t.Fatalf("RepoID is not deterministic: %q != %q", first, second)
	}
	if first == other {
		t.Fatalf("RepoID must differ per path, both %q", first)
	}
	if len(first) != len("sha256:")+64 || first[:7] != "sha256:" {
		t.Fatalf("RepoID %q does not use the sha256: prefix with a full digest", first)
	}
}

func TestCanonicalResolvesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if got, want := config.Canonical(link), config.Canonical(dir); got != want {
		t.Fatalf("Canonical(%q) = %q, want %q", link, got, want)
	}
}
