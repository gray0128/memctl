package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
)

// This file verifies the two local Dreaming tools of stage H-3 over the MCP
// protocol: memory_dream_prepare returns the bundle and passes the caller's
// session identity through, memory_dream_propose forwards the structured input
// and returns the persisted proposal, and no apply tool exists. The transport
// carries one JSON-RPC response per request, exactly like a host client would
// read it. See docs/09-mcp-memory-tools.md section 4.4.

// TestDreamToolsAreRegisteredAndHaveNoApply covers the documented tool set: the
// prepare and propose tools are listed, the apply tool never is.
func TestDreamToolsAreRegisteredAndHaveNoApply(t *testing.T) {
	server := newTestServer(&fakeService{})
	var names []string
	for _, tool := range server.Tools() {
		names = append(names, tool.Name)
		if tool.Name == "memory_dream_apply" {
			t.Fatal("an apply tool must never be registered over MCP")
		}
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"memory_dream_prepare", "memory_dream_propose"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tool %s is missing from %v", want, names)
		}
	}
}

// TestDreamPrepareForwardsSessionIdentity checks that the host session identity
// reaches the application service, so the calling session's digest is refreshed
// before the sweep.
func TestDreamPrepareForwardsSessionIdentity(t *testing.T) {
	service := &fakeService{prepareResult: dream.DreamPrepareBundle{
		Today:               "2026-10-20",
		BaseHead:            "abc123",
		BaseRevision:        "sha256:def",
		RedactionNotice:     "notice",
		RemainingUnconsumed: 3,
		Sessions: []dream.BundleSession{{
			SessionIndex: 0,
			Agent:        "claude-code",
			SourceTag:    "agent://device/claude-code/sid",
			Date:         "2026-10-20",
		}},
	}}
	server := newTestServer(service)

	result, rpcErr := server.CallTool(context.Background(), "memory_dream_prepare",
		json.RawMessage(`{"project":"memctl","maxBytes":4096}`))
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	if len(service.prepareReqs) != 1 {
		t.Fatalf("prepare calls = %d", len(service.prepareReqs))
	}
	req := service.prepareReqs[0]
	if req.Project != "memctl" || req.MaxBytes != 4096 {
		t.Fatalf("request = %+v", req)
	}
	if req.Agent != "dsh" || req.SID != "session-1" || req.Device != "device-1" {
		t.Fatalf("session identity not forwarded: %+v", req)
	}

	var bundle dream.DreamPrepareBundle
	if err := json.Unmarshal([]byte(result.Content[0].Text), &bundle); err != nil {
		t.Fatalf("result is not the bundle JSON: %v", err)
	}
	if bundle.BaseHead != "abc123" || bundle.RemainingUnconsumed != 3 || len(bundle.Sessions) != 1 {
		t.Fatalf("bundle = %+v", bundle)
	}
}

// TestDreamPrepareReportsContractErrors checks the documented error mapping: a
// contract failure is an isError result the model can read, not a protocol
// error.
func TestDreamPrepareReportsContractErrors(t *testing.T) {
	service := &fakeService{prepareErr: memerr.New(memerr.ExternalChanges, "the repository has changes")}
	server := newTestServer(service)
	result, rpcErr := server.CallTool(context.Background(), "memory_dream_prepare", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "EXTERNAL_CHANGES") {
		t.Fatalf("result = %+v", result)
	}

	negative := &fakeService{}
	result, rpcErr = newTestServer(negative).CallTool(context.Background(), "memory_dream_prepare",
		json.RawMessage(`{"maxBytes":-1}`))
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if !result.IsError || len(negative.prepareReqs) != 0 {
		t.Fatalf("negative maxBytes was forwarded: %+v", result)
	}
}

// TestDreamProposeForwardsStructuredInput checks that every documented member
// reaches the service and that the response is the persisted proposal.
func TestDreamProposeForwardsStructuredInput(t *testing.T) {
	service := &fakeService{proposeResult: dream.ProposeResult{
		ProposalID:           "dp-00000000000000000000000000000001",
		Status:               "ready",
		Runner:               "local-agent",
		PatchHash:            "sha256:patch",
		SessionsScanned:      2,
		NewMemoriesExtracted: 1,
	}}
	server := newTestServer(service)
	params := json.RawMessage(`{
		"baseHead": "abc123",
		"baseRevision": "sha256:def",
		"newMemories": [{
			"kind": "state",
			"targetPath": "notes/release.md",
			"fact": "release by tag",
			"sessionIndices": [0, 1],
			"rationale": "the user corrected the flow"
		}],
		"files": [{"path": "notes/release.md", "action": "create", "content": "# Release\n"}],
		"warnings": ["review the ordering"]
	}`)
	result, rpcErr := server.CallTool(context.Background(), "memory_dream_propose", params)
	if rpcErr != nil {
		t.Fatalf("unexpected protocol error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	if len(service.proposeInputs) != 1 {
		t.Fatalf("propose calls = %d", len(service.proposeInputs))
	}
	input := service.proposeInputs[0]
	if input.BaseHead != "abc123" || input.BaseRevision != "sha256:def" {
		t.Fatalf("baseline not forwarded: %+v", input)
	}
	if len(input.NewMemories) != 1 || len(input.NewMemories[0].SessionIndices) != 2 {
		t.Fatalf("candidates not forwarded: %+v", input.NewMemories)
	}
	if input.NewMemories[0].Fact != "release by tag" || input.NewMemories[0].TargetPath != "notes/release.md" {
		t.Fatalf("candidate = %+v", input.NewMemories[0])
	}
	if len(input.Files) != 1 || input.Files[0].Action != "create" {
		t.Fatalf("files not forwarded: %+v", input.Files)
	}
	if len(input.Warnings) != 1 {
		t.Fatalf("warnings not forwarded: %+v", input.Warnings)
	}

	var proposal dream.ProposeResult
	if err := json.Unmarshal([]byte(result.Content[0].Text), &proposal); err != nil {
		t.Fatalf("result is not the proposal JSON: %v", err)
	}
	if proposal.Runner != "local-agent" || proposal.PatchHash != "sha256:patch" {
		t.Fatalf("proposal = %+v", proposal)
	}
}

// TestDreamProposeRequiresBaseline checks the documented required fields before
// any application call.
func TestDreamProposeRequiresBaseline(t *testing.T) {
	service := &fakeService{}
	server := newTestServer(service)
	for _, params := range []string{
		`{"newMemories":[],"files":[{"path":"topic.md","action":"update","content":"x"}]}`,
		`{"baseHead":"abc","files":[{"path":"topic.md","action":"update","content":"x"}]}`,
	} {
		result, rpcErr := server.CallTool(context.Background(), "memory_dream_propose", json.RawMessage(params))
		if rpcErr != nil {
			t.Fatalf("unexpected protocol error: %v", rpcErr)
		}
		if !result.IsError || !strings.Contains(result.Content[0].Text, "INVALID_ARGUMENT") {
			t.Fatalf("result = %+v", result)
		}
	}
	if len(service.proposeInputs) != 0 {
		t.Fatalf("an invalid proposal reached the service: %+v", service.proposeInputs)
	}

}

// TestDreamToolsRefuseWrongArgumentTypes checks that a wrong argument type is a
// protocol error instead of being guessed, matching the other tools.
func TestDreamToolsRefuseWrongArgumentTypes(t *testing.T) {
	service := &fakeService{}
	server := newTestServer(service)
	if _, rpcErr := server.CallTool(context.Background(), "memory_dream_prepare",
		json.RawMessage(`{"maxBytes":"a lot"}`)); rpcErr == nil || rpcErr.Code != codeInvalidParams {
		t.Fatalf("a mistyped maxBytes was accepted: %+v", rpcErr)
	}
	if _, rpcErr := server.CallTool(context.Background(), "memory_dream_propose",
		json.RawMessage(`{"baseHead":1}`)); rpcErr == nil || rpcErr.Code != codeInvalidParams {
		t.Fatalf("a mistyped baseHead was accepted: %+v", rpcErr)
	}
}
