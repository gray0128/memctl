package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/doctor"
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

	fixed, err := service.DoctorFix(ctx, DoctorFixRequest{})
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
	_, err = service.DoctorFix(ctx, DoctorFixRequest{IssueCodes: []string{"ORPHAN_FILE"}})
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
