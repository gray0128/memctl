package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/cli"
	piplugin "github.com/gray0128/memctl/plugins/pi"
)

// TestRunPiPluginDir covers docs/08-pi-plugin.md section 8.2: the command writes
// the local package directory under the state root, prints an absolute path and
// never touches the pi configuration.
func TestRunPiPluginDir(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_MEMORY_STATE", state)

	dir := runPiCommand(t, "plugin-dir")
	if !filepath.IsAbs(dir) {
		t.Fatalf("plugin dir %q is not absolute", dir)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(state, "pi"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("plugin dir = %q, want %q", dir, filepath.Join(state, "pi"))
	}
	for _, name := range []string{"package.json", "index.js", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("plugin dir is missing %s: %v", name, err)
		}
	}
	version, err := piplugin.Version()
	if err != nil {
		t.Fatalf("plugin version: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"version": "`+version+`"`) {
		t.Fatalf("manifest version does not match %s:\n%s", version, manifest)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi")); !os.IsNotExist(err) {
		t.Fatalf("pi configuration was modified: %v", err)
	}
}

func TestRunPiUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"pi"}, {"pi", "unknown"}, {"pi", "plugin-path"}, {"pi", "plugin-dir", "extra"}} {
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

func runPiCommand(t *testing.T, action string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"pi", action}, strings.NewReader(""), &stdout, &stderr, "v0.1.2")
	if code != cli.ExitOK {
		t.Fatalf("pi %s exited %d: %s", action, code, stderr.String())
	}
	path := strings.TrimSpace(stdout.String())
	if path == "" || strings.Contains(path, "\n") {
		t.Fatalf("pi %s printed an unexpected path %q", action, stdout.String())
	}
	return path
}
