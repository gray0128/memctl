package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/cli"
)

// TestRunDshPluginArtifacts covers the absolute paths printed for the DSH
// "Add plugin" dialog and the absence of profile writes.
func TestRunDshPluginArtifacts(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_MEMORY_STATE", state)

	tarball := runDshCommand(t, "plugin-path")
	if !filepath.IsAbs(tarball) || !strings.HasSuffix(tarball, "memctl-dsh-0.1.2.tgz") {
		t.Fatalf("unexpected tarball path %q", tarball)
	}
	if _, err := os.Stat(tarball); err != nil {
		t.Fatalf("tarball was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "dsh")); err != nil {
		t.Fatalf("plugin artifact is not under the state root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".dsh")); !os.IsNotExist(err) {
		t.Fatalf("dsh profile was modified: %v", err)
	}

	dir := runDshCommand(t, "plugin-dir")
	if !filepath.IsAbs(dir) || filepath.Base(dir) != "0.1.2" {
		t.Fatalf("unexpected plugin dir %q", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		t.Fatalf("plugin dir is incomplete: %v", err)
	}
}

func TestRunDshUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"dsh"}, {"dsh", "unknown"}, {"dsh", "plugin-path", "extra"}} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr, "v0.1.2")
		if code != cli.ExitUsageError {
			t.Fatalf("%v: got exit code %d, want %d (stderr=%q)", args, code, cli.ExitUsageError, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("%v: usage error wrote to stdout: %q", args, stdout.String())
		}
	}
}

func runDshCommand(t *testing.T, action string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"dsh", action}, strings.NewReader(""), &stdout, &stderr, "v0.1.2")
	if code != cli.ExitOK {
		t.Fatalf("dsh %s exited %d: %s", action, code, stderr.String())
	}
	path := strings.TrimSpace(stdout.String())
	if path == "" || strings.Contains(path, "\n") {
		t.Fatalf("dsh %s printed an unexpected path %q", action, stdout.String())
	}
	return path
}
