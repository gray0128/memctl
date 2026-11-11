package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/assets"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

func TestT141MidRebaseDetachedHeadSnapshotReadAndExpiry(t *testing.T) {
	service, ctx := newLifecycleService(t)
	repo := service.RepoPath()

	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}
	memContent := strings.Join([]string{
		"# Memory",
		"",
		"## Working preferences",
		"- Keep commits atomic [source: agent://dev/claude/s1; added: 2026-04-15]",
		"- Expired root fact [source: agent://dev/claude/s1; added: 2026-04-01; until: 2026-04-10]",
		"",
		"## Index",
		"- [[notes/nebula]] — Nebula service notes",
		"",
	}, "\n")
	writeFile(t, service, ctx, "MEMORY.md", memContent)

	noteContent := strings.Join([]string{
		"# Nebula",
		"",
		"- Older active note [source: agent://dev/claude/s1; added: 2026-04-12]",
		"- Expired note fact [source: agent://dev/claude/s1; added: 2026-04-01; until: 2026-04-09]",
		"- Newer active note [source: agent://dev/claude/s2; added: 2026-04-19]",
		"",
	}, "\n")
	if _, err := service.Apply(ctx, MutationRequest{
		Agent: "claude", SessionID: "s2", Device: "dev",
		Changes: []FileChange{{
			Path:             "notes/nebula.md",
			ExpectedRevision: vault.AbsentRevision,
			Action:           ActionPut,
			RawContent:       noteContent,
		}},
	}); err != nil {
		t.Fatalf("create notes/nebula.md: %v", err)
	}

	wantMem, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("locked ReadFile(MEMORY.md): %v", err)
	}
	wantNote, err := service.ReadFile(ctx, "notes/nebula.md")
	if err != nil {
		t.Fatalf("locked ReadFile(notes/nebula.md): %v", err)
	}
	wantEntries, err := service.ListEntries(ctx, EntryQuery{})
	if err != nil {
		t.Fatalf("locked ListEntries: %v", err)
	}

	gitDir, err := git.GitDir(ctx, repo)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}
	mainHead, err := git.Head(ctx, repo)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	firstCommit, err := git.RevParse(ctx, repo, "HEAD~1")
	if err != nil {
		t.Fatalf("RevParse HEAD~1: %v", err)
	}

	// Simulate an active `git rebase`: detach HEAD to an earlier commit while
	// `refs/heads/main` still points at `mainHead`, record `refs/heads/main` in
	// `.git/rebase-merge/head-name`, and dirty the worktree files on disk.
	if _, err := git.Run(ctx, repo, "checkout", "-q", "--detach", firstCommit); err != nil {
		t.Fatalf("detach HEAD: %v", err)
	}
	rebaseMergeDir := filepath.Join(gitDir, "rebase-merge")
	if err := os.MkdirAll(rebaseMergeDir, 0o700); err != nil {
		t.Fatalf("mkdir rebase-merge: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rebaseMergeDir, "head-name"), []byte("refs/heads/main\n"), 0o600); err != nil {
		t.Fatalf("write head-name: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(rebaseMergeDir)
		_, _ = git.Run(ctx, repo, "checkout", "-q", "-f", "main")
	}()

	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Dirty worktree mid-rebase\n"), 0o644); err != nil {
		t.Fatalf("dirty MEMORY.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("mkdir notes after detach: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes", "nebula.md"), []byte("# Dirty note mid-rebase\n"), 0o644); err != nil {
		t.Fatalf("dirty notes/nebula.md: %v", err)
	}

	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer held.Release()

	snapCommit, err := git.SnapshotCommit(ctx, repo, gitDir)
	if err != nil {
		t.Fatalf("SnapshotCommit: %v", err)
	}
	if snapCommit != mainHead {
		t.Fatalf("SnapshotCommit = %q, want pre-rebase branch commit %q", snapCommit, mainHead)
	}

	gotMem, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("lock-busy ReadFile(MEMORY.md): %v", err)
	}
	if gotMem.FileRevision != wantMem.FileRevision || gotMem.RawContent != wantMem.RawContent || gotMem.HiddenExpired != 1 {
		t.Fatalf("lock-busy ReadFile(MEMORY.md) mismatch:\ngot  rev=%s hidden=%d content=%q\nwant rev=%s hidden=%d content=%q",
			gotMem.FileRevision, gotMem.HiddenExpired, gotMem.RawContent,
			wantMem.FileRevision, wantMem.HiddenExpired, wantMem.RawContent)
	}
	if strings.Contains(gotMem.RawContent, "Dirty worktree") || strings.Contains(gotMem.RawContent, "Expired root fact") {
		t.Fatalf("lock-busy ReadFile(MEMORY.md) leaked dirty or expired content: %q", gotMem.RawContent)
	}

	gotNote, err := service.ReadFile(ctx, "notes/nebula.md")
	if err != nil {
		t.Fatalf("lock-busy ReadFile(notes/nebula.md): %v", err)
	}
	if gotNote.FileRevision != wantNote.FileRevision || gotNote.RawContent != wantNote.RawContent || gotNote.HiddenExpired != 1 {
		t.Fatalf("lock-busy ReadFile(notes/nebula.md) mismatch: got=%+v want=%+v", gotNote, wantNote)
	}

	// Path validation and NOT_FOUND boundaries must match locked reads.
	_, err = service.ReadFile(ctx, "../outside.md")
	requireContractCode(t, err, memerr.InvalidPath)
	_, err = service.ReadFile(ctx, ".git/config")
	requireContractCode(t, err, memerr.InvalidPath)
	_, err = service.ReadFile(ctx, "notes")
	requireContractCode(t, err, memerr.InvalidPath)
	_, err = service.ReadFile(ctx, "notes/absent.md")
	requireContractCode(t, err, memerr.NotFound)

	gotEntries, err := service.ListEntries(ctx, EntryQuery{})
	if err != nil {
		t.Fatalf("lock-busy ListEntries: %v", err)
	}
	if gotEntries.Revision != wantEntries.Revision || gotEntries.Total != wantEntries.Total {
		t.Fatalf("lock-busy ListEntries = (rev=%s total=%d), want (rev=%s total=%d)",
			gotEntries.Revision, gotEntries.Total, wantEntries.Revision, wantEntries.Total)
	}
	for _, item := range gotEntries.Items {
		if strings.Contains(item.FactText, "Expired") || strings.Contains(item.FactText, "Dirty") {
			t.Fatalf("lock-busy ListEntries leaked item: %+v", item)
		}
	}

	outcome, err := service.PrepareSession(ctx, SessionOptions{
		Agent:           "claude",
		SessionID:       "s-busy-rebase",
		Cwd:             "/workspace/nebula",
		NonBlockingLock: true,
	})
	if err != nil {
		t.Fatalf("PrepareSession while lock busy: %v", err)
	}
	if outcome.SyncState != SessionSyncBusy {
		t.Fatalf("SyncState = %q, want %q", outcome.SyncState, SessionSyncBusy)
	}
	if !strings.Contains(outcome.Body, "Keep commits atomic") ||
		!strings.Contains(outcome.Body, vault.ExpiredFactPlaceholder) ||
		strings.Contains(outcome.Body, "Expired root fact") ||
		strings.Contains(outcome.Body, "Dirty worktree") {
		t.Fatalf("unexpected injected MEMORY.md body during mid-rebase lock busy:\n%s", outcome.Body)
	}
	if !strings.Contains(outcome.Body, "----- CURRENT PROJECT MEMORY (nebula) -----") ||
		!strings.Contains(outcome.Body, "[notes/nebula.md] Newer active note") ||
		strings.Contains(outcome.Body, "Expired note fact") {
		t.Fatalf("unexpected project dashboard during mid-rebase lock busy:\n%s", outcome.Body)
	}
}

func TestT141MemoryTruncationPrioritizesWorkingPreferencesAndProjectIndex(t *testing.T) {
	service, ctx := newLifecycleService(t)

	var lines []string
	lines = append(lines, "# Memory", "", "## Misc backlog")
	for i := 1; i <= 230; i++ {
		lines = append(lines, fmt.Sprintf("- Backlog item %03d 多字节安全校验 [source: agent://dev/claude/s1; added: 2026-04-10]", i))
	}
	lines = append(lines,
		"",
		"## Working preferences",
		"- Run focused tests before full suite [source: agent://dev/claude/s1; added: 2026-04-18]",
		"- Preserve user uncommitted work [source: agent://dev/claude/s1; added: 2026-04-19]",
		"",
		"## Index",
		"- [[notes/Orion]] — Orion core architecture",
		"",
	)
	writeFile(t, service, ctx, "MEMORY.md", strings.Join(lines, "\n"))

	outcome, err := service.PrepareSession(ctx, SessionOptions{
		Agent:     "claude",
		SessionID: "s-trunc",
		Cwd:       "/workspace/orion",
	})
	if err != nil {
		t.Fatalf("PrepareSession: %v", err)
	}
	if !utf8.ValidString(outcome.Body) {
		t.Fatal("injected body is not valid UTF-8")
	}
	for _, want := range []string{
		"## Working preferences",
		"Run focused tests before full suite",
		"Preserve user uncommitted work",
		"## Index",
		"[[notes/Orion]] — Orion core architecture",
		MemoryTruncatedNotice,
	} {
		if !strings.Contains(outcome.Body, want) {
			t.Fatalf("truncated session body missing %q:\n%s", want, outcome.Body)
		}
	}
	if strings.Contains(outcome.Body, "Backlog item 230") {
		t.Fatalf("expected excess backlog lines to be truncated:\n%s", outcome.Body)
	}
}

func TestT141ProjectDashboardPreloadAndUnrelatedIndexFolding(t *testing.T) {
	service, ctx := newLifecycleService(t)
	repo := service.RepoPath()

	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}

	var indexLines []string
	indexLines = append(indexLines, "# Memory", "", "## Working preferences", "- Prefer simple Go code", "", "## Index")
	for i := 1; i <= 25; i++ {
		indexLines = append(indexLines, fmt.Sprintf("- [[notes/other-%02d]] — Unrelated project %02d", i, i))
	}
	indexLines = append(indexLines,
		"- [[notes/phoenix]] — Phoenix main notes",
		"- [[notes/PHOENIX-auth]] — Phoenix auth subtopic",
		"- [[notes/shared-infra]] — Shared infra for Phoenix [project: phoenix]",
		"",
	)
	fullMem := strings.Join(indexLines, "\n")
	writeFile(t, service, ctx, "MEMORY.md", fullMem)

	noteFiles := map[string]string{
		"notes/phoenix.md": strings.Join([]string{
			"# Phoenix",
			"",
			"- Oldest phoenix fact [source: agent://dev/claude/s1; added: 2026-04-11]",
			"- Expired phoenix fact [source: agent://dev/claude/s1; added: 2026-04-01; until: 2026-04-05]",
			"",
		}, "\n"),
		"notes/PHOENIX-auth.md": strings.Join([]string{
			"# Phoenix Auth",
			"",
			"- Newest phoenix auth fact [source: agent://dev/claude/s2; added: 2026-04-19]",
			"",
		}, "\n"),
		"notes/shared-infra.md": strings.Join([]string{
			"# Shared Infra",
			"",
			"- Middle phoenix infra fact [source: agent://dev/claude/s3; added: 2026-04-15]",
			"",
		}, "\n"),
	}
	var changes []FileChange
	for path, content := range noteFiles {
		changes = append(changes, FileChange{
			Path:             path,
			ExpectedRevision: vault.AbsentRevision,
			Action:           ActionPut,
			RawContent:       content,
		})
	}
	if _, err := service.Apply(ctx, MutationRequest{
		Agent: "claude", SessionID: "s-notes", Device: "dev",
		Changes: changes,
	}); err != nil {
		t.Fatalf("seed project notes: %v", err)
	}

	outcome, err := service.PrepareSession(ctx, SessionOptions{
		Agent:     "claude",
		SessionID: "s-phoenix",
		Cwd:       "/workspace/phoenix",
	})
	if err != nil {
		t.Fatalf("PrepareSession: %v", err)
	}

	// First 20 unrelated index lines are kept; remaining 5 are folded.
	if !strings.Contains(outcome.Body, "[[notes/other-20]]") {
		t.Fatalf("expected 20th unrelated index line to be kept:\n%s", outcome.Body)
	}
	if strings.Contains(outcome.Body, "[[notes/other-21]]") || strings.Contains(outcome.Body, "[[notes/other-25]]") {
		t.Fatalf("expected unrelated index lines 21..25 to be folded:\n%s", outcome.Body)
	}
	wantFold := "[+5 other project topics omitted; query with 'memctl entries --q <keyword>']"
	if !strings.Contains(outcome.Body, wantFold) {
		t.Fatalf("missing fold notice %q:\n%s", wantFold, outcome.Body)
	}
	// Related project index lines after the 25 unrelated ones are still kept.
	for _, related := range []string{"[[notes/phoenix]]", "[[notes/PHOENIX-auth]]", "[[notes/shared-infra]]"} {
		if !strings.Contains(outcome.Body, related) {
			t.Fatalf("missing related index line %q:\n%s", related, outcome.Body)
		}
	}

	// ReadFile(MEMORY.md) and on-disk MEMORY.md must remain completely unfolded.
	readMem, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("ReadFile(MEMORY.md): %v", err)
	}
	if !strings.Contains(readMem.RawContent, "[[notes/other-25]]") || strings.Contains(readMem.RawContent, "other project topics omitted") {
		t.Fatalf("ReadFile(MEMORY.md) must not fold index lines:\n%s", readMem.RawContent)
	}

	// Verify CURRENT PROJECT MEMORY section ordering (added date descending) and expiry filtering.
	if !strings.Contains(outcome.Body, "----- CURRENT PROJECT MEMORY (phoenix) -----") {
		t.Fatalf("missing CURRENT PROJECT MEMORY section:\n%s", outcome.Body)
	}
	if strings.Contains(outcome.Body, "Expired phoenix fact") {
		t.Fatalf("expired project entry must not be preloaded:\n%s", outcome.Body)
	}
	idxNewest := strings.Index(outcome.Body, "[notes/PHOENIX-auth.md] Newest phoenix auth fact")
	idxMiddle := strings.Index(outcome.Body, "[notes/shared-infra.md] Middle phoenix infra fact")
	idxOldest := strings.Index(outcome.Body, "[notes/phoenix.md] Oldest phoenix fact")
	if idxNewest < 0 || idxMiddle < 0 || idxOldest < 0 {
		t.Fatalf("missing expected project entries in dashboard:\n%s", outcome.Body)
	}
	if !(idxNewest < idxMiddle && idxMiddle < idxOldest) {
		t.Fatalf("project entries not ordered by added descending (idxNewest=%d idxMiddle=%d idxOldest=%d):\n%s",
			idxNewest, idxMiddle, idxOldest, outcome.Body)
	}

	// Host config directory must skip project preloading.
	hostCfgOutcome, err := service.PrepareSession(ctx, SessionOptions{
		Agent:     "claude",
		SessionID: "s-hostcfg",
		Cwd:       "/Users/dev/.gemini/config",
	})
	if err != nil {
		t.Fatalf("PrepareSession host config dir: %v", err)
	}
	if strings.Contains(hostCfgOutcome.Body, "----- CURRENT PROJECT MEMORY") {
		t.Fatalf("host config dir must not trigger project memory preload:\n%s", hostCfgOutcome.Body)
	}
}

func TestT141EmbeddedInstructionsBudget(t *testing.T) {
	lines := strings.Count(strings.TrimRight(assets.Instructions, "\n"), "\n") + 1
	if lines > 30 {
		t.Fatalf("assets.Instructions has %d lines, want <= 30", lines)
	}
	if len(assets.Instructions) > 1500 {
		t.Fatalf("assets.Instructions is %d bytes, want <= 1500", len(assets.Instructions))
	}
}
