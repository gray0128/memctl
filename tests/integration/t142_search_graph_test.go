package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/server"
	"github.com/gray0128/memctl/internal/vault"
)

// TestT142TokenizedBM25RelatedFilesAndTranscriptsCoversI3a verifies stage I-3a (T142):
//  1. Pure Go in-memory tokenization (English multi-word out-of-order, compound
//     identifiers, CJK unigrams + bigrams) and 4-field weighted BM25 + recency
//     ranking with tiered AND / >=50% OR recall and stable limit/offset paging.
//  2. One-hop wiki-link graph hints (`relatedFiles`: outgoing, backlink, both,
//     with `MEMORY.md` `## Index` summary extraction) on CLI `entries --json` /
//     `read --json`, MCP `memory_entries` / `memory_read`, and HTTP
//     `GET /api/v1/entries` / `GET /api/v1/files/{path...}`.
//  3. `--project <name>` filtering and `--include-transcripts` recent (<= 14d)
//     SessionDigest search (with >14d filtering and `corpus.Redact` safety net)
//     across CLI, HTTP, and MCP.
//  4. Lock-free branch snapshot fallback (`BUSY` lock held) preserving tokenized
//     BM25 search, `--project` filtering, and `relatedFiles`.
func TestT142TokenizedBM25RelatedFilesAndTranscriptsCoversI3a(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatalf("create notes dir: %v", err)
	}

	memContent := strings.Join([]string{
		"# Memory",
		"",
		"## Working preferences",
		"- Keep commits small and atomic [source: agent://dev/claude/s0; added: 2026-04-01]",
		"",
		"## Index",
		"- [[notes/atlas]] — Atlas core service",
		"- [[notes/ATLAS-db]] — Atlas database layer",
		"- [[notes/runbook]] — Deployment runbook [project: atlas]",
		"- [[notes/other]] — Unrelated frontend notes [project: web]",
		"",
	}, "\n")
	a4WriteFile(t, bin, home, repo, env, "MEMORY.md", memContent)

	// notes/atlas.md links to [[notes/ATLAS-db]] and [[notes/runbook]].
	a4WriteFile(t, bin, home, repo, env, "notes/atlas.md", strings.Join([]string{
		"# Atlas",
		"",
		"## Git Workflow",
		"- push release via git tag after CI passes [[notes/runbook]] [source: agent://dev/claude/s1; added: 2026-04-10]",
		"- only push release branch for staging [[notes/ATLAS-db]] [source: agent://dev/claude/s1; added: 2026-04-05]",
		"- expired tag release rule [source: agent://dev/claude/s1; added: 2020-01-01; until: 2020-01-02]",
		"",
	}, "\n"))

	// notes/ATLAS-db.md links back to [[notes/atlas]], creating a bidirectional ("both") link with notes/atlas.md.
	a4WriteFile(t, bin, home, repo, env, "notes/ATLAS-db.md", strings.Join([]string{
		"# Atlas DB",
		"",
		"## Schema",
		"- 通过镜像标签完成灰度发布与数据库迁移 [[notes/atlas]] [source: agent://dev/claude/s2; added: 2026-04-18]",
		"",
	}, "\n"))

	// notes/runbook.md has a heading match for "Tag" vs fact match to test field weights and recency.
	a4WriteFile(t, bin, home, repo, env, "notes/runbook.md", strings.Join([]string{
		"# Runbook",
		"",
		"## Tag Release",
		"- verify checksum before rollout [source: agent://dev/claude/s3; added: 2026-04-19]",
		"",
	}, "\n"))

	a4WriteFile(t, bin, home, repo, env, "notes/other.md", strings.Join([]string{
		"# Other",
		"",
		"## UI",
		"- frontend button style tokens [source: agent://dev/claude/s4; added: 2026-04-12]",
		"",
	}, "\n"))

	// Seed corpus cache with one recent (<= 14d) digest for project "atlas"
	// (including a secret token to verify corpus.Redact safety net) and one stale
	// (> 14d) digest that must be ignored.
	stateDir := filepath.Join(home, ".local", "state", "memctl")
	for _, kv := range env {
		if after, ok := strings.CutPrefix(kv, "AGENT_MEMORY_STATE="); ok && after != "" {
			stateDir = after
		}
	}
	now := time.Now().UTC()
	cache := corpus.OpenCache(stateDir, config.RepoID(config.Canonical(repo)))
	recentDigest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "claude",
		SessionID:     "sid-recent-1",
		SourceTag:     "agent://dev/claude/sid-recent-1",
		Date:          now.Add(-2 * 24 * time.Hour).Format("2006-01-02"),
		UpdatedAt:     now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
		CwdBase:       "atlas",
		CwdKey:        corpus.CwdKey("/workspace/atlas"),
		Turns: []corpus.Turn{
			{
				User:      "How do we fix the pgbouncer connection pool timeout in atlas?",
				Assistant: "Increase pool_mode transaction timeout and rotate token sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789.",
				Tools:     []string{"bash: psql -c 'show pool_timeout' -> 0"},
			},
		},
	}
	if err := cache.Save(recentDigest); err != nil {
		t.Fatalf("save recent digest: %v", err)
	}
	staleDigest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "claude",
		SessionID:     "sid-stale-old",
		SourceTag:     "agent://dev/claude/sid-stale-old",
		Date:          now.Add(-20 * 24 * time.Hour).Format("2006-01-02"),
		UpdatedAt:     now.Add(-20 * 24 * time.Hour).Format(time.RFC3339),
		CwdBase:       "atlas",
		CwdKey:        corpus.CwdKey("/workspace/atlas"),
		Turns: []corpus.Turn{
			{
				User:      "Old pgbouncer timeout discussion from 20 days ago",
				Assistant: "Should be ignored by 14d window",
			},
		},
	}
	if err := cache.Save(staleDigest); err != nil {
		t.Fatalf("save stale digest: %v", err)
	}

	// 1. CLI multi-word out-of-order & compound query (`release-tag` / `tag release`) + pagination + relatedFiles.
	cliSearch := runMemctl(t, bin, home, env, "entries", "--q", "release-tag", "--json")
	if cliSearch.code != 0 {
		t.Fatalf("entries --q release-tag failed: code=%d stderr=%s", cliSearch.code, cliSearch.stderr)
	}
	var searchPage app.EntryPage
	if err := json.Unmarshal([]byte(cliSearch.stdout), &searchPage); err != nil {
		t.Fatalf("decode searchPage: %v\n%s", err, cliSearch.stdout)
	}
	// Expect 3 matches:
	// - Tier 1 (FullMatch AND): notes/runbook.md (Heading "Tag Release", w=1.8) and notes/atlas.md line 4 ("push release via git tag", FactText w=1.5)
	// - Tier 2 (OR >= 50%): notes/atlas.md line 5 ("only push release branch for staging")
	// Expired entry must not appear.
	if searchPage.Total != 3 || len(searchPage.Items) != 3 {
		t.Fatalf("unexpected searchPage total=%d items=%+v", searchPage.Total, searchPage.Items)
	}
	for _, item := range searchPage.Items {
		if strings.Contains(item.FactText, "expired") {
			t.Fatalf("expired entry leaked into BM25 search: %+v", item)
		}
	}
	// Heading match ("Tag Release" in notes/runbook.md, weight 1.8) ranks ahead
	// of FactText match ("push release via git tag" in notes/atlas.md, weight 1.5),
	// and both FullMatch (AND) items rank ahead of the partial OR item ("only push release branch").
	if searchPage.Items[0].FilePath != "notes/runbook.md" ||
		searchPage.Items[1].FactText != "push release via git tag after CI passes [[notes/runbook]]" ||
		searchPage.Items[2].FactText != "only push release branch for staging [[notes/ATLAS-db]]" {
		t.Fatalf("unexpected BM25 ranking order: %+v", searchPage.Items)
	}

	// Verify pagination stability with --limit 1 --offset 1.
	cliPage2 := runMemctl(t, bin, home, env, "entries", "--q", "tag release", "--limit", "1", "--offset", "1", "--json")
	if cliPage2.code != 0 {
		t.Fatalf("entries paged failed: %s", cliPage2.stderr)
	}
	var page2 app.EntryPage
	if err := json.Unmarshal([]byte(cliPage2.stdout), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if page2.Total != 3 || len(page2.Items) != 1 || page2.Items[0].ID != searchPage.Items[1].ID {
		t.Fatalf("pagination mismatch: page2=%+v want item %+v", page2, searchPage.Items[1])
	}
	// Page2's single hit is in notes/atlas.md, which has `both` with notes/ATLAS-db.md
	// and `outgoing` to notes/runbook.md.
	wantAtlasRelated := []vault.RelatedFile{
		{Path: "notes/ATLAS-db.md", Relation: vault.RelationBoth, Summary: "Atlas database layer"},
		{Path: "notes/runbook.md", Relation: vault.RelationOutgoing, Summary: "Deployment runbook"},
	}
	if !reflect.DeepEqual(page2.RelatedFiles, wantAtlasRelated) {
		t.Fatalf("page2.RelatedFiles = %+v, want %+v", page2.RelatedFiles, wantAtlasRelated)
	}

	// 2. Chinese unigram + bigram query ("灰度 发布") and `--project` filter.
	cjkRes := runMemctl(t, bin, home, env, "entries", "--q", "灰度 发布", "--project", "atlas", "--json")
	if cjkRes.code != 0 {
		t.Fatalf("entries CJK failed: %s", cjkRes.stderr)
	}
	var cjkPage app.EntryPage
	if err := json.Unmarshal([]byte(cjkRes.stdout), &cjkPage); err != nil {
		t.Fatalf("decode cjkPage: %v", err)
	}
	if cjkPage.Total != 1 || cjkPage.Items[0].FilePath != "notes/ATLAS-db.md" {
		t.Fatalf("unexpected CJK search page: %+v", cjkPage)
	}

	// `--project atlas` without `--q` returns only entries from atlas files
	// (`notes/atlas.md`, `notes/ATLAS-db.md`, and `[project: atlas]` `notes/runbook.md`),
	// excluding `notes/other.md` and `MEMORY.md`.
	projRes := runMemctl(t, bin, home, env, "entries", "--project", "atlas", "--json")
	if projRes.code != 0 {
		t.Fatalf("entries --project atlas failed: %s", projRes.stderr)
	}
	var projPage app.EntryPage
	if err := json.Unmarshal([]byte(projRes.stdout), &projPage); err != nil {
		t.Fatalf("decode projPage: %v", err)
	}
	if projPage.Total != 4 {
		t.Fatalf("expected 4 active entries in project atlas, got %d: %+v", projPage.Total, projPage.Items)
	}
	for _, item := range projPage.Items {
		if item.FilePath == "notes/other.md" || item.FilePath == "MEMORY.md" {
			t.Fatalf("project filter leaked non-atlas file %s: %+v", item.FilePath, item)
		}
	}

	// 3. `--include-transcripts` in CLI JSON and text modes, MCP, and HTTP.
	transJSON := runMemctl(t, bin, home, env, "entries", "--q", "pgbouncer timeout", "--project", "atlas", "--include-transcripts", "--json")
	if transJSON.code != 0 {
		t.Fatalf("entries --include-transcripts --json failed: %s", transJSON.stderr)
	}
	var transPage app.EntryPage
	if err := json.Unmarshal([]byte(transJSON.stdout), &transPage); err != nil {
		t.Fatalf("decode transPage: %v", err)
	}
	if len(transPage.TranscriptHits) != 1 {
		t.Fatalf("expected 1 recent transcript hit (stale 20d digest excluded), got %+v", transPage.TranscriptHits)
	}
	hit := transPage.TranscriptHits[0]
	if hit.SessionID != "sid-recent-1" || hit.Project != "atlas" || hit.CwdBase != "atlas" || len(hit.Snippet) > app.MaxTranscriptSnippetBytes {
		t.Fatalf("unexpected transcript hit metadata: %+v", hit)
	}
	if strings.Contains(hit.Snippet, "sk-ant-api03") {
		t.Fatalf("transcript snippet leaked unredacted secret: %q", hit.Snippet)
	}

	transText := runMemctl(t, bin, home, env, "entries", "--q", "pgbouncer timeout", "--project", "atlas", "--include-transcripts")
	if transText.code != 0 || !strings.Contains(transText.stdout, "transcript:claude/sid-recent-1#0") {
		t.Fatalf("expected CLI text output to print transcript hit, got code=%d stdout=%q", transText.code, transText.stdout)
	}

	// 4. `memctl read notes/runbook.md --json` returns 1-hop backlink from `notes/atlas.md`.
	readRunbook := runMemctl(t, bin, home, env, "read", "notes/runbook.md", "--json")
	if readRunbook.code != 0 {
		t.Fatalf("read notes/runbook.md --json failed: %s", readRunbook.stderr)
	}
	var readPayload struct {
		FilePath     string              `json:"filePath"`
		RelatedFiles []vault.RelatedFile `json:"relatedFiles"`
	}
	if err := json.Unmarshal([]byte(readRunbook.stdout), &readPayload); err != nil {
		t.Fatalf("decode readPayload: %v", err)
	}
	wantRunbookRelated := []vault.RelatedFile{
		{Path: "notes/atlas.md", Relation: vault.RelationBacklink, Summary: "Atlas core service"},
	}
	if !reflect.DeepEqual(readPayload.RelatedFiles, wantRunbookRelated) {
		t.Fatalf("read notes/runbook.md relatedFiles = %+v, want %+v", readPayload.RelatedFiles, wantRunbookRelated)
	}

	// 5. HTTP GET /api/v1/entries and GET /api/v1/files/{path...}.
	cfg := config.Config{RepoPath: config.Canonical(repo), StateDir: stateDir}
	srv, err := server.New(server.Options{
		App:   app.New(cfg),
		Addr:  "127.0.0.1:18420",
		Token: b1Token,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	reqEntries := httptest.NewRequest(http.MethodGet, "/api/v1/entries?q=pgbouncer+timeout&project=atlas&includeTranscripts=true", nil)
	reqEntries.Host = "127.0.0.1:18420"
	reqEntries.Header.Set("X-Memctl-Token", b1Token)
	recEntries := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recEntries, reqEntries)
	if recEntries.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/entries status=%d body=%s", recEntries.Code, recEntries.Body.String())
	}
	var httpPage app.EntryPage
	if err := json.Unmarshal(recEntries.Body.Bytes(), &httpPage); err != nil {
		t.Fatalf("decode HTTP entries: %v", err)
	}
	if len(httpPage.TranscriptHits) != 1 || httpPage.TranscriptHits[0].SessionID != "sid-recent-1" {
		t.Fatalf("unexpected HTTP transcriptHits: %+v", httpPage.TranscriptHits)
	}

	reqFile := httptest.NewRequest(http.MethodGet, "/api/v1/files/notes/atlas.md", nil)
	reqFile.Host = "127.0.0.1:18420"
	reqFile.Header.Set("X-Memctl-Token", b1Token)
	recFile := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recFile, reqFile)
	if recFile.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/files/notes/atlas.md status=%d body=%s", recFile.Code, recFile.Body.String())
	}
	var httpFile struct {
		RelatedFiles []vault.RelatedFile `json:"relatedFiles"`
	}
	if err := json.Unmarshal(recFile.Body.Bytes(), &httpFile); err != nil {
		t.Fatalf("decode HTTP file: %v", err)
	}
	if !reflect.DeepEqual(httpFile.RelatedFiles, wantAtlasRelated) {
		t.Fatalf("HTTP file relatedFiles = %+v, want %+v", httpFile.RelatedFiles, wantAtlasRelated)
	}

	// 6. Lock-busy snapshot mode: hold the repository lock and dirty worktree files,
	// then verify MCP `memory_entries` and `memory_read` still serve tokenized BM25
	// search and 1-hop `relatedFiles` from the committed branch snapshot.
	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes", "atlas.md"), []byte("# Corrupted dirty worktree\n"), 0o644); err != nil {
		t.Fatalf("dirty atlas.md: %v", err)
	}
	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer func() {
		_ = held.Release()
		gitOutput(t, env, repo, "checkout", "-q", "-f", "main")
	}()

	responses, mcpStderr := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_entries","arguments":{"q":"tag release","project":"atlas","includeTranscripts":false}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memory_read","arguments":{"path":"notes/atlas.md"}}}`,
	)
	if mcpStderr != "" {
		t.Fatalf("unexpected mcp stderr: %s", mcpStderr)
	}
	mcpEntriesRes := decodeCallResult(t, responses[0])
	if mcpEntriesRes.IsError {
		t.Fatalf("lock-busy MCP memory_entries failed: %+v", mcpEntriesRes)
	}
	var busyPage app.EntryPage
	if err := json.Unmarshal([]byte(mcpEntriesRes.Content[0].Text), &busyPage); err != nil {
		t.Fatalf("decode busyPage: %v", err)
	}
	if busyPage.Total != 3 || busyPage.Items[0].FilePath != "notes/runbook.md" || len(busyPage.RelatedFiles) == 0 {
		t.Fatalf("unexpected lock-busy MCP memory_entries page: %+v", busyPage)
	}

	mcpReadRes := decodeCallResult(t, responses[1])
	if mcpReadRes.IsError {
		t.Fatalf("lock-busy MCP memory_read failed: %+v", mcpReadRes)
	}
	var busyRead struct {
		RawContent   string              `json:"rawContent"`
		RelatedFiles []vault.RelatedFile `json:"relatedFiles"`
	}
	if err := json.Unmarshal([]byte(mcpReadRes.Content[0].Text), &busyRead); err != nil {
		t.Fatalf("decode busyRead: %v", err)
	}
	if strings.Contains(busyRead.RawContent, "Corrupted") || !reflect.DeepEqual(busyRead.RelatedFiles, wantAtlasRelated) {
		t.Fatalf("unexpected lock-busy MCP memory_read: %+v", busyRead)
	}
}
