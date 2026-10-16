package integration_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestR03BuildAndCLIEntry verifies scenario R03 from
// docs/05-validation-and-delivery.md. The build uses the recorded toolchain:
// internal/server embeds web/dist, so the frontend is built first via
// `make ui` (see Makefile). `version` and `help` succeed with exit 0 without
// accessing HOME/memory/state or requiring git/node in PATH; unknown commands
// and invalid flags fail with exit code 2; unimplemented subcommands do not
// return placeholder success.
func TestR03BuildAndCLIEntry(t *testing.T) {
	tmpDir := t.TempDir()
	binName := "memctl"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(tmpDir, binName)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	buildCmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=v2.0.0-r03test", "-o", binPath, "./cmd/memctl")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}

	isolatedHome := filepath.Join(tmpDir, "nonexistent-home")
	isolatedMem := filepath.Join(tmpDir, "nonexistent-memory")
	isolatedState := filepath.Join(tmpDir, "nonexistent-state")

	runIsolated := func(args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Dir = tmpDir
		cmd.Env = []string{
			"HOME=" + isolatedHome,
			"AGENT_MEMORY_DIR=" + isolatedMem,
			"AGENT_MEMORY_STATE=" + isolatedState,
			"PATH=",
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("unexpected execution error for %v: %v", args, err)
			}
		}
		return exitCode, stdout.String(), stderr.String()
	}

	code, stdout, stderr := runIsolated("version")
	if code != 0 {
		t.Fatalf("version exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if strings.TrimSpace(stdout) != "memctl v2.0.0-r03test" {
		t.Fatalf("version stdout = %q, want %q", stdout, "memctl v2.0.0-r03test\n")
	}
	if stderr != "" {
		t.Fatalf("version stderr = %q, want empty", stderr)
	}

	code, stdout, stderr = runIsolated("help")
	if code != 0 {
		t.Fatalf("help exit code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "memctl <subcommand>") {
		t.Fatalf("help stdout missing usage synopsis: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("help stderr = %q, want empty", stderr)
	}

	code, stdout, stderr = runIsolated("unknown-subcommand")
	if code != 2 {
		t.Fatalf("unknown-subcommand exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("unknown-subcommand stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("unknown-subcommand stderr missing message: %q", stderr)
	}

	// init is implemented since stage A-2. In an environment without Git it
	// must fail with a diagnostic exit code and must not create any path.
	code, stdout, stderr = runIsolated("init")
	if code != 1 {
		t.Fatalf("init without git exit code = %d, want 1 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("init without git stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "GIT_FAILED") {
		t.Fatalf("init without git stderr missing GIT_FAILED: %q", stderr)
	}

	// install is implemented since C-3. It requires an explicit agent or --all,
	// so a bare invocation is a usage error and creates no path.
	code, stdout, stderr = runIsolated("install")
	if code != 2 {
		t.Fatalf("install without an agent exit code = %d, want 2 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("install without an agent stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "at least one agent") {
		t.Fatalf("install usage stderr missing guidance: %q", stderr)
	}

	// doctor is implemented since A-6. With no repository it fails with a
	// targeted error and still creates no path. (R02 covers the existing-repo
	// but missing-Git case; here AGENT_MEMORY_DIR does not exist, so the
	// repository check reports NO_REPO first.)
	code, stdout, stderr = runIsolated("doctor")
	if code != 1 {
		t.Fatalf("doctor without a repository exit code = %d, want 1 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("doctor without a repository stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "NO_REPO") {
		t.Fatalf("doctor without a repository stderr missing NO_REPO: %q", stderr)
	}

	// resolve is implemented since A-4; with no repository it fails clearly and
	// still creates no path.
	code, stdout, stderr = runIsolated("resolve")
	if code != 1 {
		t.Fatalf("resolve without a repository exit code = %d, want 1 (stderr=%q)", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("resolve without a repository stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "NO_REPO") {
		t.Fatalf("resolve without a repository stderr missing NO_REPO: %q", stderr)
	}

	for _, p := range []string{isolatedHome, isolatedMem, isolatedState} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected path %q to remain untouched/non-existent, stat err=%v", p, err)
		}
	}
}
