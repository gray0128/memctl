package dream

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/corpus"
)

// T144 (docs/18 section 4.4.1 and 4.4.2) on the proposal channels: structured
// operations keep entry blocks whole and carry kind/status, and neither
// channel deletes or rewrites a `kind: event` entry.

const t144OpsNotes = "# DB\n\n" +
	"- 连接池排障配方 [source: test; added: 2026-10-01]\n" +
	"  1. 查看 pgbouncer 统计\n" +
	"  2. 调整 statement_timeout\n" +
	"- 缓存可能过期 [source: test; added: 2026-10-01; status: hypothesis]\n" +
	"- 发布 v1.0 [source: test; added: 2026-10-01; kind: event]\n" +
	"- 尾部条目 [source: test; added: 2026-10-01]\n"

func t144OpsFixture(t *testing.T) (*Service, DreamPrepareBundle) {
	t.Helper()
	hermeticHome(t)
	repo := initLocalRepo(t)
	writeFile(t, filepath.Join(repo, "MEMORY.md"), "# Memory\n\n## Index\n- [[topic]]\n- [[notes/db-service]] — 数据库服务\n- [[notes/archive]] — 归档\n")
	writeFile(t, filepath.Join(repo, "notes", "db-service.md"), t144OpsNotes)
	writeFile(t, filepath.Join(repo, "notes", "archive.md"), "# Archive\n")
	env := gitEnv(t)
	gitRun(t, env, repo, "add", "--", "MEMORY.md", "notes/db-service.md", "notes/archive.md")
	gitRun(t, env, repo, "commit", "-q", "-m", "add db notes")
	service := newTestService(t, repo, t.TempDir())
	service.Now = func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }
	project := filepath.Join(t.TempDir(), "db-service")
	digest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "claude-code",
		SessionID:     "sess-db",
		SourceTag:     "agent://device-1/claude-code/sess-db",
		Date:          "2026-10-20",
		UpdatedAt:     "2026-10-20T10:00:00Z",
		CwdBase:       corpus.CwdBase(project),
		CwdKey:        corpus.CwdKey(project),
		Turns: []corpus.Turn{{
			User:      "排查连接池与缓存",
			Assistant: "确认缓存确实过期，pgbouncer 统计正常。",
		}},
	}
	if err := service.corpusCache().Save(digest); err != nil {
		t.Fatalf("save digest: %v", err)
	}
	bundle, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: project})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	return service, bundle
}

func t144Planned(t *testing.T, service *Service, input DreamProposeInput) map[string]string {
	t.Helper()
	result, err := service.ProposeLocal(context.Background(), input)
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}
	stored, err := service.store.Load(result.ProposalID)
	if err != nil {
		t.Fatalf("load proposal: %v", err)
	}
	contents := map[string]string{}
	for path, file := range stored.Planned {
		contents[path] = file.Content
	}
	return contents
}

func TestT144OperationsKeepBlocksAndKindStatus(t *testing.T) {
	service, bundle := t144OpsFixture(t)
	base := func(ops ...DreamOperation) DreamProposeInput {
		return DreamProposeInput{BundleID: bundle.BundleID, BaseHead: bundle.BaseHead, BaseRevision: bundle.BaseRevision, Operations: ops}
	}
	tag := "agent://device-1/claude-code/sess-db"

	// delete_entry removes the bullet and its detail lines.
	contents := t144Planned(t, service, base(DreamOperation{Op: OpDeleteEntry, Path: "notes/db-service.md", TargetRawLine: "- 连接池排障配方 [source: test; added: 2026-10-01]"}))
	if got := contents["notes/db-service.md"]; strings.Contains(got, "pgbouncer") || strings.Contains(got, "statement_timeout") || !strings.HasPrefix(got, "# DB\n\n- 缓存可能过期") {
		t.Fatalf("block delete = %q", got)
	}

	// replace_entry keeps the details; status: verified upgrades a hypothesis,
	// and an omitted status keeps the target's.
	contents = t144Planned(t, service, base(
		DreamOperation{Op: OpReplaceEntry, Path: "notes/db-service.md", TargetRawLine: "- 连接池排障配方 [source: test; added: 2026-10-01]", Fact: "连接池排障配方（已复核）", Kind: "procedural", SessionIndices: []int{0}},
		DreamOperation{Op: OpReplaceEntry, Path: "notes/db-service.md", TargetRawLine: "- 缓存可能过期 [source: test; added: 2026-10-01; status: hypothesis]", Fact: "缓存确实过期", Status: "verified", SessionIndices: []int{0}},
		DreamOperation{Op: OpAppendEntry, Path: "notes/db-service.md", Fact: "pgbouncer 统计正常", Status: "hypothesis", SessionIndices: []int{0}},
	))
	want := "# DB\n\n" +
		"- 连接池排障配方（已复核） [source: " + tag + "; added: 2026-10-20; kind: procedural]\n" +
		"  1. 查看 pgbouncer 统计\n" +
		"  2. 调整 statement_timeout\n" +
		"- 缓存确实过期 [source: " + tag + "; added: 2026-10-20; status: verified]\n" +
		"- 发布 v1.0 [source: test; added: 2026-10-01; kind: event]\n" +
		"- 尾部条目 [source: test; added: 2026-10-01]\n" +
		"- pgbouncer 统计正常 [source: " + tag + "; added: 2026-10-20; status: hypothesis]\n"
	if got := contents["notes/db-service.md"]; got != want {
		t.Fatalf("notes/db-service.md = %q, want %q", got, want)
	}
	keep := t144Planned(t, service, base(DreamOperation{Op: OpReplaceEntry, Path: "notes/db-service.md", TargetRawLine: "- 缓存可能过期 [source: test; added: 2026-10-01; status: hypothesis]", Fact: "缓存仍可能过期", SessionIndices: []int{0}}))
	if !strings.Contains(keep["notes/db-service.md"], "- 缓存仍可能过期 [source: "+tag+"; added: 2026-10-20; status: hypothesis]\n") {
		t.Fatalf("omitted status was not kept: %q", keep["notes/db-service.md"])
	}

	if _, err := service.ProposeLocal(context.Background(), base(DreamOperation{Op: OpAppendEntry, Path: "notes/db-service.md", Fact: "x", Kind: "milestone", SessionIndices: []int{0}})); !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("unknown kind: err = %v", err)
	}
}

func TestT144ProposalsNeverDropEvents(t *testing.T) {
	service, bundle := t144OpsFixture(t)
	ctx := context.Background()
	event := "- 发布 v1.0 [source: test; added: 2026-10-01; kind: event]"
	ops := func(op DreamOperation) DreamProposeInput {
		return DreamProposeInput{BundleID: bundle.BundleID, BaseHead: bundle.BaseHead, BaseRevision: bundle.BaseRevision, Operations: []DreamOperation{op}}
	}
	if _, err := service.ProposeLocal(ctx, ops(DreamOperation{Op: OpDeleteEntry, Path: "notes/db-service.md", TargetRawLine: event})); !contractCode(err, "EVENT_PROTECTED") {
		t.Fatalf("delete_entry on an event: err = %v", err)
	}
	if _, err := service.ProposeLocal(ctx, ops(DreamOperation{Op: OpReplaceEntry, Path: "notes/db-service.md", TargetRawLine: event, Fact: "发布 v1.1", SessionIndices: []int{0}})); !contractCode(err, "EVENT_PROTECTED") {
		t.Fatalf("replace_entry on an event: err = %v", err)
	}

	files := func(entries ...ProposeFile) DreamProposeInput {
		return DreamProposeInput{BundleID: bundle.BundleID, BaseHead: bundle.BaseHead, BaseRevision: bundle.BaseRevision, Files: entries}
	}
	withoutEvent := strings.Replace(t144OpsNotes, event+"\n", "", 1)
	if _, err := service.ProposeLocal(ctx, files(ProposeFile{Path: "notes/db-service.md", Action: ChangeUpdate, Content: withoutEvent})); !contractCode(err, "EVENT_PROTECTED") {
		t.Fatalf("files[] dropping an event: err = %v", err)
	}
	// Moving the event unchanged to the linked archive is allowed.
	contents := t144Planned(t, service, files(
		ProposeFile{Path: "notes/db-service.md", Action: ChangeUpdate, Content: withoutEvent},
		ProposeFile{Path: "notes/archive.md", Action: ChangeUpdate, Content: "# Archive\n" + event + "\n"},
	))
	if !strings.Contains(contents["notes/archive.md"], event) || strings.Contains(contents["notes/db-service.md"], "发布 v1.0") {
		t.Fatalf("archive move = %+v", contents)
	}
}
