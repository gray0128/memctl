package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/vault"
)

// This file verifies stage A-3 (T007) through the CLI binary: the local
// managed write, the explicit capture and the recovery entry points. Each run
// uses a temporary HOME and memory repository and forbids network Git
// protocols via GIT_ALLOW_PROTOCOL=file.

func runMemctlStdin(t *testing.T, bin, dir string, env []string, stdin string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run memctl %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return cliResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

type writeReadPayload struct {
	FilePath     string `json:"filePath"`
	FileRevision string `json:"fileRevision"`
	RawContent   string `json:"rawContent"`
}

func readRevision(t *testing.T, bin, home, repo string, env []string, path string) writeReadPayload {
	t.Helper()
	result := runMemctl(t, bin, home, env, "read", path, "--json")
	if result.code != 0 {
		t.Fatalf("read %s --json exit = %d (stderr=%q)", path, result.code, result.stderr)
	}
	var payload writeReadPayload
	if err := json.Unmarshal([]byte(result.stdout), &payload); err != nil {
		t.Fatalf("read %s --json is not valid JSON: %v\n%s", path, err, result.stdout)
	}
	return payload
}

// TestA3WriteCoversW01 covers W01 through the CLI: a fresh baseline succeeds,
// a stale baseline is refused with REVISION_MISMATCH and loses nothing.
func TestA3WriteCoversW01(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	baseline := readRevision(t, bin, home, repo, env, "MEMORY.md")
	firstText := "# Memory\n\n## Index\n- first\n"
	first := runMemctlStdin(t, bin, home, env, firstText,
		"write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin", "--agent", "test", "--sid", "s1")
	if first.code != 0 {
		t.Fatalf("write exit = %d (stderr=%q)", first.code, first.stderr)
	}
	if !strings.Contains(first.stdout, "commit: ") || !strings.Contains(first.stdout, "sync: local_only") {
		t.Fatalf("write stdout = %q, want commit and local_only", first.stdout)
	}
	if first.stderr != "" {
		t.Fatalf("write stderr = %q, want empty", first.stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, "MEMORY.md")); got != firstText {
		t.Fatalf("file = %q, want %q", got, firstText)
	}
	if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after write: %q", status)
	}
	// The commit records the transaction trailer, not the user content.
	if message := gitOutput(t, env, repo, "log", "-1", "--format=%B"); !strings.Contains(message, "Memctl-Transaction: ") {
		t.Fatalf("commit message missing the transaction trailer: %q", message)
	}

	stale := runMemctlStdin(t, bin, home, env, "# Memory\n\n## Index\n- stale\n",
		"write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin")
	if stale.code != 1 || !strings.Contains(stale.stderr, "REVISION_MISMATCH") {
		t.Fatalf("stale write: code=%d stderr=%q, want REVISION_MISMATCH", stale.code, stale.stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, "MEMORY.md")); got != firstText {
		t.Fatalf("stale write changed the file: %q", got)
	}

	newBaseline := readRevision(t, bin, home, repo, env, "MEMORY.md")
	if newBaseline.FileRevision == baseline.FileRevision {
		t.Fatal("fileRevision did not change after the managed write")
	}
}

// TestA3WriteCoversW03AndCapture covers the local part of W03: a normal write
// refuses external changes and `sync --capture` accepts them explicitly.
func TestA3WriteCoversW03AndCapture(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	baseline := readRevision(t, bin, home, repo, env, "MEMORY.md")
	if err := os.WriteFile(filepath.Join(repo, "external.md"), []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := runMemctlStdin(t, bin, home, env, "# Memory\n\n## Index\n- managed\n",
		"write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin")
	if blocked.code != 1 || !strings.Contains(blocked.stderr, "EXTERNAL_CHANGES") {
		t.Fatalf("write with external changes: code=%d stderr=%q, want EXTERNAL_CHANGES", blocked.code, blocked.stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, "MEMORY.md")); got != initialMemoryContent {
		t.Fatalf("refused write changed MEMORY.md: %q", got)
	}

	capture := runMemctl(t, bin, home, env, "sync", "--capture", "--agent", "test", "--sid", "s1")
	if capture.code != 0 {
		t.Fatalf("capture exit = %d (stderr=%q)", capture.code, capture.stderr)
	}
	if !strings.Contains(capture.stdout, "changed: external.md") {
		t.Fatalf("capture stdout = %q, want the captured path", capture.stdout)
	}
	if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after capture: %q", status)
	}
	if remote := gitOutput(t, env, repo, "remote"); remote != "" {
		t.Fatalf("capture must stay local, found remote %q", remote)
	}

	// `sync` without --capture is the automatic Hook path. Background sync is
	// disabled in this test environment, so it is a silent exit 0.
	plain := runMemctl(t, bin, home, env, "sync")
	if plain.code != 0 || plain.stdout != "" || plain.stderr != "" {
		t.Fatalf("plain sync: code=%d stdout=%q stderr=%q, want a silent 0", plain.code, plain.stdout, plain.stderr)
	}
}

// TestA3WriteBoundaries covers P01-P03 through the CLI.
func TestA3WriteBoundaries(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	invalid := []string{"../escape.md", ".git/config", "NUL", `notes\bad.md`}
	for _, path := range invalid {
		result := runMemctlStdin(t, bin, home, env, "x", "write", path, "--base", vault.AbsentRevision, "--stdin")
		if result.code != 1 || !strings.Contains(result.stderr, "INVALID_PATH") {
			t.Fatalf("write %q: code=%d stderr=%q, want INVALID_PATH", path, result.code, result.stderr)
		}
	}

	outside := filepath.Join(home, "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "link.md")); err != nil {
		t.Fatal(err)
	}
	// Commit the link so the clean-baseline check is not what stops the write;
	// the path boundary must reject it on its own.
	gitOutput(t, env, repo, "add", "--", "link.md")
	gitOutput(t, env, repo, "commit", "-q", "-m", "add symlink")
	link := runMemctlStdin(t, bin, home, env, "x", "write", "link.md", "--base", vault.AbsentRevision, "--stdin")
	if link.code != 1 || !strings.Contains(link.stderr, "UNSUPPORTED") {
		t.Fatalf("write over a symlink: code=%d stderr=%q, want UNSUPPORTED", link.code, link.stderr)
	}
	if got := mustReadFile(t, outside); got != "secret" {
		t.Fatalf("write escaped the repository: %q", got)
	}

	oversized := runMemctlStdin(t, bin, home, env, strings.Repeat("x", vault.MaxTextFileBytes+1),
		"write", "big.md", "--base", vault.AbsentRevision, "--stdin")
	if oversized.code != 1 || !strings.Contains(oversized.stderr, "LIMIT_EXCEEDED") {
		t.Fatalf("oversized write: code=%d stderr=%q, want LIMIT_EXCEEDED", oversized.code, oversized.stderr)
	}

	// A new file under an existing directory is accepted.
	if err := os.Mkdir(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	create := runMemctlStdin(t, bin, home, env, "created\n",
		"write", "notes/new.md", "--base", vault.AbsentRevision, "--stdin")
	if create.code != 0 {
		t.Fatalf("create exit = %d (stderr=%q)", create.code, create.stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, "notes", "new.md")); got != "created\n" {
		t.Fatalf("created file = %q", got)
	}
}

// TestA3RecoverCli verifies the recovery CLI wiring and its exit behavior.
func TestA3RecoverCli(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	status := runMemctl(t, bin, home, env, "recover", "--status")
	if status.code != 0 {
		t.Fatalf("recover --status exit = %d (stderr=%q)", status.code, status.stderr)
	}
	if !strings.Contains(status.stdout, "no unfinished transactions") {
		t.Fatalf("recover --status stdout = %q", status.stdout)
	}

	rollback := runMemctl(t, bin, home, env, "recover", "--rollback")
	if rollback.code != 0 {
		t.Fatalf("recover --rollback exit = %d (stderr=%q)", rollback.code, rollback.stderr)
	}

	missing := runMemctl(t, bin, home, env, "recover")
	if missing.code != 2 || !strings.Contains(missing.stderr, "exactly one of --status or --rollback") {
		t.Fatalf("recover without a mode: code=%d stderr=%q", missing.code, missing.stderr)
	}
}

// TestA3WriteRequiresIdentity covers the missing-identity part of G05 for the
// write path: the write fails clearly before changing files and memctl writes
// no global Git configuration.
func TestA3WriteRequiresIdentity(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	baseline := readRevision(t, bin, home, repo, env, "MEMORY.md")

	globalConfig, seededConfig := restrictIdentityAutoDetection(t, home)
	envNoIdentity := a2Env(home, repo, false)

	result := runMemctlStdin(t, bin, home, envNoIdentity, "# Memory\n\n## Index\n- uncommitted\n",
		"write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin")
	if result.code != 1 {
		t.Fatalf("write without identity exit = %d, want 1 (stderr=%q)", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "GIT_FAILED") || !strings.Contains(result.stderr, "identity") {
		t.Fatalf("write without identity stderr = %q, want a GIT_FAILED identity message", result.stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo, "MEMORY.md")); got != initialMemoryContent {
		t.Fatalf("write without identity changed MEMORY.md: %q", got)
	}
	if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the failed write: %q", status)
	}
	if content := mustReadFile(t, globalConfig); content != seededConfig {
		t.Fatalf("write rewrote the global Git config: %q", content)
	}
}
