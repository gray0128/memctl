package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeProbeScript writes a POSIX stub that answers the handshake with body.
func writeProbeScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the probe stub is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "memctl")
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// TestProbeReadsToolsList covers the capability check behind M09: a binary that
// answers the handshake is reported with its tools and version.
func TestProbeReadsToolsList(t *testing.T) {
	path := writeProbeScript(t, `cat >/dev/null
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"memctl","version":"v9.9.9"}}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"memory_read"},{"name":"memory_write"}]}}'
`)

	result, err := Probe(context.Background(), path, "test")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !result.HasMemoryTools() {
		t.Fatalf("probe did not see both tools: %+v", result)
	}
	if result.Version != "v9.9.9" {
		t.Fatalf("probe version = %q", result.Version)
	}
}

// TestProbeRejectsBinaryWithoutSubcommand covers the diagnostic branch: a
// binary that exits without answering must not be reported as serving tools.
func TestProbeRejectsBinaryWithoutSubcommand(t *testing.T) {
	path := writeProbeScript(t, "echo 'memctl: unknown command \"mcp\"' >&2\nexit 2\n")

	if _, err := Probe(context.Background(), path, "test"); err == nil {
		t.Fatal("probe accepted a binary without the mcp subcommand")
	}
}

// TestProbeMissingBinary covers a path that cannot be started.
func TestProbeMissingBinary(t *testing.T) {
	if _, err := Probe(context.Background(), filepath.Join(t.TempDir(), "absent"), "test"); err == nil {
		t.Fatal("probe accepted a missing binary")
	}
}

// TestProbeIgnoresNonToolResults covers a binary that answers with a protocol
// error instead of a tool list.
func TestProbeIgnoresNonToolResults(t *testing.T) {
	path := writeProbeScript(t, `cat >/dev/null
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"memctl"}}}'
`)

	_, err := Probe(context.Background(), path, "test")
	if err == nil || !strings.Contains(err.Error(), "tools/list") {
		t.Fatalf("probe error = %v, want a missing tools/list diagnostic", err)
	}
}
