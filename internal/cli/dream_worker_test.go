package cli

import (
	"path/filepath"
	"testing"

	"github.com/gray0128/memctl/internal/config"
)

// TestWorkerRepositoryResolvesWorkspace guards the Actions entry point. The
// worker runs on a fresh runner whose home directory has no memory repository;
// it must operate on the checked-out workspace instead of the configured
// default, otherwise Git runs in a missing directory and reports a misleading
// "fork/exec ... no such file or directory".
func TestWorkerRepositoryResolvesWorkspace(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{RepoPath: "/nonexistent/home/runner/memory", StateDir: "/state"}

	env := map[string]string{"GITHUB_WORKSPACE": workspace}
	got, err := workerRepository(func(key string) string { return env[key] }, cfg)
	if err != nil {
		t.Fatalf("workerRepository: %v", err)
	}
	if got.RepoPath != config.Canonical(workspace) {
		t.Fatalf("RepoPath = %q, want the workspace %q", got.RepoPath, config.Canonical(workspace))
	}
	if got.StateDir != cfg.StateDir {
		t.Fatalf("StateDir = %q, want the configured %q", got.StateDir, cfg.StateDir)
	}

	// An explicit AGENT_MEMORY_DIR keeps the configured repository.
	env["AGENT_MEMORY_DIR"] = "/explicit/repo"
	got, err = workerRepository(func(key string) string { return env[key] }, cfg)
	if err != nil {
		t.Fatalf("workerRepository with AGENT_MEMORY_DIR: %v", err)
	}
	if got.RepoPath != cfg.RepoPath {
		t.Fatalf("RepoPath = %q, want the configured %q", got.RepoPath, cfg.RepoPath)
	}

	// No workspace and no explicit repository is a targeted error, never a
	// silent fallback to the default path.
	if _, err := workerRepository(func(string) string { return "" }, cfg); err == nil {
		t.Fatal("workerRepository accepted an empty environment")
	}
	if _, err := workerRepository(func(key string) string {
		if key == "GITHUB_WORKSPACE" {
			return filepath.Join(workspace, "missing")
		}
		return ""
	}, cfg); err == nil {
		t.Fatal("workerRepository accepted a missing workspace directory")
	}
}
