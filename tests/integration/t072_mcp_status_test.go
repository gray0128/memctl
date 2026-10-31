package integration_test

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestM09InstallStatusReportsMCP covers M09: `install --status` reports the MCP
// channel of the running binary and one registration action per adapter. See
// docs/09-mcp-memory-tools.md sections 5.3 and 6.
func TestM09InstallStatusReportsMCP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the synthetic host executable uses a POSIX shell")
	}
	harness := newC3Harness(t)

	code, stdout, stderr := harness.run("install", "--status")
	if code != 0 {
		t.Fatalf("status exit = %d (stderr=%q)", code, stderr)
	}
	for _, tool := range []string{"memory_entries", "memory_read", "memory_write"} {
		if !strings.Contains(stdout, "mcp: serving "+tool) && !strings.Contains(stdout, ", "+tool) {
			t.Fatalf("status did not report the %s tool:\n%s", tool, stdout)
		}
	}
	for _, want := range []string{
		"claude-code: claude mcp add memctl --scope user -- " + harness.binPath + " mcp --agent claude-code",
		"codex: codex mcp add memctl -- " + harness.binPath + " mcp --agent codex",
		"grok: grok mcp add --scope user memctl -- " + harness.binPath + " mcp --agent grok",
		"commandcode: commandcode mcp add --scope user memctl " + harness.binPath + " mcp --agent commandcode",
		"opencode: opencode mcp add --global memctl -- " + harness.binPath + " mcp --agent opencode",
		"pi: pi mcp add memctl --exposure direct -- " + harness.binPath + " mcp --agent pi",
		"antigravity: agy mcp add --type stdio memctl " + harness.binPath + " mcp --agent antigravity",
		"dsh: add an @deepseek-ai/dsh-mcp-client stdio entry",
		// Delta accepts only remote HTTPS MCP servers, so the entry
		// must say so instead of offering a command that cannot work.
		"delta: not supported: Delta connects only to remote HTTPS MCP servers",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}

	// The MCP block is informational: it must not create or modify host
	// configuration.
	entries, err := os.ReadDir(harness.home)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read home: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == ".claude" || entry.Name() == ".codex" || entry.Name() == ".pi" ||
			entry.Name() == ".grok" || entry.Name() == ".gemini" || entry.Name() == ".commandcode" {
			t.Fatalf("status wrote host configuration %q", entry.Name())
		}
	}
}

// TestDeltaAdapterStatusReportsNoManagedConfig covers the Delta row of
// `install --status`: Delta has no lifecycle hook, so the adapter is detected
// from its CLI and reported as configured with manual steps, and the install
// writes nothing. See docs/13-delta-integration.md sections 3 and 5.
func TestDeltaAdapterStatusReportsNoManagedConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the synthetic host executable uses a POSIX shell")
	}
	harness := newC3Harness(t)
	harness.addHost("delta", "0.23.0")

	code, stdout, stderr := harness.run("install", "--status")
	if code != 0 {
		t.Fatalf("status exit = %d (stderr=%q)", code, stderr)
	}
	for _, want := range []string{
		"delta: configured host=delta@0.23.0",
		"step: no lifecycle hook",
		"step: install the Skill with npx skills add gray0128/memctl -g -s memctl -a zed -y",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}
	// No managed artifact for Delta: the plan must not name a config path.
	if strings.Contains(stdout, "delta: not_installed") {
		t.Fatalf("delta was reported as not_installed:\n%s", stdout)
	}

	// `install delta` must be a no-op: no file may appear under HOME.
	code, dryRun, stderr := harness.run("install", "delta", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit = %d (stderr=%q)", code, stderr)
	}
	if !strings.Contains(dryRun, "delta: configured") {
		t.Fatalf("dry-run did not report delta as configured:\n%s", dryRun)
	}
	entries, err := os.ReadDir(harness.home)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read home: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "bin" {
			t.Fatalf("install delta wrote %q into HOME", entry.Name())
		}
	}
}
