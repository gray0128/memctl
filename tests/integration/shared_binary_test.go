package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// sharedBinary caches one memctl build per test process. The integration suite
// drives the real binary from more than a hundred call sites; building it once
// instead of once per test removes the dominant fixed cost without changing
// what any test exercises. Tests must treat the path as read-only: it outlives
// t.TempDir and is shared by every test in the process.
var (
	sharedBinaryOnce sync.Once
	sharedBinaryPath string
	sharedBinaryErr  error
)

// buildMemctlBinary returns the shared memctl binary, building it on first use.
func buildMemctlBinary(t *testing.T) string {
	t.Helper()
	sharedBinaryOnce.Do(func() {
		sharedBinaryPath, sharedBinaryErr = buildSharedBinary("")
	})
	if sharedBinaryErr != nil {
		t.Fatalf("build memctl: %v", sharedBinaryErr)
	}
	return sharedBinaryPath
}

// buildMemctlBinaryWithVersion returns a separate build with an explicit
// version stamp. R03 needs a known version string, which the shared build does
// not carry, so it builds its own copy.
func buildMemctlBinaryWithVersion(t *testing.T, version string) string {
	t.Helper()
	path, err := buildSharedBinary(version)
	if err != nil {
		t.Fatalf("build memctl: %v", err)
	}
	return path
}

// buildSharedBinary compiles ./cmd/memctl into a process-lifetime temporary
// directory. An empty version leaves main.version at its default.
func buildSharedBinary(version string) (string, error) {
	binName := "memctl"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	dir, err := os.MkdirTemp("", "memctl-integration-bin-")
	if err != nil {
		return "", err
	}
	binPath := filepath.Join(dir, binName)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", err
	}
	args := []string{"build", "-trimpath"}
	if version != "" {
		args = append(args, "-ldflags", fmt.Sprintf("-s -w -X main.version=%s", version))
	}
	args = append(args, "-o", binPath, "./cmd/memctl")
	build := exec.Command("go", args...)
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build failed: %v\n%s", err, string(out))
	}
	return binPath, nil
}
