package integration_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestT048DshPluginTarballInstall covers I04/I06: the embedded plugin tarball
// installs into an isolated DSH profile and remains loadable after the tarball
// is removed. The test is skipped when dsh is not installed on the host.
func TestT048DshPluginTarballInstall(t *testing.T) {
	dshPath, err := exec.LookPath("dsh")
	if err != nil {
		t.Skip("dsh is not installed; the isolated profile install cannot run")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "memctl")
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, "./cmd/memctl")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	home := filepath.Join(tmpDir, "home")
	state := filepath.Join(tmpDir, "state")
	env := []string{
		"HOME=" + home,
		"DSH_HOME=" + filepath.Join(home, ".dsh"),
		"AGENT_MEMORY_STATE=" + state,
		"PATH=" + os.Getenv("PATH"),
	}

	tarball := runT048(t, env, binPath, "dsh", "plugin-path")
	if !filepath.IsAbs(tarball) {
		t.Fatalf("plugin-path printed %q, want an absolute path", tarball)
	}
	version := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(tarball), "memctl-dsh-"), ".tgz")
	if _, err := os.Stat(tarball); err != nil {
		t.Fatalf("plugin-path did not write the tarball: %v", err)
	}

	install := exec.Command(dshPath, "plugin", "--profile", "test", "add", tarball)
	install.Env = env
	install.Dir = tmpDir
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("dsh plugin add failed: %v\n%s", err, out)
	}

	assertDshConfigContains(t, dshPath, env, tmpDir, "memctl-dsh")

	// The profile owns a copy after installation, so removing the source tarball
	// must not break the next load.
	if err := os.Remove(tarball); err != nil {
		t.Fatalf("remove the installed tarball: %v", err)
	}
	assertDshConfigContains(t, dshPath, env, tmpDir, "memctl-dsh")

	status := runT048(t, env, binPath, "install", "--status")
	if !strings.Contains(status, "dsh: configured") || !strings.Contains(status, "plugin@"+version) {
		t.Fatalf("install --status did not detect the native plugin:\n%s", status)
	}
	if _, err := os.Stat(filepath.Join(home, ".dsh", "hooks.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("native plugin install wrote hooks.json: %v", err)
	}
}

func assertDshConfigContains(t *testing.T, dshPath string, env []string, dir, want string) {
	t.Helper()
	cmd := exec.Command(dshPath, "--profile", "test", "--dump-config")
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dsh --dump-config failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), want) {
		t.Fatalf("dsh --dump-config does not contain %q:\n%s", want, out)
	}
}

func runT048(t *testing.T, env []string, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", bin, args, err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
