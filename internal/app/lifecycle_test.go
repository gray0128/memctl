package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
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

// TestBusyInjectionHidesExpiredEntries covers the lock-busy fallback of the
// session body: while another process holds the repository lock, start and
// context still inject the last readable MEMORY.md, and that read goes
// through the same hide projection as the locked path.
func TestBusyInjectionHidesExpiredEntries(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n- keep me\n- drop me [until: 2026-10-18]\n")
	gitDir, err := git.GitDir(ctx, service.cfg.RepoPath)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer held.Release()

	outcome, err := service.PrepareSession(ctx, SessionOptions{SessionID: "s-busy"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(outcome.Body, "sync=busy") {
		t.Fatalf("banner does not report the busy lock:\n%s", outcome.Body)
	}
	if strings.Contains(outcome.Body, "drop me") {
		t.Fatalf("busy injection leaked the expired fact:\n%s", outcome.Body)
	}
	if !strings.Contains(outcome.Body, "keep me") || !strings.Contains(outcome.Body, vault.ExpiredFactPlaceholder) {
		t.Fatalf("busy injection lost the projected entry file:\n%s", outcome.Body)
	}
}

// TestWriteAfterHideKeepsExpiredEntry covers the baseline contract: a caller
// writes back the projected read it received, placeholders included, and the
// hidden facts stay on disk byte for byte, so hiding never deletes data. Two
// entries with the same projection are restored in their on-disk order.
func TestWriteAfterHideKeepsExpiredEntry(t *testing.T) {
	service, ctx := newLifecycleService(t)
	original := "# Memory\n\n" +
		"- expired [until: 2026-10-01]\n" +
		"- active [until: 2026-12-31]\n" +
		"- twin one [until: 2026-10-02]\n" +
		"- twin two [until: 2026-10-02]\n"
	writeFile(t, service, ctx, "MEMORY.md", original)
	file, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if file.HiddenExpired != 3 || strings.Contains(file.RawContent, "expired [") {
		t.Fatalf("projection did not hide the expired entries: %+v", file)
	}
	// The caller only adds a line to the projected content it was given.
	updated := strings.Replace(file.RawContent, "# Memory\n", "# Memory\n\n- new fact\n", 1)
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
	want := strings.Replace(original, "# Memory\n", "# Memory\n\n- new fact\n", 1)
	if onDisk.RawContent != want {
		t.Fatalf("write did not restore the hidden entries:\n%s\nwant:\n%s", onDisk.RawContent, want)
	}
	if strings.Contains(onDisk.RawContent, vault.ExpiredFactPlaceholder) {
		t.Fatalf("placeholder reached the file:\n%s", onDisk.RawContent)
	}
}

// TestWriteRefusesUnmatchedPlaceholder covers the forgery side of the restore:
// a placeholder that is not the projection of a hidden entry on disk, whether
// fabricated, duplicated or with edited metadata, is refused with
// INVALID_ARGUMENT and the file stays unchanged.
func TestWriteRefusesUnmatchedPlaceholder(t *testing.T) {
	service, ctx := newLifecycleService(t)
	original := "# Memory\n\n- expired [until: 2026-10-01]\n"
	writeFile(t, service, ctx, "MEMORY.md", original)
	file, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	placeholderLine := "- " + vault.ExpiredFactPlaceholder + " [until: 2026-10-01]\n"
	cases := map[string]string{
		"fabricated": file.RawContent + "- " + vault.ExpiredFactPlaceholder + " [until: 2026-09-01]\n",
		"duplicated": file.RawContent + placeholderLine,
		"edited":     strings.Replace(file.RawContent, "[until: 2026-10-01]", "[until: 2027-10-01]", 1),
	}
	for name, content := range cases {
		_, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
			Path: "MEMORY.md", ExpectedRevision: file.FileRevision, Action: ActionPut, RawContent: content,
		}}})
		requireContractCode(t, err, memerr.InvalidArgument)
		if onDisk, _ := service.readRaw(ctx, "MEMORY.md"); onDisk.RawContent != original {
			t.Fatalf("%s: the refused write changed the file:\n%s", name, onDisk.RawContent)
		}
	}
	// A new file has no hidden entry, so any placeholder in it is forged.
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "forged.md", ExpectedRevision: vault.AbsentRevision, Action: ActionPut, RawContent: "# Forged\n\n" + placeholderLine,
	}}})
	requireContractCode(t, err, memerr.InvalidArgument)
}

// TestWholeFileWriteRespectsHiddenPeriod covers the delete gate on the shared
// Apply kernel behind CLI write, MCP memory_write and PUT /api/v1/files: a
// whole-file write or file delete that drops an expired entry inside its
// hidden period is refused with DELETION_DEFERRED, and the same write passes
// once the period is over.
func TestWholeFileWriteRespectsHiddenPeriod(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "topic.md", "# Topic\n\n- expired [until: 2026-10-01]\n- plain\n")
	file, err := service.ReadFile(ctx, "topic.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The clock is 2026-10-19; until 2026-10-01 becomes deletable on 2026-11-01.
	dropped := "# Topic\n\n- plain\n"
	_, err = service.PutFile(ctx, "topic.md", file.FileRevision, dropped, "", "", "")
	requireContractCode(t, err, memerr.DeletionDeferred)
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "topic.md", ExpectedRevision: file.FileRevision, Action: ActionDelete,
	}}})
	requireContractCode(t, err, memerr.DeletionDeferred)
	if onDisk, _ := service.readRaw(ctx, "topic.md"); !strings.Contains(onDisk.RawContent, "- expired [until: 2026-10-01]") {
		t.Fatalf("a refused write changed the file:\n%s", onDisk.RawContent)
	}

	service.SetClock(fixedClock(t, time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)))
	if _, err := service.PutFile(ctx, "topic.md", file.FileRevision, dropped, "", "", ""); err != nil {
		t.Fatalf("write after the hidden period: %v", err)
	}
	if onDisk, _ := service.readRaw(ctx, "topic.md"); onDisk.RawContent != dropped {
		t.Fatalf("the expired entry was not removed:\n%s", onDisk.RawContent)
	}
}

// readRaw reads one file without the hide projection, so a test can inspect
// the bytes on disk.
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

// TestDeleteEntryRespectsHiddenPeriod covers the delete gate: an expired entry
// inside its 30-day hidden period is refused with DELETION_DEFERRED and the
// file is untouched; the same entry is deletable once the period ends, and an
// entry without `until` is always deletable.
func TestDeleteEntryRespectsHiddenPeriod(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n- expired [until: 2026-10-01; added: 2026-09-01]\n- plain\n")

	// The expired entry is hidden from ListEntries, so its ID comes from the
	// raw parse. That is the point: a hidden entry is still addressable by an
	// explicit delete request.
	file, err := service.readRaw(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
	expiredID := ""
	for _, entry := range parsed.Entries {
		if entry.FactText == "expired" {
			expiredID = entry.ID
		}
	}
	if expiredID == "" {
		t.Fatalf("could not find the expired entry in the raw parse: %+v", parsed.Entries)
	}

	// The clock is 2026-10-19; until 2026-10-01 becomes deletable on 2026-11-01.
	if _, err := service.DeleteEntry(ctx, DeleteEntryRequest{
		ID: expiredID, FilePath: "MEMORY.md", FileRevision: file.FileRevision,
	}); err == nil {
		t.Fatal("deleting an entry inside its hidden period should be refused")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.DeletionDeferred {
		t.Fatalf("delete error = %v, want DELETION_DEFERRED", err)
	}
	if onDisk, _ := service.readRaw(ctx, "MEMORY.md"); onDisk.RawContent != file.RawContent {
		t.Fatal("the refused delete changed the file")
	}

	// After the hidden period the same entry can be deleted deliberately.
	service.SetClock(fixedClock(t, time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)))
	file, err = service.readRaw(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	parsed = vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
	for _, entry := range parsed.Entries {
		if entry.FactText == "expired" {
			expiredID = entry.ID
		}
	}
	if _, err := service.DeleteEntry(ctx, DeleteEntryRequest{
		ID: expiredID, FilePath: "MEMORY.md", FileRevision: file.FileRevision,
	}); err != nil {
		t.Fatalf("delete after the hidden period: %v", err)
	}
	if onDisk, _ := service.readRaw(ctx, "MEMORY.md"); strings.Contains(onDisk.RawContent, "expired") {
		t.Fatalf("the entry was not deleted:\n%s", onDisk.RawContent)
	}
}

// TestDeleteEntryWithoutUntilIsAlwaysAllowed covers the default: an entry with
// no `until` keeps the documented "no expiry" semantics and its delete path is
// unchanged.
func TestDeleteEntryWithoutUntilIsAlwaysAllowed(t *testing.T) {
	service, ctx := newLifecycleService(t)
	writeFile(t, service, ctx, "MEMORY.md", "# Memory\n\n- plain [source: agent://dev/claude/s1]\n")
	page, err := service.ListEntries(ctx, EntryQuery{})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("entries = %+v", page.Items)
	}
	file, err := service.readRaw(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if _, err := service.DeleteEntry(ctx, DeleteEntryRequest{
		ID: page.Items[0].ID, FilePath: "MEMORY.md", FileRevision: file.FileRevision,
	}); err != nil {
		t.Fatalf("delete without until: %v", err)
	}
}

// TestApplyDreamRefusesDeleteInsideHiddenPeriod covers the bulk path: the
// Dreaming proposal is the documented place for cleanup, so applying one that
// removes an expired entry inside its hidden period is refused with
// DELETION_DEFERRED, the repository is untouched and the proposal stays ready.
func TestApplyDreamRefusesDeleteInsideHiddenPeriod(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	// A time-limited entry that expired two days ago: hidden, but still inside
	// the 30-day period. The date is relative so the test never ages.
	until := time.Now().AddDate(0, 0, -2).Format(vault.DateLayout)
	entryFile := "# Memory\n\n## Index\n- [[topic]]\n- recent trial [until: " + until + "; source: test; added: 2026-01-01]\n"
	writeFile(t, service, ctx, "MEMORY.md", entryFile)
	// The worker runs from a clone of origin, so the fixture must publish the
	// commit the proposal will pin as its baseline.
	runGit(t, repo, "push", "-q", "origin", "main")
	before := readTestFile(t, filepath.Join(repo, "MEMORY.md"))

	// The worker drops the expired entry and keeps the rest of the index.
	response := `{"operations":[{"action":"update","path":"MEMORY.md","content":"# Memory\n\n## Index\n- [[topic]]\n","rationale":"drop expired"}],"warnings":[]}`
	proposal := previewWithResponse(t, service, response)
	if proposal.Status != dream.StatusReady {
		t.Fatalf("proposal = %+v, want ready", proposal)
	}

	_, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	requireContractCode(t, err, memerr.DeletionDeferred)
	if after := readTestFile(t, filepath.Join(repo, "MEMORY.md")); after != before {
		t.Fatalf("the refused apply changed MEMORY.md:\n%s", after)
	}
	// The proposal is still ready, so the operator can regenerate it once the
	// hidden period ends.
	reloaded, loadErr := service.dreamService().Load(ctx, proposal.ProposalID)
	if loadErr != nil {
		t.Fatalf("reload proposal: %v", loadErr)
	}
	if reloaded.Status != dream.StatusReady {
		t.Fatalf("proposal status after refusal = %s, want ready", reloaded.Status)
	}
}

// TestApplyDreamAllowsDeleteAfterHiddenPeriod covers the allowed side: once the
// hidden period is over, a proposal may remove the expired entry, and the
// delete is a normal managed commit.
func TestApplyDreamAllowsDeleteAfterHiddenPeriod(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	until := time.Now().AddDate(0, 0, -40).Format(vault.DateLayout)
	entryFile := "# Memory\n\n## Index\n- [[topic]]\n- old trial [until: " + until + "; source: test; added: 2026-01-01]\n"
	writeFile(t, service, ctx, "MEMORY.md", entryFile)
	runGit(t, repo, "push", "-q", "origin", "main")

	response := `{"operations":[{"action":"update","path":"MEMORY.md","content":"# Memory\n\n## Index\n- [[topic]]\n","rationale":"drop expired"}],"warnings":[]}`
	proposal := previewWithResponse(t, service, response)

	result, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	if err != nil {
		t.Fatalf("apply after the hidden period: %v", err)
	}
	if result.Commit == "" {
		t.Fatal("apply returned no commit")
	}
	if after := readTestFile(t, filepath.Join(repo, "MEMORY.md")); strings.Contains(after, "old trial") {
		t.Fatalf("the expired entry was not removed:\n%s", after)
	}
}
