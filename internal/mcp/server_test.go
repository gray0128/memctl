package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/vault"
)

// runServe feeds lines to Serve and returns the decoded responses plus stderr.
func runServe(t *testing.T, server *Server, lines ...string) ([]JSONRPCResponse, string) {
	t.Helper()
	input := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	if err := Serve(context.Background(), server, input, &stdout, &stderr); err != nil {
		t.Fatalf("Serve returned %v", err)
	}

	var responses []JSONRPCResponse
	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	scanner.Buffer(make([]byte, 1024), maxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var response JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout line is not JSON: %v\n%s", err, line)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning stdout: %v", err)
	}
	return responses, stderr.String()
}

// TestM01HandshakeAndProtocolErrors covers M01: initialize, unknown methods,
// invalid JSON lines and the all-JSON stdout invariant.
func TestM01HandshakeAndProtocolErrors(t *testing.T) {
	server := newTestServer(&fakeService{})
	responses, stderr := runServe(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	// initialize, parse error, method not found, ping -- the notification has
	// no response.
	if len(responses) != 4 {
		t.Fatalf("want 4 responses, got %d: %+v", len(responses), responses)
	}

	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools struct {
				ListChanged bool `json:"listChanged"`
			} `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	raw, _ := json.Marshal(responses[0].Result)
	if err := json.Unmarshal(raw, &initResult); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if initResult.ProtocolVersion != ProtocolVersion || initResult.ServerInfo.Name != "memctl" {
		t.Fatalf("unexpected initialize result %+v", initResult)
	}

	if responses[1].Error == nil || responses[1].Error.Code != codeParseError {
		t.Fatalf("want parse error, got %+v", responses[1])
	}
	if responses[2].Error == nil || responses[2].Error.Code != codeMethodNotFound {
		t.Fatalf("want method-not-found, got %+v", responses[2])
	}
	if responses[3].Error != nil {
		t.Fatalf("ping failed: %+v", responses[3])
	}
}

// TestM01UnknownProtocolVersion covers the fallback to the newest supported
// revision instead of echoing an unsupported one.
func TestM01UnknownProtocolVersion(t *testing.T) {
	server := newTestServer(&fakeService{})
	responses, _ := runServe(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
	)
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("unexpected responses %+v", responses)
	}
	raw, _ := json.Marshal(responses[0].Result)
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(raw, &result)
	if result.ProtocolVersion != ProtocolVersion {
		t.Fatalf("version = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
}

// TestM01ToolsCallOverStream covers the tools/list and tools/call round trip
// over the wire, including the isError result shape.
func TestM01ToolsCallOverStream(t *testing.T) {
	service := &fakeService{
		readResult: vault.FileRead{RelPath: "MEMORY.md", FileRevision: "sha256:1", RawContent: "# Memory\n"},
	}
	server := newTestServer(service)
	responses, _ := runServe(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memory_read","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_read"}}`,
	)
	if len(responses) != 3 {
		t.Fatalf("want 3 responses, got %d", len(responses))
	}
	raw, _ := json.Marshal(responses[0].Result)
	var list struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Tools) != 2 {
		t.Fatalf("tools/list result: %v %+v", err, list)
	}
	if responses[1].Error != nil {
		t.Fatalf("memory_read failed: %+v", responses[1])
	}
	if responses[2].Error != nil {
		t.Fatalf("missing arguments should default the path: %+v", responses[2])
	}
	if service.reads[len(service.reads)-1] != DefaultEntryPoint {
		t.Fatalf("default path = %q", service.reads[len(service.reads)-1])
	}
}

// TestM10MissingSessionIdentity covers M10: without a session id the write
// still runs and records an unknown identity instead of inventing one.
func TestM10MissingSessionIdentity(t *testing.T) {
	service := &fakeService{applyResult: app.MutationResult{Commit: "abc", SyncState: "pending"}}
	server := NewServer(service, Session{Agent: "dsh"})
	result, rpcErr := server.CallTool(context.Background(), "memory_write",
		json.RawMessage(`{"path":"MEMORY.md","base":"absent","content":"x"}`))
	if rpcErr != nil || result.IsError {
		t.Fatalf("unexpected result %+v %+v", result, rpcErr)
	}
	if service.writes[0].SessionID != "" {
		t.Fatalf("session id = %q, want empty", service.writes[0].SessionID)
	}
}

// TestM11DiagnosticsDoNotEchoContent covers M11: failures must not put memory
// text or tool arguments on stderr.
func TestM11DiagnosticsDoNotEchoContent(t *testing.T) {
	const secret = "SENSITIVE-MEMORY-TEXT"
	service := &fakeService{applyErrs: []error{
		errors.New("git failed while committing " + secret),
	}}
	server := newTestServer(service)
	responses, stderr := runServe(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write","arguments":{"path":"MEMORY.md","base":"absent","content":"`+secret+`"}}}`,
	)
	if stderr != "" {
		t.Fatalf("stderr must stay empty: %s", stderr)
	}
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("unexpected responses %+v", responses)
	}
	raw, _ := json.Marshal(responses[0].Result)
	if !bytes.Contains(raw, []byte("isError")) {
		t.Fatalf("want isError result, got %s", raw)
	}
}

// TestM12OversizedLineKeepsStreamUsable covers M12's stream durability: one
// oversized message is rejected and the next request is still answered.
func TestM12OversizedLineKeepsStreamUsable(t *testing.T) {
	server := newTestServer(&fakeService{})
	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","params":"` + strings.Repeat("a", maxLineBytes+1024) + `"}`
	responses, _ := runServe(t, server, huge, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if len(responses) != 2 {
		t.Fatalf("want 2 responses, got %d: %+v", len(responses), responses)
	}
	if responses[0].Error == nil || responses[0].Error.Code != codeParseError {
		t.Fatalf("want parse error for the oversized line, got %+v", responses[0])
	}
	if responses[1].Error != nil {
		t.Fatalf("stream did not recover: %+v", responses[1])
	}
}

// TestM12ServeStopsOnEOF covers a clean shutdown when the host closes stdin.
func TestM12ServeStopsOnEOF(t *testing.T) {
	server := newTestServer(&fakeService{})
	var stdout, stderr bytes.Buffer
	if err := Serve(context.Background(), server, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected output %q", stdout.String())
	}
}
