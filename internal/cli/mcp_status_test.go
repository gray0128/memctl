package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeStatusStub writes a POSIX stub standing in for a memctl binary.
func writeStatusStub(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the probe stub is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "memctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// TestPrintMCPStatusUnavailableBinary covers M09's diagnostic branch: a binary
// that cannot answer `mcp` is reported with its reason and no registration
// commands. Registering a server that fails every call would be worse than
// reporting the binary as unusable.
func TestPrintMCPStatusUnavailableBinary(t *testing.T) {
	path := writeStatusStub(t, "echo 'memctl: unknown command \"mcp\"' >&2\nexit 2\n")

	var stdout bytes.Buffer
	printMCPStatus(context.Background(), &stdout, path)
	out := stdout.String()
	if !strings.Contains(out, "mcp: unavailable:") {
		t.Fatalf("output did not report the unavailable channel:\n%s", out)
	}
	if strings.Contains(out, "mcp add memctl") {
		t.Fatalf("output printed registration commands for an unusable binary:\n%s", out)
	}
}

// TestPrintMCPStatusServingBinary covers the success branch and the exact
// registration actions. Every command must name the probed binary so a user can
// paste it unchanged.
func TestPrintMCPStatusServingBinary(t *testing.T) {
	path := writeStatusStub(t, `cat >/dev/null
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"memctl","version":"v1.0.0"}}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"memory_read"},{"name":"memory_write"}]}}'
`)

	var stdout bytes.Buffer
	printMCPStatus(context.Background(), &stdout, path)
	out := stdout.String()
	if !strings.Contains(out, "mcp: serving memory_read, memory_write (v1.0.0)") {
		t.Fatalf("output did not report the tools and version:\n%s", out)
	}
	for _, want := range []string{
		"claude-code: claude mcp add memctl --scope user -- " + path + " mcp --agent claude-code",
		"codex: codex mcp add memctl -- " + path + " mcp --agent codex",
		"grok: grok mcp add --scope user memctl -- " + path + " mcp --agent grok",
		"commandcode: commandcode mcp add --scope user memctl " + path + " mcp --agent commandcode",
		"opencode: opencode mcp add --global memctl -- " + path + " mcp --agent opencode",
		"pi: pi mcp add memctl -- " + path + " mcp --agent pi",
		`antigravity: add "memctl" to mcpServers`,
		"dsh: add an @deepseek-ai/dsh-mcp-client stdio entry",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
