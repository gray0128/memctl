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
	if !strings.Contains(stdout, "mcp: serving memory_read, memory_write") {
		t.Fatalf("status did not report the MCP tools:\n%s", stdout)
	}
	for _, want := range []string{
		"claude-code: claude mcp add memctl --scope user -- " + harness.binPath + " mcp --agent claude-code",
		"codex: codex mcp add memctl -- " + harness.binPath + " mcp --agent codex",
		"grok: grok mcp add --scope user memctl -- " + harness.binPath + " mcp --agent grok",
		"commandcode: commandcode mcp add --scope user memctl " + harness.binPath + " mcp --agent commandcode",
		"opencode: opencode mcp add --global memctl -- " + harness.binPath + " mcp --agent opencode",
		"pi: pi mcp add memctl -- " + harness.binPath + " mcp --agent pi",
		"antigravity: add \"memctl\" to mcpServers",
		"dsh: add an @deepseek-ai/dsh-mcp-client stdio entry",
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
