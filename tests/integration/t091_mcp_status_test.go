package integration_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/mcp"
)

// This file verifies T091: the 客户端接入 view needs the memctl MCP channel
// status, so GET /api/v1/mcp reports the same probe result and registration
// actions as the `mcp:` block of `memctl install --status`. See
// docs/09-mcp-memory-tools.md sections 5.2 and 5.3 and
// docs/02-domain-models-and-contracts.md section 4.2.

type t091MCPStatus struct {
	Executable    string   `json:"executable"`
	Available     bool     `json:"available"`
	Version       string   `json:"version"`
	Tools         []string `json:"tools"`
	Reason        string   `json:"reason"`
	Registrations []struct {
		Agent       string `json:"agent"`
		Command     string `json:"command"`
		Instruction string `json:"instruction"`
	} `json:"registrations"`
}

// TestT091MCPStatusRoute covers the route contract: the status object carries
// the executable, the served tools and one action per host, and a host without
// a CLI carries its manual instruction instead of a command.
func TestT091MCPStatusRoute(t *testing.T) {
	fixture := newB1Fixture(t, false)

	res := fixture.get(t, "/api/v1/mcp")
	mustStatus(t, res.status, http.StatusOK)

	var status t091MCPStatus
	if err := json.Unmarshal(res.body, &status); err != nil {
		t.Fatalf("decode mcp status: %v\n%s", err, res.body)
	}
	if status.Executable != "/opt/memctl" {
		t.Fatalf("mcp status executable = %q", status.Executable)
	}
	if !status.Available {
		t.Fatalf("fixture binary should be reported as serving, reason=%q", status.Reason)
	}
	for _, tool := range mcp.ToolNames() {
		found := false
		for _, served := range status.Tools {
			if served == tool {
				found = true
			}
		}
		if !found {
			t.Fatalf("mcp status tools = %v, missing %s", status.Tools, tool)
		}
	}
	if len(status.Registrations) != 11 {
		t.Fatalf("expected one registration per adapter, got %d", len(status.Registrations))
	}
	for _, registration := range status.Registrations {
		// dsh and WorkBuddy have no host CLI, so they carry an instruction
		// instead of a command that quotes the probed executable. Delta has a
		// CLI but accepts no local stdio server at all, so its instruction must
		// deny the registration rather than offer a command.
		if registration.Agent == "dsh" || registration.Agent == "workbuddy" || registration.Agent == "delta" {
			if registration.Command != "" || registration.Instruction == "" {
				t.Fatalf("%s registration = %+v", registration.Agent, registration)
			}
			continue
		}
		if !strings.Contains(registration.Command, status.Executable) {
			t.Fatalf("%s registration does not use the probed executable: %q", registration.Agent, registration.Command)
		}
	}
}

// TestT091MCPStatusReadOnly covers the read-only contract: the status route is
// a GET and the UI can therefore render it from a `--readonly` session.
func TestT091MCPStatusReadOnly(t *testing.T) {
	fixture := newB1Fixture(t, true)

	mustStatus(t, fixture.get(t, "/api/v1/mcp").status, http.StatusOK)

	// The read-only middleware rejects a write verb before routing, which is
	// the documented A03 behavior.
	res := fixture.do(t, http.MethodPost, "http://127.0.0.1:18420/api/v1/mcp", b1Token, "127.0.0.1:18420", "")
	mustStatus(t, res.status, http.StatusForbidden)
	mustCode(t, errorCode(t, res), "READ_ONLY")
}

// TestT091MCPStatusWritesNothing covers the boundary that the route only
// probes: reading the status must not create host configuration.
func TestT091MCPStatusWritesNothing(t *testing.T) {
	fixture := newB1Fixture(t, false)

	mustStatus(t, fixture.get(t, "/api/v1/mcp").status, http.StatusOK)
	entries, err := os.ReadDir(fixture.home)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read home: %v", err)
	}
	hosts := map[string]bool{
		".claude": true, ".codex": true, ".pi": true,
		".grok": true, ".gemini": true, ".commandcode": true,
	}
	for _, entry := range entries {
		if hosts[entry.Name()] {
			t.Fatalf("mcp status wrote host configuration %q", entry.Name())
		}
	}
}
