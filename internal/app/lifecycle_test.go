package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/vault"
)

// fixedClock returns a clock the expiry tests can control.
func fixedClock(t *testing.T, value time.Time) func() time.Time {
	t.Helper()
	return func() time.Time { return value }
}

// newLifecycleService builds an initialized repository whose clock is fixed at
// noon on 2026-10-19, so `until` boundaries are deterministic.
func newLifecycleService(t *testing.T) (*App, context.Context) {
	t.Helper()
	service, _ := newTestService(t)
	service.SetClock(fixedClock(t, time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)))
	return service, context.Background()
}

// TestReadFileHidesExpiredEntryCoversReadProjection covers the read-time hide:
// the visible fact disappears, the on-disk file and the baseline revision stay
// untouched, and HiddenExpired reports the count.
func TestReadFileHidesExpiredEntryCoversReadProjection(t *testing.T) {
	service, ctx := newLifecycleService(t)
	content := "# Memory\n\n" +
		"## Index\n\n" +
		"- active fact [until: 2026-12-31]\n" +
		"- expired fact [until: 2026-10-01; added: 2026-09-01]\n" +
		"- plain fact\n"
	writeFile(t, service, ctx, "MEMORY.md", content)

	file, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if file.HiddenExpired != 1 {
		t.Fatalf("HiddenExpired = %d, want 1", file.HiddenExpired)
	}
	if strings.Contains(file.RawContent, "expired fact") {
		t.Fatalf("expired fact is still visible:\n%s", file.RawContent)
	}
	if !strings.Contains(file.RawContent, vault.ExpiredFactPlaceholder) {
		t.Fatalf("hidden entry placeholder missing:\n%s", file.RawContent)
	}
	if !strings.Contains(file.RawContent, "active fact") || !strings.Contains(file.RawContent, "plain fact") {
		t.Fatalf("visible entries changed:\n%s", file.RawContent)
	}

	// The on-disk file keeps the expired entry, and the returned revision is
	// still the baseline of that unchanged file.
	onDisk, err := service.readRaw(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if onDisk.RawContent != content {
		t.Fatalf("read projection modified the file:\n%s", onDisk.RawContent)
	}
	if file.FileRevision != onDisk.FileRevision {
		t.Fatalf("projection changed the baseline: %q != %q", file.FileRevision, onDisk.FileRevision)
	}
}

// TestSnapshotHidesExpiredEntries covers the second read surface: the snapshot
// drops expired entries from every file's entry list and the flat entry list,
// and counts them.
func TestSnapshotHidesExpiredEntries(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n## Index\n\n- [[topic]]\n- top level [until: 2026-10-02]\n")
	writeFile(t, service, ctx, "topic.md", "# Topic\n\n- kept [until: 2026-11-01]\n- gone [until: 2026-01-01]\n")

	snapshot, err := service.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.HiddenExpired != 2 {
		t.Fatalf("HiddenExpired = %d, want 2", snapshot.HiddenExpired)
	}
	total := 0
	for _, file := range snapshot.Files {
		for _, entry := range file.Entries {
			total++
			if entry.FactText == "gone" || entry.FactText == "top level" {
				t.Fatalf("expired entry %q is in the snapshot", entry.FactText)
			}
		}
	}
	if total != 2 {
		t.Fatalf("snapshot has %d visible entries, want 2", total)
	}

	page, err := service.ListEntries(ctx, EntryQuery{})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("ListEntries total = %d, want 2", page.Total)
	}
	for _, entry := range page.Items {
		if entry.FactText == "gone" || entry.FactText == "top level" {
			t.Fatalf("expired entry %q is in the entry page", entry.FactText)
		}
	}
}

// TestInjectionHidesExpiredEntries covers the third read surface: the session
// body delivered to the host carries the placeholder instead of the expired
// fact.
func TestInjectionHidesExpiredEntries(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n- keep me\n- drop me [until: 2026-10-18]\n")

	outcome, err := service.PrepareSession(ctx, SessionOptions{SessionID: "s-expiry"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if strings.Contains(outcome.Body, "drop me") {
		t.Fatalf("injection still contains the expired fact:\n%s", outcome.Body)
	}
	if !strings.Contains(outcome.Body, "keep me") {
		t.Fatalf("injection lost the current fact:\n%s", outcome.Body)
	}
	if !strings.Contains(outcome.Body, vault.ExpiredFactPlaceholder) {
		t.Fatalf("injection does not mark the hidden entry:\n%s", outcome.Body)
	}
}

// TestWriteAfterHideKeepsExpiredEntry covers the baseline contract: a write
// based on the projected revision round-trips the hidden entry, so hiding never
// deletes data.
func TestWriteAfterHideKeepsExpiredEntry(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n- expired [until: 2026-10-01]\n")
	file, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The caller edits another line only. The placeholder is not part of the
	// file, so the write content has to come from the raw read.
	raw, err := service.readRaw(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	updated := strings.Replace(raw.RawContent, "# Memory\n", "# Memory\n\n- new fact\n", 1)
	if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "MEMORY.md",
		ExpectedRevision: file.FileRevision,
		Action:           ActionPut,
		RawContent:       updated,
	}}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	onDisk, err := service.readRaw(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("raw read after write: %v", err)
	}
	if !strings.Contains(onDisk.RawContent, "expired [until: 2026-10-01]") {
		t.Fatalf("write dropped the hidden entry:\n%s", onDisk.RawContent)
	}
}

// readRaw reads one file without the hide projection, which is what a caller
// that intends to rewrite the file must use.
func (a *App) readRaw(ctx context.Context, relPath string) (vault.FileRead, error) {
	session, err := a.beginRead(ctx)
	if err != nil {
		return vault.FileRead{}, err
	}
	defer session.Close()
	return session.root.ReadFile(relPath)
}

// writeFile writes one file through the managed transaction, creating it when
// it does not exist yet.
func writeFile(t *testing.T, service *App, ctx context.Context, path, content string) {
	t.Helper()
	base := vault.AbsentRevision
	if file, err := service.readRaw(ctx, path); err == nil {
		base = file.FileRevision
	}
	if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             path,
		ExpectedRevision: base,
		Action:           ActionPut,
		RawContent:       content,
	}}}); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
