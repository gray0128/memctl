package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// fakeService records calls and returns scripted results.
type fakeService struct {
	readResult  vault.FileRead
	readErr     error
	applyResult app.MutationResult
	applyErrs   []error
	reads       []string
	writes      []app.MutationRequest
}

func (f *fakeService) ReadFile(_ context.Context, relPath string) (vault.FileRead, error) {
	f.reads = append(f.reads, relPath)
	return f.readResult, f.readErr
}

func (f *fakeService) Apply(_ context.Context, req app.MutationRequest) (app.MutationResult, error) {
	f.writes = append(f.writes, req)
	if len(f.applyErrs) > 0 {
		err := f.applyErrs[0]
		f.applyErrs = f.applyErrs[1:]
		return app.MutationResult{}, err
	}
	return f.applyResult, nil
}

func newTestServer(service Service) *Server {
	return NewServer(service, Session{Agent: "dsh", SessionID: "session-1", Device: "device-1"})
}

// TestM02ToolsList covers M02: exactly the two memory tools, with schemas.
func TestM02ToolsList(t *testing.T) {
	server := newTestServer(&fakeService{})
	tools := server.Tools()
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	names := []string{tools[0].Name, tools[1].Name}
	if names[0] != "memory_read" || names[1] != "memory_write" {
		t.Fatalf("unexpected tool names %v", names)
	}
	for _, tool := range tools {
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s schema is not an object", tool.Name)
		}
		if schema["type"] != "object" {
			t.Errorf("%s schema type = %v", tool.Name, schema["type"])
		}
	}
	if got := ToolNames(); len(got) != 2 {
		t.Fatalf("ToolNames() = %v", got)
	}
}

// TestM03MemoryRead covers M03: default path and explicit path return the same
// fields as `read --json`.
func TestM03MemoryRead(t *testing.T) {
	service := &fakeService{readResult: vault.FileRead{
		RelPath:      "MEMORY.md",
		FileRevision: "sha256:abc",
		RawContent:   "# Memory\n",
	}}
	server := newTestServer(service)

	result, rpcErr := server.CallTool(context.Background(), "memory_read", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	if service.reads[0] != DefaultEntryPoint {
		t.Fatalf("default path = %q", service.reads[0])
	}

	var payload struct {
		FilePath     string `json:"filePath"`
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if payload.FilePath != "MEMORY.md" || payload.FileRevision != "sha256:abc" || payload.RawContent != "# Memory\n" {
		t.Fatalf("unexpected payload %+v", payload)
	}

	if _, rpcErr := server.CallTool(context.Background(), "memory_read",
		json.RawMessage(`{"path":"notes/topic.md"}`)); rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if service.reads[1] != "notes/topic.md" {
		t.Fatalf("explicit path = %q", service.reads[1])
	}
}

// TestM04PathRejection covers M04: a path the vault rejects surfaces as an
// isError result carrying the contract code, never as repository content.
func TestM04PathRejection(t *testing.T) {
	service := &fakeService{readErr: memerr.New(memerr.InvalidPath, "path %q escapes the repository root", "../etc/passwd")}
	server := newTestServer(service)

	result, rpcErr := server.CallTool(context.Background(), "memory_read",
		json.RawMessage(`{"path":"../etc/passwd"}`))
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatal("want isError for an invalid path")
	}
	if !strings.HasPrefix(result.Content[0].Text, "INVALID_PATH: ") {
		t.Fatalf("unexpected text %q", result.Content[0].Text)
	}
}

// TestM05MemoryWrite covers M05: a managed write records the session identity
// and reports transaction, commit, revision and sync state.
func TestM05MemoryWrite(t *testing.T) {
	service := &fakeService{applyResult: app.MutationResult{
		TransactionID: "tx-1",
		Commit:        "abc123",
		Revision:      "sha256:def",
		SyncState:     "pending",
	}}
	server := newTestServer(service)

	result, rpcErr := server.CallTool(context.Background(), "memory_write",
		json.RawMessage(`{"path":"MEMORY.md","base":"sha256:abc","content":"# Memory\n"}`))
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	write := service.writes[0]
	if write.Agent != "dsh" || write.SessionID != "session-1" || write.Device != "device-1" {
		t.Fatalf("unexpected identity %+v", write)
	}
	if write.Changes[0].ExpectedRevision != "sha256:abc" || write.Changes[0].RawContent != "# Memory\n" {
		t.Fatalf("unexpected change %+v", write.Changes[0])
	}

	var payload struct {
		Transaction string `json:"transaction"`
		Commit      string `json:"commit"`
		Revision    string `json:"revision"`
		Sync        string `json:"sync"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if payload.Transaction != "tx-1" || payload.Commit != "abc123" || payload.Sync != "pending" {
		t.Fatalf("unexpected payload %+v", payload)
	}
}

// TestM06RevisionMismatch covers M06: a stale baseline is reported and not
// retried as if it were a busy lock.
func TestM06RevisionMismatch(t *testing.T) {
	service := &fakeService{applyErrs: []error{
		memerr.New(memerr.RevisionMismatch, "file changed since the baseline"),
	}}
	server := newTestServer(service)

	result, rpcErr := server.CallTool(context.Background(), "memory_write",
		json.RawMessage(`{"path":"MEMORY.md","base":"sha256:old","content":"x"}`))
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "REVISION_MISMATCH: ") {
		t.Fatalf("unexpected result %+v", result)
	}
	if len(service.writes) != 1 {
		t.Fatalf("stale baseline must not be retried, attempts = %d", len(service.writes))
	}
}

// TestM07BusyRetryAndFailures covers M07: one BUSY retry, then the contract
// code; other failures are reported without retrying.
func TestM07BusyRetryAndFailures(t *testing.T) {
	busy := memerr.New(memerr.Busy, "lock is held")
	service := &fakeService{
		applyErrs:   []error{busy, busy},
		applyResult: app.MutationResult{Commit: "abc"},
	}
	server := newTestServer(service)
	result, _ := server.CallTool(context.Background(), "memory_write",
		json.RawMessage(`{"path":"MEMORY.md","base":"absent","content":"x"}`))
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "BUSY: ") {
		t.Fatalf("unexpected result %+v", result)
	}
	if len(service.writes) != writeAttempts {
		t.Fatalf("want %d attempts, got %d", writeAttempts, len(service.writes))
	}

	noRepo := &fakeService{applyErrs: []error{memerr.New(memerr.NoRepo, "no repository at %s", "/tmp/memory")}}
	result, _ = newTestServer(noRepo).CallTool(context.Background(), "memory_write",
		json.RawMessage(`{"path":"MEMORY.md","base":"absent","content":"x"}`))
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "NO_REPO: ") {
		t.Fatalf("unexpected result %+v", result)
	}
	if len(noRepo.writes) != 1 {
		t.Fatalf("NO_REPO must not be retried, attempts = %d", len(noRepo.writes))
	}

	limit := &fakeService{applyErrs: []error{memerr.New(memerr.LimitExceeded, "file exceeds the 2 MiB text limit")}}
	result, _ = newTestServer(limit).CallTool(context.Background(), "memory_write",
		json.RawMessage(`{"path":"MEMORY.md","base":"absent","content":"x"}`))
	if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "LIMIT_EXCEEDED: ") {
		t.Fatalf("unexpected result %+v", result)
	}
}

// TestM05MissingWriteArguments covers the argument validation that keeps a
// call from reaching the application layer without a baseline.
func TestM05MissingWriteArguments(t *testing.T) {
	service := &fakeService{}
	server := newTestServer(service)
	for _, args := range []string{
		`{"base":"absent","content":"x"}`,
		`{"path":"MEMORY.md","content":"x"}`,
	} {
		result, rpcErr := server.CallTool(context.Background(), "memory_write", json.RawMessage(args))
		if rpcErr != nil {
			t.Fatalf("unexpected protocol error for %s: %v", args, rpcErr)
		}
		if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "INVALID_ARGUMENT: ") {
			t.Fatalf("unexpected result for %s: %+v", args, result)
		}
	}
	if len(service.writes) != 0 {
		t.Fatalf("invalid arguments reached the service: %d writes", len(service.writes))
	}
}

// TestM05UnknownTool covers the protocol error for a tool name that is not in
// the list.
func TestM05UnknownTool(t *testing.T) {
	server := newTestServer(&fakeService{})
	_, rpcErr := server.CallTool(context.Background(), "memory_delete", nil)
	if rpcErr == nil || rpcErr.Code != codeInvalidParams {
		t.Fatalf("unexpected protocol error %+v", rpcErr)
	}
}
