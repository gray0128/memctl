package app

import (
	"context"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// T144 (docs/18 section 4.4.1 and 4.4.2) on the application write paths:
// block-level delete, move and patch, the expired-detail projection and its
// write-back, the `kind: event` guard and append with kind/status.

const t144Topic = "# Topic\n\n" +
	"- Fix the pool timeout [source: agent://d/a/s; added: 2026-10-01]\n" +
	"  1. Stop the pgbouncer worker\n" +
	"  2. Raise pool_timeout to 30\n" +
	"- Unrelated fact [added: 2026-10-02]\n"

func t144Service(t *testing.T) (*App, context.Context) {
	t.Helper()
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n## Index\n\n- [[topic]]\n- [[archive]]\n")
	writeFile(t, service, ctx, "topic.md", t144Topic)
	writeFile(t, service, ctx, "archive.md", "# Archive\n")
	return service, ctx
}

func TestT144EntriesSearchHitsDetailLines(t *testing.T) {
	service, ctx := t144Service(t)
	page, err := service.ListEntries(ctx, EntryQuery{Q: "pgbouncer"})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(page.Items) == 0 || page.Items[0].FactText != "Fix the pool timeout" {
		t.Fatalf("detail keyword not found: %+v", page.Items)
	}
	if len(page.Items[0].DetailLines) != 2 {
		t.Fatalf("DetailLines = %q", page.Items[0].DetailLines)
	}
}

func TestT144DeleteEntryRemovesBlockAndKeepsOtherIDs(t *testing.T) {
	service, ctx := t144Service(t)
	target := entryByFact(t, service, "topic.md", "Fix the pool timeout")
	other := entryByFact(t, service, "topic.md", "Unrelated fact")
	if _, err := service.DeleteEntry(ctx, DeleteEntryRequest{ID: target.ID, FilePath: "topic.md", FileRevision: target.FileRevision}); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	onDisk, _ := service.readRaw(ctx, "topic.md")
	if onDisk.RawContent != "# Topic\n\n- Unrelated fact [added: 2026-10-02]\n" {
		t.Fatalf("block delete left lines behind:\n%s", onDisk.RawContent)
	}
	// The other entry moved up two lines and the file revision changed, but
	// its content-stable id did not.
	after := entryByFact(t, service, "topic.md", "Unrelated fact")
	if after.ID != other.ID || after.LineNumber == other.LineNumber || after.FileRevision == other.FileRevision {
		t.Fatalf("id not stable: before %+v after %+v", other, after)
	}
}

func TestT144PatchKeepsDetailsAndMoveCarriesThem(t *testing.T) {
	service, ctx := t144Service(t)
	target := entryByFact(t, service, "topic.md", "Fix the pool timeout")
	patched, err := service.PatchEntry(ctx, PatchEntryRequest{ID: target.ID, FilePath: "topic.md", FileRevision: target.FileRevision, FactText: "Fix the pool timeout fast"})
	if err != nil {
		t.Fatalf("PatchEntry: %v", err)
	}
	if len(patched.Entry.DetailLines) != 2 {
		t.Fatalf("patch dropped details: %+v", patched.Entry)
	}
	base := snapshotRevision(t, service)
	moved, err := service.PatchEntry(ctx, PatchEntryRequest{
		ID: patched.Entry.ID, FilePath: "topic.md", FileRevision: patched.Entry.FileRevision,
		FactText: "Fix the pool timeout fast", MoveToFilePath: "archive.md", BaseRevision: base,
	})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.Entry.FilePath != "archive.md" || moved.Entry.FactText != "Fix the pool timeout fast" || len(moved.Entry.DetailLines) != 2 {
		t.Fatalf("moved entry = %+v", moved.Entry)
	}
	source, _ := service.readRaw(ctx, "topic.md")
	if strings.Contains(source.RawContent, "pgbouncer") || strings.Contains(source.RawContent, "pool_timeout") {
		t.Fatalf("move left detail lines behind:\n%s", source.RawContent)
	}
	archive, _ := service.readRaw(ctx, "archive.md")
	if !strings.HasSuffix(archive.RawContent, "  1. Stop the pgbouncer worker\n  2. Raise pool_timeout to 30\n") {
		t.Fatalf("archive = %q", archive.RawContent)
	}
}

func TestT144ExpiredDetailsHiddenAndRestoredOnWrite(t *testing.T) {
	service, ctx := newLifecycleService(t)
	content := "# Memory\n\n## Index\n\n" +
		"- Old recipe [added: 2026-01-01; until: 2026-10-01]\n" +
		"  hidden step one\n" +
		"  hidden step two\n" +
		"- Live fact\n"
	writeFile(t, service, ctx, "MEMORY.md", content)

	file, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(file.RawContent, "hidden step") {
		t.Fatalf("expired details leaked:\n%s", file.RawContent)
	}
	// Writing the projection back with one unrelated change restores the
	// expired bullet and its details byte for byte.
	edited := file.RawContent + "- New fact\n"
	if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: file.FileRevision, Action: ActionPut, RawContent: edited,
	}}}); err != nil {
		t.Fatalf("write back: %v", err)
	}
	onDisk, _ := service.readRaw(ctx, "MEMORY.md")
	if onDisk.RawContent != content+"- New fact\n" {
		t.Fatalf("restore lost bytes:\n%q\nwant\n%q", onDisk.RawContent, content+"- New fact\n")
	}
}

func TestT144EventGuardOnWriteDeleteAndPatch(t *testing.T) {
	service, ctx := newLifecycleService(t)
	event := "- Released v1.0 [added: 2026-10-01; kind: event]"
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n## Index\n\n- [[topic]]\n- [[archive]]\n")
	writeFile(t, service, ctx, "topic.md", "# Topic\n\n"+event+"\n- Other [added: 2026-10-02]\n")
	writeFile(t, service, ctx, "archive.md", "# Archive\n")

	put := func(path, content string, force bool) error {
		file, err := service.readRaw(ctx, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		_, err = service.Apply(ctx, MutationRequest{ForceDeleteEvent: force, Changes: []FileChange{{
			Path: path, ExpectedRevision: file.FileRevision, Action: ActionPut, RawContent: content,
		}}})
		return err
	}

	// A whole-file write may not drop or rewrite the event.
	requireContractCode(t, put("topic.md", "# Topic\n\n- Other [added: 2026-10-02]\n", false), memerr.EventProtected)
	requireContractCode(t, put("topic.md", "# Topic\n\n- Released v1.1 [added: 2026-10-01; kind: event]\n- Other [added: 2026-10-02]\n", false), memerr.EventProtected)
	// Unrelated edits and metadata additions that keep the event are fine.
	if err := put("topic.md", "# Topic\n\nintro\n\n"+event+"\n- Other changed [added: 2026-10-02]\n", false); err != nil {
		t.Fatalf("unrelated edit refused: %v", err)
	}

	// The entry routes refuse too.
	entry := entryByFact(t, service, "topic.md", "Released v1.0")
	if entry.Kind != vault.KindEvent {
		t.Fatalf("DTO kind = %q", entry.Kind)
	}
	_, err := service.DeleteEntry(ctx, DeleteEntryRequest{ID: entry.ID, FilePath: "topic.md", FileRevision: entry.FileRevision})
	requireContractCode(t, err, memerr.EventProtected)
	_, err = service.PatchEntry(ctx, PatchEntryRequest{ID: entry.ID, FilePath: "topic.md", FileRevision: entry.FileRevision, FactText: "Released v2"})
	requireContractCode(t, err, memerr.EventProtected)

	// A multi-file write that moves the event unchanged is an archive.
	topic, _ := service.readRaw(ctx, "topic.md")
	archive, _ := service.readRaw(ctx, "archive.md")
	if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{
		{Path: "topic.md", ExpectedRevision: topic.FileRevision, Action: ActionPut, RawContent: "# Topic\n\n- Other changed [added: 2026-10-02]\n"},
		{Path: "archive.md", ExpectedRevision: archive.FileRevision, Action: ActionPut, RawContent: "# Archive\n\n" + event + "\n"},
	}}); err != nil {
		t.Fatalf("cross-file archive refused: %v", err)
	}

	// The explicit force flag deletes it.
	requireContractCode(t, put("archive.md", "# Archive\n", false), memerr.EventProtected)
	if err := put("archive.md", "# Archive\n", true); err != nil {
		t.Fatalf("forced delete refused: %v", err)
	}

	// DeleteEntry honours its own force flag.
	writeFile(t, service, ctx, "archive.md", "# Archive\n\n"+event+"\n")
	entry = entryByFact(t, service, "archive.md", "Released v1.0")
	if _, err := service.DeleteEntry(ctx, DeleteEntryRequest{ID: entry.ID, FilePath: "archive.md", FileRevision: entry.FileRevision, ForceDeleteEvent: true}); err != nil {
		t.Fatalf("forced DeleteEntry: %v", err)
	}
}

func TestT144AppendEntryKindStatus(t *testing.T) {
	service, ctx := t144Service(t)
	result, err := service.AppendEntry(ctx, AppendEntryRequest{FilePath: "topic.md", FactText: "Maybe the cache is stale", Kind: "state", Status: "hypothesis", Agent: "a", SessionID: "s"})
	if err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	if result.Entry.Kind != vault.KindState || result.Entry.Status != vault.StatusHypothesis ||
		!strings.HasSuffix(result.Entry.RawLine, "; kind: state; status: hypothesis]") {
		t.Fatalf("entry = %+v", result.Entry)
	}
	_, err = service.AppendEntry(ctx, AppendEntryRequest{FilePath: "topic.md", FactText: "x", Kind: "events"})
	requireContractCode(t, err, memerr.InvalidArgument)
	_, err = service.AppendEntry(ctx, AppendEntryRequest{FilePath: "topic.md", FactText: "x", Status: "probably"})
	requireContractCode(t, err, memerr.InvalidArgument)
}

// TestT144DreamApplyRefusesEventRemoval covers the apply-time guard that also
// protects proposals built by the remote worker, which never pass through
// ProposeLocal.
func TestT144DreamApplyRefusesEventRemoval(t *testing.T) {
	service, ctx := newLifecycleService(t)
	event := "- Released v1.0 [added: 2026-10-01; kind: event]\n"
	writeFile(t, service, ctx, "topic.md", "# Topic\n"+event)
	session, err := service.beginRead(ctx)
	if err != nil {
		t.Fatalf("beginRead: %v", err)
	}
	defer session.Close()
	drop := dream.ApplyPlan{Changes: []dream.ApplyChange{{Path: "topic.md", Action: dream.ChangeUpdate, Content: "# Topic\n"}}}
	requireContractCode(t, requireDreamEventsKept(session.root, drop), memerr.EventProtected)
	remove := dream.ApplyPlan{Changes: []dream.ApplyChange{{Path: "topic.md", Action: dream.ChangeDelete}}}
	requireContractCode(t, requireDreamEventsKept(session.root, remove), memerr.EventProtected)
	move := dream.ApplyPlan{Changes: []dream.ApplyChange{
		{Path: "topic.md", Action: dream.ChangeUpdate, Content: "# Topic\n"},
		{Path: "archive.md", Action: dream.ChangeCreate, Content: "# Archive\n" + event},
	}}
	if err := requireDreamEventsKept(session.root, move); err != nil {
		t.Fatalf("move refused: %v", err)
	}
}
