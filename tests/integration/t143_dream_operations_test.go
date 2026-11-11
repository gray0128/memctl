package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
)

// This file verifies T143 (stage I step I-3b of
// docs/05-validation-and-delivery.md, design in
// docs/18-agent-memory-review-and-optimization.md section 4.3.2) through the
// real binary: `dream --prepare` hands the agent bounded toolOutcomes evidence
// and the related existing entries to check, and `--propose` (CLI and MCP
// memory_dream_propose) accepts structured operations[] that memctl assembles
// itself, guarded by the 30-day deletion window, until `--apply` commits the
// result and consumes the cited sessions.

// t143Bundle is the part of the prepare bundle this file reads.
type t143Bundle struct {
	BundleID     string `json:"bundleId"`
	BaseHead     string `json:"baseHead"`
	BaseRevision string `json:"baseRevision"`
	Rules        struct {
		Tasks []string `json:"tasks"`
	} `json:"rules"`
	Sessions []struct {
		SessionID string `json:"sessionId"`
		Agent     string `json:"agent"`
		Turns     []struct {
			ToolOutcomes []string `json:"toolOutcomes"`
		} `json:"turns"`
	} `json:"sessions"`
	RelatedExistingEntries []struct {
		ID         string `json:"id"`
		FilePath   string `json:"filePath"`
		LineNumber int    `json:"lineNumber"`
		FactText   string `json:"factText"`
		RawLine    string `json:"rawLine"`
		AddedDate  string `json:"addedDate"`
	} `json:"relatedExistingEntries"`
}

// t143Proposal is the part of the propose result this file reads.
type t143Proposal struct {
	ProposalID           string `json:"proposalId"`
	Status               string `json:"status"`
	Runner               string `json:"runner"`
	PatchHash            string `json:"patchHash"`
	NewMemoriesExtracted int    `json:"newMemoriesExtracted"`
}

// writeT143Digest seeds one digest whose troubleshooting turn carries a failed
// tool outcome that contradicts an existing entry.
func writeT143Digest(t *testing.T, home, sid, cwd string) {
	t.Helper()
	date := h3Day(0)
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	digest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "claude-code",
		SessionID:     sid,
		SourceTag:     "agent://" + deviceName() + "/claude-code/" + sid,
		Date:          date,
		UpdatedAt:     date + "T10:00:00Z",
		CwdBase:       corpus.CwdBase(cwd),
		CwdKey:        corpus.CwdKey(cwd),
		Turns: []corpus.Turn{{
			User:         "排查 pool_timeout 连接池超时",
			Tools:        []string{"Bash: psql -c 'show pool_timeout'"},
			ToolOutcomes: []string{"Bash: psql -c 'show pool_timeout' -> failed: ERROR: unrecognized configuration parameter pool_timeout"},
			Assistant:    "根因：连接池超时参数应为 statement_timeout。",
		}},
	}
	if err := cache.Save(digest); err != nil {
		t.Fatalf("save digest: %v", err)
	}
}

// runT143Propose submits one propose input through the CLI.
func runT143Propose(t *testing.T, bin, home, repo string, input map[string]any) cliResult {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encode proposal: %v", err)
	}
	return runMemctlStdin(t, bin, repo, h3Env(home), string(encoded), "dream", "--propose", "--stdin")
}

func TestT143StructuredOperationsDreamLoopCoversI3b(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	env := h3Env(home)
	project := filepath.Join(t.TempDir(), "db-service")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	// The expired entry is five days past its until date, so it is inside the
	// 30-day hidden period and must not be removed yet.
	expired := "- 临时冻结迁移 [source: test; added: " + h3Day(-40) + "; until: " + h3Day(-5) + "]"
	notes := "# DB\n\n" +
		"- 连接池超时参数是 pool_timeout [source: test; added: " + h3Day(-30) + "]\n" +
		"- 数据库主机是 db.internal [source: test; added: " + h3Day(-30) + "]\n" +
		expired + "\n"
	if result := runMemctlStdin(t, bin, home, env, notes, "write", "notes/db-service.md", "--base", "absent", "--stdin"); result.code != 0 {
		t.Fatalf("seed notes exited %d: %s", result.code, result.stderr)
	}
	writeT143Digest(t, home, "sess-db", project)

	// 1. prepare carries toolOutcomes, the recalled entries and the tasks.
	result := runMemctl(t, bin, project, env, "dream", "--prepare")
	if result.code != 0 {
		t.Fatalf("dream --prepare exited %d: %s", result.code, result.stderr)
	}
	var bundle t143Bundle
	if err := json.Unmarshal([]byte(result.stdout), &bundle); err != nil {
		t.Fatalf("decode bundle: %v (%s)", err, result.stdout)
	}
	if len(bundle.Sessions) != 1 || len(bundle.Sessions[0].Turns) != 1 || len(bundle.Sessions[0].Turns[0].ToolOutcomes) != 1 {
		t.Fatalf("sessions = %+v, want the digest with its toolOutcomes", bundle.Sessions)
	}
	if !strings.Contains(strings.Join(bundle.Rules.Tasks, "\n"), "relatedExistingEntries") {
		t.Fatalf("rules.tasks = %q", bundle.Rules.Tasks)
	}
	wrongID := ""
	for _, entry := range bundle.RelatedExistingEntries {
		if strings.Contains(entry.FactText, "临时冻结迁移") {
			t.Fatal("an expired entry was recalled")
		}
		if strings.Contains(entry.FactText, "pool_timeout") && entry.FilePath == "notes/db-service.md" {
			wrongID = entry.ID
		}
	}
	if wrongID == "" || len(bundle.RelatedExistingEntries) > 20 {
		t.Fatalf("relatedExistingEntries = %+v", bundle.RelatedExistingEntries)
	}

	base := func(ops ...map[string]any) map[string]any {
		return map[string]any{
			"bundleId":     bundle.BundleID,
			"baseHead":     bundle.BaseHead,
			"baseRevision": bundle.BaseRevision,
			"operations":   ops,
		}
	}

	// 2. guards: mixing channels and a premature delete are refused.
	mixed := base(map[string]any{"op": "append_entry", "path": "notes/db-service.md", "fact": "x", "sessionIndices": []int{0}})
	mixed["files"] = []map[string]any{{"path": "MEMORY.md", "action": "update", "content": "# Memory\n"}}
	if refused := runT143Propose(t, bin, home, repo, mixed); refused.code == 0 || !strings.Contains(refused.stderr, "INVALID_ARGUMENT") {
		t.Fatalf("mixed channels: exit %d, stderr %s", refused.code, refused.stderr)
	}
	premature := base(map[string]any{"op": "delete_entry", "path": "notes/db-service.md", "targetRawLine": expired, "reason": "lifted"})
	if refused := runT143Propose(t, bin, home, repo, premature); refused.code == 0 || !strings.Contains(refused.stderr, "DELETION_DEFERRED") {
		t.Fatalf("premature delete: exit %d, stderr %s", refused.code, refused.stderr)
	}

	// 3. append + replace + delete through the CLI, then apply.
	proposed := runT143Propose(t, bin, home, repo, base(
		map[string]any{"op": "append_entry", "path": "notes/release.md", "heading": "规则", "fact": "发布只推标签", "indexSummary": "发布规则", "sessionIndices": []int{0}},
		map[string]any{"op": "replace_entry", "path": "notes/db-service.md", "targetEntryId": wrongID, "fact": "连接池超时参数应为 statement_timeout", "sessionIndices": []int{0}},
		map[string]any{"op": "delete_entry", "path": "notes/db-service.md", "targetRawLine": "- 数据库主机是 db.internal [source: test; added: " + h3Day(-30) + "]", "reason": "host moved"},
	))
	if proposed.code != 0 {
		t.Fatalf("dream --propose exited %d: %s", proposed.code, proposed.stderr)
	}
	var proposal t143Proposal
	if err := json.Unmarshal([]byte(proposed.stdout), &proposal); err != nil {
		t.Fatalf("decode proposal: %v (%s)", err, proposed.stdout)
	}
	if proposal.Status != "ready" || proposal.Runner != "local-agent" || proposal.PatchHash == "" || proposal.NewMemoriesExtracted != 2 {
		t.Fatalf("proposal = %+v", proposal)
	}
	applied := runMemctl(t, bin, repo, env, "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
	if applied.code != 0 {
		t.Fatalf("dream --apply exited %d: %s", applied.code, applied.stderr)
	}
	tag := "agent://" + deviceName() + "/claude-code/sess-db"
	db := runMemctl(t, bin, repo, env, "read", "notes/db-service.md")
	wantDB := "# DB\n\n- 连接池超时参数应为 statement_timeout [source: " + tag + "; added: " + h3Day(0) + "]\n"
	if db.code != 0 || !strings.HasPrefix(db.stdout, wantDB) || strings.Contains(db.stdout, "db.internal") {
		t.Fatalf("notes/db-service.md after apply:\n%s", db.stdout)
	}
	release := runMemctl(t, bin, repo, env, "read", "notes/release.md")
	if release.code != 0 || !strings.Contains(release.stdout, "- 发布只推标签 [source: "+tag+"; added: "+h3Day(0)+"]") {
		t.Fatalf("notes/release.md after apply:\n%s", release.stdout)
	}
	memory := runMemctl(t, bin, repo, env, "read", "MEMORY.md")
	if memory.code != 0 || strings.Count(memory.stdout, "[[notes/release]] — 发布规则") != 1 {
		t.Fatalf("MEMORY.md after apply:\n%s", memory.stdout)
	}
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(config.Canonical(repo)))
	if digest, ok := cache.Load("claude-code", "sess-db"); !ok || digest.ConsumedBy != proposal.ProposalID {
		t.Fatalf("the cited digest was not consumed: %+v", digest)
	}

	// 4. the same channel through MCP memory_dream_prepare / propose.
	writeT143Digest(t, home, "sess-mcp", project)
	responses, stderr := runMCPSession(t, bin, project, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_dream_prepare","arguments":{}}}`)
	if stderr != "" {
		t.Fatalf("mcp stderr: %s", stderr)
	}
	prepared := decodeCallResult(t, responses[0])
	if prepared.IsError {
		t.Fatalf("memory_dream_prepare failed: %s", prepared.Content[0].Text)
	}
	var mcpBundle t143Bundle
	if err := json.Unmarshal([]byte(prepared.Content[0].Text), &mcpBundle); err != nil {
		t.Fatalf("decode mcp bundle: %v", err)
	}
	if len(mcpBundle.Sessions) != 1 || mcpBundle.Sessions[0].SessionID != "sess-mcp" || len(mcpBundle.RelatedExistingEntries) == 0 {
		t.Fatalf("mcp bundle sessions = %+v related = %+v", mcpBundle.Sessions, mcpBundle.RelatedExistingEntries)
	}
	args, _ := json.Marshal(map[string]any{
		"bundleId":     mcpBundle.BundleID,
		"baseHead":     mcpBundle.BaseHead,
		"baseRevision": mcpBundle.BaseRevision,
		"operations": []map[string]any{
			{"op": "append_entry", "path": "notes/db-service.md", "fact": "statement_timeout 以毫秒为单位", "indexSummary": "数据库服务", "sessionIndices": []int{0}},
		},
	})
	responses, stderr = runMCPSession(t, bin, project, env,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memory_dream_propose","arguments":`+string(args)+`}}`)
	if stderr != "" {
		t.Fatalf("mcp stderr: %s", stderr)
	}
	proposedMCP := decodeCallResult(t, responses[0])
	if proposedMCP.IsError {
		t.Fatalf("memory_dream_propose failed: %s", proposedMCP.Content[0].Text)
	}
	var mcpProposal t143Proposal
	if err := json.Unmarshal([]byte(proposedMCP.Content[0].Text), &mcpProposal); err != nil {
		t.Fatalf("decode mcp proposal: %v", err)
	}
	applied = runMemctl(t, bin, repo, env, "dream", "--apply", mcpProposal.ProposalID, "--patch-hash", mcpProposal.PatchHash)
	if applied.code != 0 {
		t.Fatalf("dream --apply (mcp proposal) exited %d: %s", applied.code, applied.stderr)
	}
	db = runMemctl(t, bin, repo, env, "read", "notes/db-service.md")
	if !strings.Contains(db.stdout, "- statement_timeout 以毫秒为单位 [source: agent://"+deviceName()+"/claude-code/sess-mcp; added: "+h3Day(0)+"]") {
		t.Fatalf("notes/db-service.md after the MCP apply:\n%s", db.stdout)
	}
	memory = runMemctl(t, bin, repo, env, "read", "MEMORY.md")
	if strings.Count(memory.stdout, "[[notes/db-service]] — 数据库服务") != 1 {
		t.Fatalf("MEMORY.md was not indexed exactly once:\n%s", memory.stdout)
	}
}
