package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// TestCreateEntryAppendsAndCreatesFile covers POST /api/v1/entries through the
// application service: appending to an existing file and creating a new file.
func TestCreateEntryAppendsAndCreatesFile(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- first fact\n", vault.AbsentRevision)

	file, err := service.ReadFile(ctx, "alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "alpha.md",
		FactText:         "second fact",
		Sources:          []string{"agent://dev/claude/s1"},
		ExpectedRevision: file.FileRevision,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if created.Entry.FactText != "second fact" || created.Entry.FilePath != "alpha.md" {
		t.Fatalf("unexpected entry: %+v", created.Entry)
	}
	if len(created.Entry.Sources) != 1 || created.Entry.Sources[0].Agent != "claude" {
		t.Fatalf("sources not parsed: %+v", created.Entry.Sources)
	}
	if created.Entry.AddedDate == "" {
		t.Fatal("created entry should carry the recommended added date")
	}
	content := readTestFile(t, filepath.Join(repo, "alpha.md"))
	if !strings.Contains(content, "- second fact [source: agent://dev/claude/s1; added: ") {
		t.Fatalf("appended content = %q", content)
	}

	// A new file under a missing directory is refused; memctl does not invent
	// directory structure while holding the repository lock.
	if _, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "topics/beta.md",
		Heading:          "Beta",
		FactText:         "beta fact",
		ExpectedRevision: vault.AbsentRevision,
	}); err == nil {
		t.Fatal("creating a file under a missing directory should fail")
	}
	if err := os.Mkdir(filepath.Join(repo, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "topics/beta.md",
		Heading:          "Beta",
		FactText:         "beta fact",
		ExpectedRevision: vault.AbsentRevision,
	})
	if err != nil {
		t.Fatalf("create file entry: %v", err)
	}
	if got := readTestFile(t, filepath.Join(repo, "topics", "beta.md")); got != "# Beta\n\n- beta fact [added: "+fresh.Entry.AddedDate+"]\n" {
		t.Fatalf("new file content = %q", got)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean: %q", status)
	}
}

// TestPatchEntryReplacesFactKeepsMetadata covers an in-place patch: only the
// fact text changes and the trailing metadata bytes survive.
func TestPatchEntryReplacesFactKeepsMetadata(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- old fact [source: agent://dev/claude/s1; added: 2026-01-02]\n", vault.AbsentRevision)

	entry := firstEntry(t, service, "alpha.md")
	file, err := service.ReadFile(ctx, "alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	patched, err := service.PatchEntry(ctx, PatchEntryRequest{
		ID:           entry.ID,
		FilePath:     "alpha.md",
		FactText:     "new fact",
		FileRevision: file.FileRevision,
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if patched.Entry.FactText != "new fact" {
		t.Fatalf("patched fact = %q", patched.Entry.FactText)
	}
	if len(patched.Entry.Metadata) != 2 {
		t.Fatalf("metadata lost: %+v", patched.Entry.Metadata)
	}
	if got := readTestFile(t, filepath.Join(repo, "alpha.md")); got != "# Alpha\n\n- new fact [source: agent://dev/claude/s1; added: 2026-01-02]\n" {
		t.Fatalf("patched content = %q", got)
	}
}

// TestPatchEntryMovesAcrossFiles covers the graph split: the entry leaves its
// source and the destination file is created in one transaction.
func TestPatchEntryMovesAcrossFiles(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- keep\n- split me [source: agent://dev/claude/s1]\n- tail\n", vault.AbsentRevision)
	entry := entryByFact(t, service, "alpha.md", "split me")
	file, err := service.ReadFile(ctx, "alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	revision := snapshotRevision(t, service)

	moved, err := service.PatchEntry(ctx, PatchEntryRequest{
		ID:             entry.ID,
		FilePath:       "alpha.md",
		FactText:       "split me",
		MoveToFilePath: "topic.md",
		BaseRevision:   revision,
		FileRevision:   file.FileRevision,
	})
	if err != nil {
		t.Fatalf("move entry: %v", err)
	}
	if moved.Entry.FilePath != "topic.md" {
		t.Fatalf("entry moved to %q", moved.Entry.FilePath)
	}
	source := readTestFile(t, filepath.Join(repo, "alpha.md"))
	if strings.Contains(source, "split me") {
		t.Fatalf("source still contains the moved entry: %q", source)
	}
	if !strings.Contains(source, "- keep") || !strings.Contains(source, "- tail") {
		t.Fatalf("source lost unrelated entries: %q", source)
	}
	target := readTestFile(t, filepath.Join(repo, "topic.md"))
	if !strings.Contains(target, "- split me [source: agent://dev/claude/s1]") {
		t.Fatalf("target = %q", target)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean: %q", status)
	}
}

// TestEntryOperationsRejectStaleBaselines covers the A04 preconditions: a stale
// file revision and a stale repository baseline change nothing.
func TestEntryOperationsRejectStaleBaselines(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- fact\n", vault.AbsentRevision)
	entry := firstEntry(t, service, "alpha.md")
	file, err := service.ReadFile(ctx, "alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	revision := snapshotRevision(t, service)

	// Another writer commits a new revision of the file.
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- fact\n- other\n", file.FileRevision)

	_, err = service.PatchEntry(ctx, PatchEntryRequest{
		ID:           entry.ID,
		FilePath:     "alpha.md",
		FactText:     "patched",
		FileRevision: file.FileRevision,
	})
	requireContractCode(t, err, memerr.RevisionMismatch)

	_, err = service.PatchEntry(ctx, PatchEntryRequest{
		ID:             entry.ID,
		FilePath:       "alpha.md",
		FactText:       "patched",
		MoveToFilePath: "beta.md",
		BaseRevision:   revision,
		FileRevision:   file.FileRevision,
	})
	requireContractCode(t, err, memerr.RevisionMismatch)
	if _, statErr := os.Lstat(filepath.Join(repo, "beta.md")); !os.IsNotExist(statErr) {
		t.Fatalf("stale move created the destination: %v", statErr)
	}
	if got := readTestFile(t, filepath.Join(repo, "alpha.md")); !strings.Contains(got, "- other") {
		t.Fatalf("stale patch changed the file: %q", got)
	}
}

// TestDeleteEntryRemovesLine covers DELETE /api/v1/entries/{id}.
func TestDeleteEntryRemovesLine(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- keep\n- delete me\n", vault.AbsentRevision)
	entry := entryByFact(t, service, "alpha.md", "delete me")
	file, err := service.ReadFile(ctx, "alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.DeleteEntry(ctx, DeleteEntryRequest{
		ID:           entry.ID,
		FilePath:     "alpha.md",
		FileRevision: file.FileRevision,
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if result.Commit == "" {
		t.Fatal("delete did not create a commit")
	}
	if got := readTestFile(t, filepath.Join(repo, "alpha.md")); got != "# Alpha\n\n- keep\n" {
		t.Fatalf("deleted content = %q", got)
	}
}

// TestDoctorFixRecreatesEntryPoint covers the only deterministic repair:
// recreating a missing entry file in one managed transaction.
func TestDoctorFixRecreatesEntryPoint(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	if err := os.Remove(filepath.Join(repo, "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "--", "MEMORY.md")
	runGit(t, repo, "commit", "-q", "-m", "remove entry")

	report, err := service.Doctor(ctx)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !hasIssue(report.Issues, "ENTRY_MISSING", true) {
		t.Fatalf("expected an auto-fixable ENTRY_MISSING issue: %+v", report.Issues)
	}

	fixed, err := service.DoctorFix(ctx, DoctorFixRequest{BaseRevision: report.Revision})
	if err != nil {
		t.Fatalf("doctor fix: %v", err)
	}
	if fixed.Result == nil || fixed.Result.Commit == "" {
		t.Fatalf("no transaction was created: %+v", fixed)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != "# Memory\n\n## Index\n" {
		t.Fatalf("recreated entry = %q", got)
	}
	if hasIssue(fixed.Report.Issues, "ENTRY_MISSING", true) {
		t.Fatalf("ENTRY_MISSING still reported after the fix: %+v", fixed.Report.Issues)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean: %q", status)
	}

	// A code without a deterministic repair is refused, not guessed.
	_, err = service.DoctorFix(ctx, DoctorFixRequest{IssueCodes: []string{"ORPHAN_FILE"}, BaseRevision: fixed.Report.Revision})
	requireContractCode(t, err, memerr.Unsupported)
}

func firstEntry(t *testing.T, service *App, path string) MemoryEntryDTO {
	t.Helper()
	page, err := service.ListEntries(context.Background(), EntryQuery{File: path})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("no entries in %s", path)
	}
	return page.Items[0]
}

func entryByFact(t *testing.T, service *App, path, fact string) MemoryEntryDTO {
	t.Helper()
	page, err := service.ListEntries(context.Background(), EntryQuery{File: path})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range page.Items {
		if entry.FactText == fact {
			return entry
		}
	}
	t.Fatalf("entry %q not found in %s", fact, path)
	return MemoryEntryDTO{}
}

func hasIssue(issues []doctor.Issue, code string, autoFixable bool) bool {
	for _, issue := range issues {
		if issue.Code == code && issue.AutoFixable == autoFixable {
			return true
		}
	}
	return false
}

// TestEntryTextRejectsNewlines covers the single-line bullet format: a newline
// in a fact, heading or source would shift line numbers and corrupt the trailing
// metadata of every following line, so it is refused before any write.
func TestEntryTextRejectsNewlines(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "topic.md", "# Topic\n\n", vault.AbsentRevision)
	file, err := service.ReadFile(ctx, "topic.md")
	if err != nil {
		t.Fatalf("read topic.md: %v", err)
	}

	if _, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "topic.md",
		FactText:         "a\nb",
		ExpectedRevision: file.FileRevision,
	}); err == nil {
		t.Fatal("CreateEntry accepted a multi-line factText")
	} else {
		requireContractCode(t, err, memerr.InvalidArgument)
	}
	if _, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "topic.md",
		Heading:          "a\nb",
		FactText:         "fact",
		ExpectedRevision: file.FileRevision,
	}); err == nil {
		t.Fatal("CreateEntry accepted a multi-line heading")
	} else {
		requireContractCode(t, err, memerr.InvalidArgument)
	}

	if _, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "topic.md",
		FactText:         "kept",
		ExpectedRevision: file.FileRevision,
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}
	entry := firstEntry(t, service, "topic.md")
	if _, err := service.PatchEntry(ctx, PatchEntryRequest{
		ID:           entry.ID,
		FilePath:     "topic.md",
		FactText:     "a\nb",
		FileRevision: entry.FileRevision,
	}); err == nil {
		t.Fatal("PatchEntry accepted a multi-line factText")
	} else {
		requireContractCode(t, err, memerr.InvalidArgument)
	}

	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("a rejected entry left the repository dirty: %q", status)
	}
}

// TestCreateEntryExistingFileHeadingSections covers inserting into a middle
// section, an end-of-file section, and a newly created section when a non-empty
// heading is supplied for an existing file.
func TestCreateEntryExistingFileHeadingSections(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	initial := "# Architecture\n\n## Decisions\n\n- use SQLite\n\n## Open Questions\n\n- shard key\n"
	applyPut(t, service, repo, "arch.md", initial, vault.AbsentRevision)

	readRev := func() string {
		t.Helper()
		file, err := service.ReadFile(ctx, "arch.md")
		if err != nil {
			t.Fatalf("ReadFile(arch.md): %v", err)
		}
		return file.FileRevision
	}

	// 1. Middle section via plain heading title.
	mid1, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "arch.md",
		Heading:          "Decisions",
		FactText:         "keep WAL",
		ExpectedRevision: readRev(),
	})
	if err != nil {
		t.Fatalf("CreateEntry middle section: %v", err)
	}
	if mid1.Entry.Heading != "Decisions" || mid1.Entry.LineNumber != 6 {
		t.Fatalf("mid1 entry = %+v, want Heading=Decisions LineNumber=6", mid1.Entry)
	}

	// 2. Middle section via "## Decisions" prefix normalization.
	mid2, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "arch.md",
		Heading:          "## Decisions",
		FactText:         "single writer lock",
		ExpectedRevision: readRev(),
	})
	if err != nil {
		t.Fatalf("CreateEntry middle section normalized: %v", err)
	}
	if mid2.Entry.Heading != "Decisions" || mid2.Entry.LineNumber != 7 {
		t.Fatalf("mid2 entry = %+v, want Heading=Decisions LineNumber=7", mid2.Entry)
	}

	// 3. End-of-file section.
	endSec, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "arch.md",
		Heading:          "Open Questions",
		FactText:         "backup schedule",
		ExpectedRevision: readRev(),
	})
	if err != nil {
		t.Fatalf("CreateEntry end section: %v", err)
	}
	if endSec.Entry.Heading != "Open Questions" || endSec.Entry.LineNumber != 12 {
		t.Fatalf("endSec entry = %+v, want Heading=Open Questions LineNumber=12", endSec.Entry)
	}

	// 4. Non-existent heading creates a new section at EOF.
	newSec, err := service.CreateEntry(ctx, CreateEntryRequest{
		FilePath:         "arch.md",
		Heading:          "References",
		FactText:         "RFC 9110",
		ExpectedRevision: readRev(),
	})
	if err != nil {
		t.Fatalf("CreateEntry new section: %v", err)
	}
	if newSec.Entry.Heading != "References" || newSec.Entry.LineNumber != 16 {
		t.Fatalf("newSec entry = %+v, want Heading=References LineNumber=16", newSec.Entry)
	}

	content := readTestFile(t, filepath.Join(repo, "arch.md"))
	decisionsIdx := strings.Index(content, "## Decisions")
	keepWALIdx := strings.Index(content, "- keep WAL")
	openQIdx := strings.Index(content, "## Open Questions")
	backupIdx := strings.Index(content, "- backup schedule")
	refsIdx := strings.Index(content, "## References")
	rfcIdx := strings.Index(content, "- RFC 9110")
	if !(decisionsIdx < keepWALIdx && keepWALIdx < openQIdx && openQIdx < backupIdx && backupIdx < refsIdx && refsIdx < rfcIdx) {
		t.Fatalf("unexpected section ordering in arch.md:\n%s", content)
	}
}

// TestAppendEntryAndSameTransactionIndex covers baseline-free AppendEntry,
// server-side source/added/until assembly, Session-Origin commit trailers, and
// same-transaction idempotent MEMORY.md ## Index maintenance.
func TestAppendEntryAndSameTransactionIndex(t *testing.T) {
	service, repo := newTestService(t)
	service.SetClock(func() time.Time {
		return time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	})
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. First append to notes/arch.md creates the file and updates MEMORY.md ## Index in one commit.
	first, err := service.AppendEntry(ctx, AppendEntryRequest{
		FilePath:      "notes/arch.md",
		Heading:       "Decisions",
		FactText:      "use exclusive lock for append",
		Until:         "2026-12-31",
		IndexSummary:  "architecture decisions",
		Agent:         "claude-code",
		SessionID:     "sess-host",
		SessionOrigin: hookio.SessionOriginHostFlag,
		Device:        "macbook",
	})
	if err != nil {
		t.Fatalf("AppendEntry first: %v", err)
	}
	if !first.IndexUpdated {
		t.Fatal("expected IndexUpdated=true when creating unindexed notes/arch.md")
	}
	if len(first.Result.ChangedFiles) != 2 {
		t.Fatalf("ChangedFiles = %v, want [MEMORY.md notes/arch.md]", first.Result.ChangedFiles)
	}
	if first.Entry.Heading != "Decisions" || first.Entry.AddedDate != "2026-10-20" {
		t.Fatalf("unexpected first entry: %+v", first.Entry)
	}
	if !strings.Contains(first.Entry.RawLine, "[source: agent://macbook/claude-code/sess-host; added: 2026-10-20; until: 2026-12-31]") {
		t.Fatalf("unexpected raw line: %q", first.Entry.RawLine)
	}
	memContent := readTestFile(t, filepath.Join(repo, "MEMORY.md"))
	if !strings.Contains(memContent, "- [[notes/arch]] — architecture decisions\n") {
		t.Fatalf("MEMORY.md missing index link:\n%s", memContent)
	}
	log1 := runGit(t, repo, "log", "-1", "--format=%B")
	for _, want := range []string{"memctl add", "Agent: claude-code", "Session: sess-host", "Session-Origin: host-flag", "Device: macbook"} {
		if !strings.Contains(log1, want) {
			t.Fatalf("commit message %q missing %q", log1, want)
		}
	}

	// 2. Second append to notes/arch.md is idempotent on MEMORY.md and records Session-Origin: model.
	second, err := service.AppendEntry(ctx, AppendEntryRequest{
		FilePath:      "notes/arch.md",
		Heading:       "Open Questions",
		FactText:      "evaluate shard count",
		IndexSummary:  "ignored on already-linked file",
		Agent:         "claude-code",
		SessionID:     "sess-model",
		SessionOrigin: hookio.SessionOriginModel,
		Device:        "macbook",
	})
	if err != nil {
		t.Fatalf("AppendEntry second: %v", err)
	}
	if second.IndexUpdated {
		t.Fatal("expected IndexUpdated=false when notes/arch.md is already linked in MEMORY.md")
	}
	if len(second.Result.ChangedFiles) != 1 || second.Result.ChangedFiles[0] != "notes/arch.md" {
		t.Fatalf("ChangedFiles = %v, want [notes/arch.md]", second.Result.ChangedFiles)
	}
	if got := strings.Count(readTestFile(t, filepath.Join(repo, "MEMORY.md")), "[[notes/arch]]"); got != 1 {
		t.Fatalf("MEMORY.md link count = %d, want 1", got)
	}
	log2 := runGit(t, repo, "log", "-1", "--format=%B")
	if !strings.Contains(log2, "Session-Origin: model") {
		t.Fatalf("commit message %q missing Session-Origin: model", log2)
	}

	// 3. Third append without SessionID uses 2-segment source URI and omits Session-Origin trailer.
	third, err := service.AppendEntry(ctx, AppendEntryRequest{
		FilePath: "notes/arch.md",
		Heading:  "## Decisions",
		FactText: "insert at end of middle section",
		Agent:    "codex",
		Device:   "workstation",
	})
	if err != nil {
		t.Fatalf("AppendEntry third: %v", err)
	}
	if !strings.Contains(third.Entry.RawLine, "[source: agent://workstation/codex; added: 2026-10-20]") {
		t.Fatalf("unexpected raw line without sid: %q", third.Entry.RawLine)
	}
	log3 := runGit(t, repo, "log", "-1", "--format=%B")
	if strings.Contains(log3, "Session-Origin:") || strings.Contains(log3, "Session:") {
		t.Fatalf("commit without sid should not contain Session trailers: %q", log3)
	}
	archContent := readTestFile(t, filepath.Join(repo, "notes/arch.md"))
	if strings.Index(archContent, "insert at end of middle section") > strings.Index(archContent, "## Open Questions") {
		t.Fatalf("middle section entry was not placed before ## Open Questions:\n%s", archContent)
	}

	// 4. Doctor reports zero ORPHAN_FILE.
	report, err := service.Doctor(ctx)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, issue := range report.Issues {
		if issue.Code == "ORPHAN_FILE" {
			t.Fatalf("unexpected ORPHAN_FILE issue: %+v", issue)
		}
	}

	// 5. Validation errors.
	badInputs := []struct {
		name string
		req  AppendEntryRequest
		code memerr.Code
	}{
		{
			name: "empty fact",
			req:  AppendEntryRequest{FilePath: "MEMORY.md", FactText: "   "},
			code: memerr.InvalidArgument,
		},
		{
			name: "multiline fact",
			req:  AppendEntryRequest{FilePath: "MEMORY.md", FactText: "line1\nline2"},
			code: memerr.InvalidArgument,
		},
		{
			name: "handwritten source metadata",
			req:  AppendEntryRequest{FilePath: "MEMORY.md", FactText: "spoof [source: agent://evil/x/y]"},
			code: memerr.InvalidArgument,
		},
		{
			name: "handwritten added metadata",
			req:  AppendEntryRequest{FilePath: "MEMORY.md", FactText: "spoof [added: 2020-01-01]"},
			code: memerr.InvalidArgument,
		},
		{
			name: "expired placeholder",
			req:  AppendEntryRequest{FilePath: "MEMORY.md", FactText: vault.ExpiredFactPlaceholder},
			code: memerr.InvalidArgument,
		},
		{
			name: "invalid until date",
			req:  AppendEntryRequest{FilePath: "MEMORY.md", FactText: "valid fact", Until: "2026-99-99"},
			code: memerr.InvalidArgument,
		},
		{
			name: "non-markdown file",
			req:  AppendEntryRequest{FilePath: "query.sql", FactText: "SELECT 1"},
			code: memerr.InvalidArgument,
		},
		{
			name: "missing parent directory",
			req:  AppendEntryRequest{FilePath: "missing/topic.md", FactText: "valid fact"},
			code: memerr.NotFound,
		},
	}
	for _, tc := range badInputs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.AppendEntry(ctx, tc.req)
			requireContractCode(t, err, tc.code)
		})
	}
}

// TestAppendEntryConcurrentZeroRevisionMismatch verifies that concurrent callers
// appending to both the same topic file and distinct topic files never fail with
// REVISION_MISMATCH and leave no ORPHAN_FILE issues.
func TestAppendEntryConcurrentZeroRevisionMismatch(t *testing.T) {
	service, repo := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)

	for i := 0; i < workers; i++ {
		i := i
		wg.Add(2)
		// Goroutine A: append to shared topic file.
		go func() {
			defer wg.Done()
			_, err := service.AppendEntry(ctx, AppendEntryRequest{
				FilePath:      "notes/shared.md",
				Heading:       "Decisions",
				FactText:      fmt.Sprintf("shared decision %d", i),
				IndexSummary:  "shared notes",
				Agent:         "claude-code",
				SessionID:     fmt.Sprintf("sid-%d", i),
				SessionOrigin: hookio.SessionOriginEnv,
				Device:        "dev",
			})
			if err != nil {
				errs <- fmt.Errorf("shared worker %d: %w", i, err)
			}
		}()
		// Goroutine B: append to per-worker topic file.
		go func() {
			defer wg.Done()
			_, err := service.AppendEntry(ctx, AppendEntryRequest{
				FilePath:      fmt.Sprintf("notes/topic-%d.md", i),
				FactText:      fmt.Sprintf("topic fact %d", i),
				IndexSummary:  fmt.Sprintf("topic %d", i),
				Agent:         "codex",
				SessionID:     fmt.Sprintf("sid-%d", i),
				SessionOrigin: hookio.SessionOriginHostFlag,
				Device:        "dev",
			})
			if err != nil {
				errs <- fmt.Errorf("topic worker %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent AppendEntry failed: %v", err)
	}

	sharedContent := readTestFile(t, filepath.Join(repo, "notes/shared.md"))
	for i := 0; i < workers; i++ {
		want := fmt.Sprintf("shared decision %d", i)
		if !strings.Contains(sharedContent, want) {
			t.Fatalf("notes/shared.md missing %q:\n%s", want, sharedContent)
		}
	}
	memContent := readTestFile(t, filepath.Join(repo, "MEMORY.md"))
	if got := strings.Count(memContent, "[[notes/shared]]"); got != 1 {
		t.Fatalf("MEMORY.md [[notes/shared]] count = %d, want 1:\n%s", got, memContent)
	}
	for i := 0; i < workers; i++ {
		target := fmt.Sprintf("[[notes/topic-%d]]", i)
		if got := strings.Count(memContent, target); got != 1 {
			t.Fatalf("MEMORY.md %s count = %d, want 1:\n%s", target, got, memContent)
		}
	}

	report, err := service.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, issue := range report.Issues {
		if issue.Code == "ORPHAN_FILE" {
			t.Fatalf("unexpected ORPHAN_FILE after concurrent appends: %+v", issue)
		}
	}
}
