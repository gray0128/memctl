package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/config"
)

// This file verifies stage A-2 (T006) from docs/05-validation-and-delivery.md:
// G01 (idempotent local init), the local part of G05 (missing Git identity),
// F04 (read / read --json), P01/P02 (path and link boundaries) and P03 (bounded
// reads). Every run uses a temporary HOME and memory repository. The child
// environment sets GIT_ALLOW_PROTOCOL=file, so any network access through Git
// would fail instead of reaching the network.

const initialMemoryContent = "# Memory\n\n## Index\n"

type cliResult struct {
	code   int
	stdout string
	stderr string
}

func buildMemctlBinary(t *testing.T) string {
	t.Helper()
	binName := "memctl"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, "./cmd/memctl")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}
	return binPath
}

// a2Env builds an isolated child environment. When withIdentity is false no
// author or committer identity is available and no global Git config exists.
func a2Env(home, repo string, withIdentity bool) []string {
	env := []string{
		"HOME=" + home,
		"AGENT_MEMORY_DIR=" + repo,
		"AGENT_MEMORY_STATE=" + filepath.Join(home, ".agent-memory"),
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
		"GIT_CONFIG_SYSTEM=" + filepath.Join(home, "absent-system-gitconfig"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=file",
		"GIT_LITERAL_PATHSPECS=1",
	}
	if withIdentity {
		env = append(env,
			"GIT_AUTHOR_NAME=Memctl Test",
			"GIT_AUTHOR_EMAIL=memctl-test@example.invalid",
			"GIT_COMMITTER_NAME=Memctl Test",
			"GIT_COMMITTER_EMAIL=memctl-test@example.invalid",
		)
	}
	return env
}

func runMemctl(t *testing.T, bin, dir string, env []string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env
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

// restrictIdentityAutoDetection seeds a global Git config that disables
// identity auto-detection, simulating a machine with no configured identity.
// It returns the seeded path and content for a post-condition check.
func restrictIdentityAutoDetection(t *testing.T, home string) (string, string) {
	t.Helper()
	path := filepath.Join(home, "gitconfig")
	content := "[user]\n\tuseConfigOnly = true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed restricted git config: %v", err)
	}
	return path, content
}

func gitOutput(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func gitFails(t *testing.T, env []string, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("git %v unexpectedly succeeded:\n%s", args, string(out))
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// TestA2InitIsIdempotent covers G01: one first commit, and a repeated init
// changes neither files nor configuration.
func TestA2InitIsIdempotent(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)

	first := runMemctl(t, bin, home, env, "init")
	if first.code != 0 {
		t.Fatalf("first init exit = %d, want 0 (stderr=%q)", first.code, first.stderr)
	}
	if !strings.Contains(first.stdout, "memory repo: "+config.Canonical(repo)) {
		t.Fatalf("first init stdout = %q, want the repository path", first.stdout)
	}
	if !strings.Contains(first.stdout, "state: created") {
		t.Fatalf("first init stdout = %q, want state: created", first.stdout)
	}
	if first.stderr != "" {
		t.Fatalf("first init stderr = %q, want empty", first.stderr)
	}

	memoryBefore := mustReadFile(t, filepath.Join(repo, "MEMORY.md"))
	if memoryBefore != initialMemoryContent {
		t.Fatalf("MEMORY.md = %q, want %q", memoryBefore, initialMemoryContent)
	}
	headBefore := gitOutput(t, env, repo, "rev-parse", "HEAD")
	if count := gitOutput(t, env, repo, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("commit count after init = %s, want 1", count)
	}
	if remotes := gitOutput(t, env, repo, "remote"); remotes != "" {
		t.Fatalf("local init must not configure a remote, got %q", remotes)
	}

	second := runMemctl(t, bin, home, env, "init")
	if second.code != 0 {
		t.Fatalf("second init exit = %d, want 0 (stderr=%q)", second.code, second.stderr)
	}
	if !strings.Contains(second.stdout, "state: existing") {
		t.Fatalf("second init stdout = %q, want state: existing", second.stdout)
	}
	if !strings.Contains(second.stdout, "head: "+headBefore) {
		t.Fatalf("second init stdout = %q, want the unchanged head %s", second.stdout, headBefore)
	}

	if memoryAfter := mustReadFile(t, filepath.Join(repo, "MEMORY.md")); memoryAfter != memoryBefore {
		t.Fatalf("repeated init changed MEMORY.md: %q -> %q", memoryBefore, memoryAfter)
	}
	if headAfter := gitOutput(t, env, repo, "rev-parse", "HEAD"); headAfter != headBefore {
		t.Fatalf("repeated init changed HEAD: %s -> %s", headBefore, headAfter)
	}
	if count := gitOutput(t, env, repo, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("commit count after repeated init = %s, want 1", count)
	}
	if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("repository is not clean after init: %q", status)
	}
	if remotes := gitOutput(t, env, repo, "remote"); remotes != "" {
		t.Fatalf("repeated init must not configure a remote, got %q", remotes)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-memory")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stage A-2 init must not create runtime state, stat err = %v", err)
	}
}

// TestA2InitWithoutIdentity covers the local part of G05: a missing Git
// identity fails clearly, memctl writes no global Git configuration, and a
// later init with a repository-local identity completes exactly one commit.
func TestA2InitWithoutIdentity(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, false)
	globalConfig, seededConfig := restrictIdentityAutoDetection(t, home)

	result := runMemctl(t, bin, home, env, "init")
	if result.code != 1 {
		t.Fatalf("init without identity exit = %d, want 1 (stderr=%q)", result.code, result.stderr)
	}
	if result.stdout != "" {
		t.Fatalf("init without identity stdout = %q, want empty", result.stdout)
	}
	if !strings.Contains(result.stderr, "GIT_FAILED") || !strings.Contains(result.stderr, "identity") {
		t.Fatalf("init without identity stderr = %q, want a GIT_FAILED identity message", result.stderr)
	}

	if mustReadFile(t, filepath.Join(repo, "MEMORY.md")) != initialMemoryContent {
		t.Fatal("init must keep the entry file it created before failing to commit")
	}
	gitFails(t, env, repo, "rev-parse", "HEAD")
	if content := mustReadFile(t, globalConfig); content != seededConfig {
		t.Fatalf("init rewrote the global Git config: %q", content)
	}

	gitOutput(t, env, repo, "config", "user.name", "Memctl Test")
	gitOutput(t, env, repo, "config", "user.email", "memctl-test@example.invalid")

	completed := runMemctl(t, bin, home, env, "init")
	if completed.code != 0 {
		t.Fatalf("completing init exit = %d, want 0 (stderr=%q)", completed.code, completed.stderr)
	}
	if !strings.Contains(completed.stdout, "state: completed") {
		t.Fatalf("completing init stdout = %q, want state: completed", completed.stdout)
	}
	if count := gitOutput(t, env, repo, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("commit count after completing init = %s, want 1", count)
	}
}

// TestA2InitRefusesUnsafeTargets verifies that non-empty directories, nested
// Git repositories and unrelated repositories are refused without modification.
func TestA2InitRefusesUnsafeTargets(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()

	t.Run("empty existing directory", func(t *testing.T) {
		target := filepath.Join(home, "empty-memory")
		if err := os.MkdirAll(target, 0o700); err != nil {
			t.Fatal(err)
		}
		result := runMemctl(t, bin, home, a2Env(home, target, true), "init")
		if result.code != 0 || !strings.Contains(result.stdout, "state: created") {
			t.Fatalf("init on an empty directory: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
		}
		if mustReadFile(t, filepath.Join(target, "MEMORY.md")) != initialMemoryContent {
			t.Fatal("init did not create the entry file")
		}
	})

	t.Run("existing file as target", func(t *testing.T) {
		target := filepath.Join(home, "not-a-directory")
		if err := os.WriteFile(target, []byte("plain file"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := runMemctl(t, bin, home, a2Env(home, target, true), "init")
		if result.code != 1 || !strings.Contains(result.stderr, "INVALID_ARGUMENT") {
			t.Fatalf("init on a file: code=%d stderr=%q", result.code, result.stderr)
		}
		if content := mustReadFile(t, target); content != "plain file" {
			t.Fatalf("refused init modified the target file: %q", content)
		}
	})

	t.Run("non-empty ordinary directory", func(t *testing.T) {
		target := filepath.Join(home, "documents")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		existing := filepath.Join(target, "keep.txt")
		if err := os.WriteFile(existing, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
		env := a2Env(home, target, true)
		result := runMemctl(t, bin, home, env, "init")
		if result.code != 1 || !strings.Contains(result.stderr, "INVALID_ARGUMENT") {
			t.Fatalf("init on non-empty directory: code=%d stderr=%q", result.code, result.stderr)
		}
		if content := mustReadFile(t, existing); content != "keep me" {
			t.Fatalf("refused init modified existing content: %q", content)
		}
		if _, err := os.Stat(filepath.Join(target, ".git")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused init created .git, stat err = %v", err)
		}
	})

	t.Run("nested repository", func(t *testing.T) {
		parent := filepath.Join(home, "project")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		env := a2Env(home, parent, true)
		gitOutput(t, env, parent, "init", "-q", "-b", "main")

		target := filepath.Join(parent, "memory")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		nestedEnv := a2Env(home, target, true)
		result := runMemctl(t, bin, home, nestedEnv, "init")
		if result.code != 1 || !strings.Contains(result.stderr, "inside the Git repository") {
			t.Fatalf("init inside a repository: code=%d stderr=%q", result.code, result.stderr)
		}
	})

	t.Run("unrelated repository without entry file", func(t *testing.T) {
		target := filepath.Join(home, "other-repo")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		env := a2Env(home, target, true)
		gitOutput(t, env, target, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(target, "README.md"), []byte("# Other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := runMemctl(t, bin, home, env, "init")
		if result.code != 1 || !strings.Contains(result.stderr, "INVALID_ARGUMENT") {
			t.Fatalf("init on unrelated repository: code=%d stderr=%q", result.code, result.stderr)
		}
		if _, err := os.Stat(filepath.Join(target, "MEMORY.md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused init created MEMORY.md, stat err = %v", err)
		}
	})
}

// TestA2InitKeepsHookAndSigningPolicy covers the remaining local part of G05:
// a rejecting hook or a failing signature makes init fail clearly, and memctl
// neither disables hooks nor turns off commit signing.
func TestA2InitKeepsHookAndSigningPolicy(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	env := a2Env(home, filepath.Join(home, "memory"), true)

	setupRepo := func(t *testing.T, name string) string {
		t.Helper()
		repo := filepath.Join(home, name)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		gitOutput(t, env, repo, "init", "-q", "-b", "main")
		gitOutput(t, env, repo, "config", "user.name", "Memctl Test")
		gitOutput(t, env, repo, "config", "user.email", "memctl-test@example.invalid")
		if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte(initialMemoryContent), 0o644); err != nil {
			t.Fatal(err)
		}
		return repo
	}

	t.Run("rejecting pre-commit hook", func(t *testing.T) {
		repo := setupRepo(t, "hooked-memory")
		hookPath := filepath.Join(repo, ".git", "hooks", "pre-commit")
		if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		result := runMemctl(t, bin, home, a2Env(home, repo, true), "init")
		if result.code != 1 || !strings.Contains(result.stderr, "GIT_FAILED") {
			t.Fatalf("init with a rejecting hook: code=%d stderr=%q", result.code, result.stderr)
		}
		gitFails(t, env, repo, "rev-parse", "HEAD")
		if _, err := os.Stat(hookPath); err != nil {
			t.Fatalf("memctl removed the user hook: %v", err)
		}
	})

	t.Run("failing commit signature", func(t *testing.T) {
		repo := setupRepo(t, "signed-memory")
		gitOutput(t, env, repo, "config", "commit.gpgsign", "true")
		gitOutput(t, env, repo, "config", "gpg.program", "/bin/false")

		result := runMemctl(t, bin, home, a2Env(home, repo, true), "init")
		if result.code != 1 || !strings.Contains(result.stderr, "GIT_FAILED") {
			t.Fatalf("init with a failing signature: code=%d stderr=%q", result.code, result.stderr)
		}
		if mode := gitOutput(t, env, repo, "config", "--get", "commit.gpgsign"); mode != "true" {
			t.Fatalf("memctl changed commit.gpgsign to %q", mode)
		}
	})
}

// TestA2ReadCoversF04 checks the raw and JSON read outputs, the revision
// baseline and the explicit failures for missing, non-text and over-limit
// inputs.
func TestA2ReadCoversF04(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)

	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	plain := runMemctl(t, bin, home, env, "read", "MEMORY.md")
	if plain.code != 0 {
		t.Fatalf("read exit = %d (stderr=%q)", plain.code, plain.stderr)
	}
	if plain.stdout != initialMemoryContent {
		t.Fatalf("read stdout = %q, want the exact file bytes %q", plain.stdout, initialMemoryContent)
	}

	type readPayload struct {
		FilePath     string `json:"filePath"`
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	decode := func(result cliResult) readPayload {
		t.Helper()
		if result.code != 0 {
			t.Fatalf("read --json exit = %d (stderr=%q)", result.code, result.stderr)
		}
		var payload readPayload
		if err := json.Unmarshal([]byte(result.stdout), &payload); err != nil {
			t.Fatalf("read --json output is not valid JSON: %v\n%s", err, result.stdout)
		}
		return payload
	}

	payload := decode(runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json"))
	if payload.FilePath != "MEMORY.md" {
		t.Fatalf("filePath = %q, want MEMORY.md", payload.FilePath)
	}
	if payload.RawContent != initialMemoryContent {
		t.Fatalf("rawContent = %q, want %q", payload.RawContent, initialMemoryContent)
	}
	if !strings.HasPrefix(payload.FileRevision, "sha256:") || len(payload.FileRevision) != len("sha256:")+64 {
		t.Fatalf("fileRevision = %q, want a full sha256 digest", payload.FileRevision)
	}
	if repeated := decode(runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json")); repeated.FileRevision != payload.FileRevision {
		t.Fatalf("fileRevision is not stable: %q -> %q", payload.FileRevision, repeated.FileRevision)
	}

	entryPath := filepath.Join(repo, "MEMORY.md")
	if err := os.Chmod(entryPath, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	executable := decode(runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json"))
	if executable.FileRevision == payload.FileRevision {
		t.Fatal("fileRevision must change when the Git file mode changes")
	}
	if err := os.Chmod(entryPath, 0o644); err != nil {
		t.Fatalf("chmod back: %v", err)
	}

	if err := os.Mkdir(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "diagram.png"), []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "huge.md"), []byte(strings.Repeat("x", 2<<20+1)), 0o644); err != nil {
		t.Fatal(err)
	}

	failures := []struct {
		name string
		args []string
		code string
	}{
		{"missing file", []string{"read", "notes/missing.md"}, "NOT_FOUND"},
		{"binary asset", []string{"read", "diagram.png"}, "UNSUPPORTED"},
		{"over limit", []string{"read", "huge.md"}, "LIMIT_EXCEEDED"},
		{"directory", []string{"read", "notes"}, "INVALID_PATH"},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			result := runMemctl(t, bin, home, env, tc.args...)
			if result.code != 1 {
				t.Fatalf("%v exit = %d, want 1 (stderr=%q)", tc.args, result.code, result.stderr)
			}
			if result.stdout != "" {
				t.Fatalf("%v stdout = %q, want empty", tc.args, result.stdout)
			}
			if !strings.Contains(result.stderr, tc.code) {
				t.Fatalf("%v stderr = %q, want %s", tc.args, result.stderr, tc.code)
			}
		})
	}
}

// TestA2ReadBoundaries covers the P01 and P02 read cases: escapes, aliases and
// links never reach content outside the repository.
func TestA2ReadBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink assertions require Unix link semantics")
	}
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)

	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	const secret = "outside-secret-content"
	outside := filepath.Join(home, "outside.md")
	if err := os.WriteFile(outside, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	invalidPaths := []string{
		"../outside.md",
		"notes/../../outside.md",
		"/etc/hosts",
		".git/config",
		".GIT/HEAD",
		"NUL",
		`notes\windows.md`,
	}
	for _, path := range invalidPaths {
		result := runMemctl(t, bin, home, env, "read", path)
		if result.code != 1 {
			t.Fatalf("read %q exit = %d, want 1", path, result.code)
		}
		if result.stdout != "" {
			t.Fatalf("read %q stdout = %q, want empty", path, result.stdout)
		}
		if !strings.Contains(result.stderr, "INVALID_PATH") {
			t.Fatalf("read %q stderr = %q, want INVALID_PATH", path, result.stderr)
		}
	}

	symlinkResult := runMemctl(t, bin, home, env, "read", "link.md")
	if symlinkResult.code != 1 || !strings.Contains(symlinkResult.stderr, "UNSUPPORTED") {
		t.Fatalf("read link.md: code=%d stderr=%q, want UNSUPPORTED", symlinkResult.code, symlinkResult.stderr)
	}
	if strings.Contains(symlinkResult.stdout, secret) {
		t.Fatal("read of a symbolic link leaked content from outside the repository")
	}

	noRepoHome := t.TempDir()
	noRepoEnv := a2Env(noRepoHome, filepath.Join(noRepoHome, "memory"), true)
	result := runMemctl(t, bin, noRepoHome, noRepoEnv, "read", "MEMORY.md")
	if result.code != 1 || !strings.Contains(result.stderr, "NO_REPO") {
		t.Fatalf("read without a repository: code=%d stderr=%q, want NO_REPO", result.code, result.stderr)
	}
}
