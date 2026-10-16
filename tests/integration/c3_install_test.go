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

	"github.com/gray0128/memctl/internal/vault"
)

// c3Harness builds the binary once and runs it with an isolated HOME, state
// root and PATH that only contains a synthetic host executable.
type c3Harness struct {
	t        *testing.T
	binPath  string
	home     string
	stateDir string
	binDir   string
}

func newC3Harness(t *testing.T) *c3Harness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the synthetic host executable uses a POSIX shell")
	}
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "memctl")
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, "./cmd/memctl")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return &c3Harness{
		t:        t,
		binPath:  binPath,
		home:     filepath.Join(tmpDir, "home"),
		stateDir: filepath.Join(tmpDir, "state"),
		binDir:   filepath.Join(tmpDir, "bin"),
	}
}

// addHost installs a synthetic host executable that reports a version.
func (h *c3Harness) addHost(name, version string) {
	h.t.Helper()
	if err := os.MkdirAll(h.binDir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"" + name + " " + version + "\"\n"
	if err := os.WriteFile(filepath.Join(h.binDir, name), []byte(script), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *c3Harness) run(args ...string) (int, string, string) {
	h.t.Helper()
	cmd := exec.Command(h.binPath, args...)
	cmd.Env = []string{
		"HOME=" + h.home,
		"AGENT_MEMORY_STATE=" + h.stateDir,
		"PATH=" + h.binDir,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			h.t.Fatalf("unexpected execution error for %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

func (h *c3Harness) settingsPath() string {
	return filepath.Join(h.home, ".claude", "settings.json")
}

func (h *c3Harness) writeSettings(content string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.settingsPath()), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.settingsPath(), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *c3Harness) readSettings() string {
	h.t.Helper()
	data, err := os.ReadFile(h.settingsPath())
	if err != nil {
		h.t.Fatalf("read settings: %v", err)
	}
	return string(data)
}

// TestC3InstallCLIIdempotentCoversI01 covers I01: installing a supported
// adapter twice produces no duplicate entries and leaves the user's own hooks
// in place.
func TestC3InstallCLIIdempotentCoversI01(t *testing.T) {
	harness := newC3Harness(t)
	harness.addHost("claude", "2.1.0")
	harness.writeSettings(`{
  "model": "opus",
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "user-stop-hook" }] }
    ]
  }
}
`)

	code, stdout, stderr := harness.run("install", "claude-code")
	if code != 0 {
		t.Fatalf("first install exit = %d (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "applied") {
		t.Fatalf("first install stdout missing applied line: %q", stdout)
	}
	first := harness.readSettings()
	if !strings.Contains(first, "user-stop-hook") {
		t.Fatalf("user hook was lost:\n%s", first)
	}
	if count := strings.Count(first, "memctl"); count != 3 {
		t.Fatalf("expected three managed commands, got %d:\n%s", count, first)
	}

	code, stdout, stderr = harness.run("install", "claude-code")
	if code != 0 {
		t.Fatalf("second install exit = %d (stderr=%q)", code, stderr)
	}
	second := harness.readSettings()
	if second != first {
		t.Fatalf("second install changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if count := strings.Count(second, "memctl"); count != 3 {
		t.Fatalf("second install duplicated managed commands: %d", count)
	}
}

// TestC3InstallDryRunAndStatusDoNotWriteCoversI02 covers I02's no-write
// previews and the manual-patch fallback for an unsupported structure.
func TestC3InstallDryRunAndStatusDoNotWriteCoversI02(t *testing.T) {
	harness := newC3Harness(t)
	harness.addHost("claude", "2.1.0")

	code, stdout, stderr := harness.run("install", "claude-code", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit = %d (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "create") {
		t.Fatalf("dry-run stdout missing the plan: %q", stdout)
	}
	if _, err := os.Stat(harness.settingsPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote the configuration: %v", err)
	}

	code, stdout, stderr = harness.run("install", "--status")
	if code != 0 {
		t.Fatalf("status exit = %d (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "claude-code") || !strings.Contains(stdout, "claude") {
		t.Fatalf("status stdout missing the adapter: %q", stdout)
	}

	// A top-level JSON array cannot be merged safely, so memctl must report a
	// manual patch and leave the file untouched.
	harness.writeSettings("[]\n")
	code, stdout, stderr = harness.run("install", "claude-code")
	if code != 0 {
		t.Fatalf("manual merge exit = %d (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "manual") {
		t.Fatalf("manual merge stdout missing manual line: %q", stdout)
	}
	if harness.readSettings() != "[]\n" {
		t.Fatalf("manual merge modified the file:\n%s", harness.readSettings())
	}
}

// TestC3InstallRecoversInterruptedInstallCoversI02 covers I02's interruption
// recovery through the CLI: a recorded install replaces the file, the journal
// survives, and the next run restores the backup before continuing.
func TestC3InstallRecoversInterruptedInstallCoversI02(t *testing.T) {
	harness := newC3Harness(t)
	harness.addHost("claude", "2.1.0")

	backupContent := "{\n  \"userMarker\": true\n}\n"
	installedContent := "{\"installed\":true}\n"
	harness.writeSettings(installedContent)

	backupPath := filepath.Join(harness.stateDir, "adapters", "backups", "install-t", "00-settings.json")
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte(backupContent), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := map[string]any{
		"installId": "install-t",
		"startedAt": "2026-10-17T00:00:00Z",
		"agent":     "claude-code",
		"files": []map[string]any{{
			"path":              harness.settingsPath(),
			"existed":           true,
			"backupPath":        backupPath,
			"mode":              "0644",
			"installedRevision": vault.FileRevision("100644", []byte(installedContent)),
			"plannedRevision":   vault.FileRevision("100644", []byte(installedContent)),
			"applied":           true,
		}},
	}
	journalPath := filepath.Join(harness.stateDir, "adapters", "install.json")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(journal)
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := harness.run("install", "claude-code")
	if code != 0 {
		t.Fatalf("install after interruption exit = %d (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "recovered") {
		t.Fatalf("install stdout missing the recovery report: %q", stdout)
	}
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still present after recovery: %v", err)
	}
	final := harness.readSettings()
	if !strings.Contains(final, "userMarker") {
		t.Fatalf("recovery did not restore the backup before reinstalling:\n%s", final)
	}
}

// TestC3EmbeddedTemplatesCoverR01 covers R01's embedded-adapter-template
// scenario: a copied binary with an empty HOME lists every supported adapter
// and installs from the embedded template.
func TestC3EmbeddedTemplatesCoverR01(t *testing.T) {
	harness := newC3Harness(t)
	code, stdout, stderr := harness.run("install", "--status")
	if code != 0 {
		t.Fatalf("status exit = %d (stderr=%q)", code, stderr)
	}
	for _, agent := range []string{"claude-code", "codex", "grok", "antigravity", "commandcode", "opencode", "pi", "dsh"} {
		if !strings.Contains(stdout, agent+":") {
			t.Fatalf("status output missing adapter %q:\n%s", agent, stdout)
		}
	}

	harness.addHost("opencode", "1.0.0")
	code, stdout, stderr = harness.run("install", "opencode")
	if code != 0 {
		t.Fatalf("opencode install exit = %d (stderr=%q)", code, stderr)
	}
	pluginPath := filepath.Join(harness.home, ".config", "opencode", "plugins", "agent-memory.ts")
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("embedded opencode template was not written: %v", err)
	}
	if !strings.Contains(string(data), "memctl") {
		t.Fatalf("opencode plugin is not the embedded template:\n%s", data)
	}
}
