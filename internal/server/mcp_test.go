package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gray0128/memctl/internal/mcp"
)

// TestHandleMCPStatusSerializesStatus verifies the response shape of
// GET /api/v1/mcp: one status object with the probe result and one
// registration action per host, including the manual dsh step.
func TestHandleMCPStatusSerializesStatus(t *testing.T) {
	srv := &Server{mcpStatus: func(context.Context) (mcp.Status, error) {
		return mcp.Status{
			Executable: "/opt/memctl",
			Available:  true,
			Version:    "v9.9.9",
			Tools:      []string{"memory_entries", "memory_read", "memory_write"},
			Registrations: []mcp.Registration{
				{Agent: "codex", Command: "codex mcp add memctl -- /opt/memctl mcp --agent codex"},
				{Agent: "dsh", Instruction: "add an entry"},
			},
		}, nil
	}}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil)
	srv.handleMCPStatus(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var payload mcpStatusDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, recorder.Body.String())
	}
	if !payload.Available || payload.Version != "v9.9.9" || len(payload.Tools) != 3 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if len(payload.Registrations) != 2 {
		t.Fatalf("registrations = %+v", payload.Registrations)
	}
	if payload.Registrations[1].Instruction != "add an entry" || payload.Registrations[1].Command != "" {
		t.Fatalf("dsh registration = %+v", payload.Registrations[1])
	}
}

// TestHandleMCPStatusEmptyToolsIsArray keeps the JSON contract stable: an
// unavailable channel reports an empty array, not null, so a client can map
// over it without a guard.
func TestHandleMCPStatusEmptyToolsIsArray(t *testing.T) {
	srv := &Server{mcpStatus: func(context.Context) (mcp.Status, error) {
		return mcp.Status{Executable: "/opt/memctl", Reason: "no handshake"}, nil
	}}

	recorder := httptest.NewRecorder()
	srv.handleMCPStatus(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["tools"]) != "[]" {
		t.Fatalf("tools = %s, want []", raw["tools"])
	}
	if string(raw["registrations"]) != "[]" {
		t.Fatalf("registrations = %s, want []", raw["registrations"])
	}
}

// TestHandleMCPStatusExecutableError covers the environment error branch.
func TestHandleMCPStatusExecutableError(t *testing.T) {
	srv := &Server{mcpStatus: func(context.Context) (mcp.Status, error) {
		return mcp.Status{}, errors.New("no executable")
	}}

	recorder := httptest.NewRecorder()
	srv.handleMCPStatus(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}
