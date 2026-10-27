package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// fakeService records calls and returns scripted results.
type fakeService struct {
	readResult    vault.FileRead
	readErr       error
	applyResult   app.MutationResult
	applyErrs     []error
	reads         []string
	writes        []app.MutationRequest
	entryPage     app.EntryPage
	entryErr      error
	entryQueries  []app.EntryQuery
	prepareResult dream.DreamPrepareBundle
	prepareErr    error
	prepareReqs   []dream.PrepareRequest
	proposeResult dream.ProposeResult
	proposeErr    error
	proposeInputs []dream.DreamProposeInput
}

func (f *fakeService) DreamPrepareLocal(_ context.Context, req dream.PrepareRequest) (dream.DreamPrepareBundle, error) {
	f.prepareReqs = append(f.prepareReqs, req)
	return f.prepareResult, f.prepareErr
}

func (f *fakeService) DreamProposeLocal(_ context.Context, input dream.DreamProposeInput) (dream.ProposeResult, error) {
	f.proposeInputs = append(f.proposeInputs, input)
	return f.proposeResult, f.proposeErr
}

func (f *fakeService) ListEntries(_ context.Context, query app.EntryQuery) (app.EntryPage, error) {
	f.entryQueries = append(f.entryQueries, query)
	return f.entryPage, f.entryErr
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

// TestM02ToolsList covers M02: exactly the documented memory tools, with
// schemas. Stage H adds the two local Dreaming tools and no apply tool.
func TestM02ToolsList(t *testing.T) {
	server := newTestServer(&fakeService{})
	tools := server.Tools()
	if len(tools) != 5 {
		t.Fatalf("want 5 tools, got %d", len(tools))
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	want := []string{"memory_read", "memory_entries", "memory_dream_prepare", "memory_dream_propose", "memory_write"}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("tool %d = %q, want %q (all: %v)", index, names[index], name, names)
		}
	}
	for _, tool := range tools {
		if tool.Name == "memory_dream_apply" {
			t.Fatal("an apply tool must never be registered over MCP")
		}
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
	if got := ToolNames(); len(got) != 5 {
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

// TestM05SessionArgument covers the provenance fallback: a server started
// without --sid — the DSH profile — accepts the session id the caller read from
// the injected source tag, while a configured session always wins.
func TestM05SessionArgument(t *testing.T) {
	t.Run("call supplies the session", func(t *testing.T) {
		service := &fakeService{applyResult: app.MutationResult{Commit: "abc"}}
		server := NewServer(service, Session{Agent: "dsh", Device: "device-1"})
		result, rpcErr := server.CallTool(context.Background(), "memory_write",
			json.RawMessage(`{"path":"MEMORY.md","base":"sha256:abc","content":"x","sid":" session-4c79 "}`))
		if rpcErr != nil || result.IsError {
			t.Fatalf("unexpected failure: %v %+v", rpcErr, result)
		}
		if service.writes[0].SessionID != "session-4c79" {
			t.Fatalf("session = %q, want the trimmed call value", service.writes[0].SessionID)
		}
	})

	t.Run("configured session wins", func(t *testing.T) {
		service := &fakeService{applyResult: app.MutationResult{Commit: "abc"}}
		server := newTestServer(service)
		if _, rpcErr := server.CallTool(context.Background(), "memory_write",
			json.RawMessage(`{"path":"MEMORY.md","base":"sha256:abc","content":"x","sid":"other"}`)); rpcErr != nil {
			t.Fatalf("unexpected protocol error: %v", rpcErr)
		}
		if service.writes[0].SessionID != "session-1" {
			t.Fatalf("session = %q, want the configured session", service.writes[0].SessionID)
		}
	})

	for name, sid := range map[string]string{
		"control character": "session-1\nAgent: evil",
		"oversized":         strings.Repeat("s", maxSessionArgumentBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			service := &fakeService{}
			server := NewServer(service, Session{Agent: "dsh", Device: "device-1"})
			args, _ := json.Marshal(map[string]string{
				"path": "MEMORY.md", "base": "sha256:abc", "content": "x", "sid": sid,
			})
			result, rpcErr := server.CallTool(context.Background(), "memory_write", args)
			if rpcErr != nil {
				t.Fatalf("unexpected protocol error: %v", rpcErr)
			}
			if !result.IsError || !strings.HasPrefix(result.Content[0].Text, "INVALID_ARGUMENT: ") {
				t.Fatalf("unexpected result %+v", result)
			}
			if len(service.writes) != 0 {
				t.Fatalf("an invalid sid must not reach the transaction, writes = %d", len(service.writes))
			}
		})
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

// TestM13MemoryEntries covers M13: the read-only entry query is exposed as a
// tool so an agent behind a host sandbox can locate a fact without reading every
// file. It must forward every filter unchanged and report contract failures as
// tool errors instead of protocol errors.
func TestM13MemoryEntries(t *testing.T) {
	page := app.EntryPage{
		Revision: "sha256:rev",
		Total:    1,
		Items: []app.MemoryEntryDTO{{
			ID:         "sha256:entry",
			FilePath:   "notes/prefs.md",
			LineNumber: 3,
			FactText:   "editor preference is Vim",
			AddedDate:  "2026-10-19",
		}},
	}
	service := &fakeService{entryPage: page}
	server := newTestServer(service)

	result, rpcErr := server.CallTool(context.Background(), "memory_entries", json.RawMessage(
		`{"q":"vim","file":"notes/prefs.md","agent":"dsh","device":"mac","from":"2026-01-01","to":"2026-12-31","limit":5,"offset":2}`))
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	want := app.EntryQuery{
		Q: "vim", File: "notes/prefs.md", Agent: "dsh", Device: "mac",
		From: "2026-01-01", To: "2026-12-31", Limit: 5, Offset: 2,
	}
	if len(service.entryQueries) != 1 || service.entryQueries[0] != want {
		t.Fatalf("query = %+v, want %+v", service.entryQueries, want)
	}

	var decoded app.EntryPage
	if err := json.Unmarshal([]byte(result.Content[0].Text), &decoded); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if decoded.Total != 1 || decoded.Revision != "sha256:rev" || decoded.Items[0].FactText != "editor preference is Vim" {
		t.Fatalf("unexpected payload %+v", decoded)
	}

	// An omitted payload means "everything" and is forwarded as zero values.
	if _, rpcErr := server.CallTool(context.Background(), "memory_entries", nil); rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if service.entryQueries[1] != (app.EntryQuery{}) {
		t.Fatalf("bare call query = %+v, want zero query", service.entryQueries[1])
	}

	// A negative limit is rejected before the service is called.
	before := len(service.entryQueries)
	rejected, rpcErr := server.CallTool(context.Background(), "memory_entries", json.RawMessage(`{"limit":-1}`))
	if rpcErr != nil || !rejected.IsError {
		t.Fatalf("negative limit: rpcErr=%v result=%+v, want a tool error", rpcErr, rejected)
	}
	if len(service.entryQueries) != before {
		t.Fatalf("negative limit reached the service")
	}

	// A contract failure is reported to the model with its code, not as a
	// protocol error, so the agent can react.
	failing := &fakeService{entryErr: memerr.New(memerr.Busy, "repository lock is held")}
	if result, rpcErr := newTestServer(failing).CallTool(context.Background(), "memory_entries", nil); rpcErr != nil || !result.IsError {
		t.Fatalf("contract failure: rpcErr=%v result=%+v", rpcErr, result)
	} else if !strings.Contains(result.Content[0].Text, "BUSY") {
		t.Fatalf("tool error does not carry the contract code: %q", result.Content[0].Text)
	}
}

// TestMemoryReadReportsHiddenExpired covers M03's expiry clause: the tool
// forwards the read result unchanged, so a projected file carries its
// hiddenExpired count in the same JSON the CLI prints.
func TestMemoryReadReportsHiddenExpired(t *testing.T) {
	service := &fakeService{readResult: vault.FileRead{
		RelPath:       "MEMORY.md",
		FileRevision:  "sha256:rev",
		RawContent:    "# Memory\n\n- active\n- " + vault.ExpiredFactPlaceholder + " [until: 2000-01-01]\n",
		HiddenExpired: 1,
	}}

	result, rpcErr := newTestServer(service).CallTool(context.Background(), "memory_read", nil)
	if rpcErr != nil {
		t.Fatalf("memory_read: %+v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("memory_read failed: %+v", result)
	}
	var payload struct {
		FileRevision  string `json:"fileRevision"`
		RawContent    string `json:"rawContent"`
		HiddenExpired int    `json:"hiddenExpired"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.HiddenExpired != 1 || payload.FileRevision != "sha256:rev" {
		t.Fatalf("payload = %+v", payload)
	}
	if !strings.Contains(payload.RawContent, vault.ExpiredFactPlaceholder) {
		t.Fatalf("placeholder missing:\n%s", payload.RawContent)
	}
}
