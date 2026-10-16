package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// applyPut creates or replaces one managed file and returns the result.
func applyPut(t *testing.T, service *App, repo, path, content, expected string) MutationResult {
	t.Helper()
	result, err := service.Apply(context.Background(), MutationRequest{
		Changes: []FileChange{{
			Path:             path,
			ExpectedRevision: expected,
			Action:           ActionPut,
			RawContent:       content,
		}},
	})
	if err != nil {
		t.Fatalf("apply put %s: %v", path, err)
	}
	return result
}

func snapshotRevision(t *testing.T, service *App) string {
	t.Helper()
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot.Revision
}

// TestMoveFileRewritesResolvedLinks covers the move half of C-1: a moved file's
// resolved links are rewritten in every other file, and links that did not
// resolve to it (inline code, dead targets) keep their bytes.
func TestMoveFileRewritesResolvedLinks(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "beta.md", "# Beta\n\n- second fact\n", vault.AbsentRevision)
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- see [[beta]]\n- literal `[[beta]]`\n- dead [[missing]]\n", vault.AbsentRevision)
	if err := os.Mkdir(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	revision := snapshotRevision(t, service)

	result, err := service.MoveFile(ctx, MoveFileRequest{
		FromPath:     "beta.md",
		ToPath:       "notes/gamma.md",
		BaseRevision: revision,
	})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !containsString(result.ChangedFiles, "beta.md") || !containsString(result.ChangedFiles, "notes/gamma.md") || !containsString(result.ChangedFiles, "alpha.md") {
		t.Fatalf("changed files = %v, want both move sides and the link source", result.ChangedFiles)
	}
	if got := readTestFile(t, filepath.Join(repo, "notes", "gamma.md")); got != "# Beta\n\n- second fact\n" {
		t.Fatalf("moved content = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(repo, "beta.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists after the move: %v", err)
	}
	alpha := readTestFile(t, filepath.Join(repo, "alpha.md"))
	if !strings.Contains(alpha, "[[notes/gamma]]") {
		t.Fatalf("resolved link was not rewritten: %q", alpha)
	}
	if !strings.Contains(alpha, "`[[beta]]`") {
		t.Fatalf("inline code link was rewritten: %q", alpha)
	}
	if !strings.Contains(alpha, "[[missing]]") {
		t.Fatalf("dead link was changed: %q", alpha)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the move: %q", status)
	}
}

// TestMoveFileCaseOnlyRename covers a rename that only changes letter case,
// which must work on a case-insensitive filesystem.
func TestMoveFileCaseOnlyRename(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "Notes.md", "# Notes\n\n- fact\n", vault.AbsentRevision)
	revision := snapshotRevision(t, service)

	if _, err := service.MoveFile(ctx, MoveFileRequest{
		FromPath:     "Notes.md",
		ToPath:       "notes.md",
		BaseRevision: revision,
	}); err != nil {
		t.Fatalf("case-only move: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "notes.md")); err != nil {
		t.Fatalf("renamed file is missing: %v", err)
	}
	entries, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".memctl-move") {
			t.Fatalf("leftover rename intermediate %q", entry.Name())
		}
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the case-only rename: %q", status)
	}
}

// TestMoveFileRefusesOverwriteAndStaleBase covers the A04 preconditions of the
// move route: an existing destination and a stale repository baseline change
// nothing.
func TestMoveFileRefusesOverwriteAndStaleBase(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "one.md", "# One\n\n- fact\n", vault.AbsentRevision)
	applyPut(t, service, repo, "two.md", "# Two\n\n- fact\n", vault.AbsentRevision)
	revision := snapshotRevision(t, service)

	_, err := service.MoveFile(ctx, MoveFileRequest{FromPath: "one.md", ToPath: "two.md", BaseRevision: revision})
	requireContractCode(t, err, memerr.InvalidArgument)
	if got := readTestFile(t, filepath.Join(repo, "one.md")); got != "# One\n\n- fact\n" {
		t.Fatalf("refused overwrite changed the source: %q", got)
	}
	if got := readTestFile(t, filepath.Join(repo, "two.md")); got != "# Two\n\n- fact\n" {
		t.Fatalf("refused overwrite changed the destination: %q", got)
	}

	stale := revision
	applyPut(t, service, repo, "three.md", "# Three\n\n- fact\n", vault.AbsentRevision)
	_, err = service.MoveFile(ctx, MoveFileRequest{FromPath: "three.md", ToPath: "four.md", BaseRevision: stale})
	requireContractCode(t, err, memerr.RevisionMismatch)

	if _, err := os.Lstat(filepath.Join(repo, "four.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale move created the destination: %v", err)
	}
}

// TestMoveInterruptionRollsBack covers W04 for a move: an interruption during
// the file replacement leaves an unfinished transaction that restores every
// pre-image.
func TestMoveInterruptionRollsBack(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "beta.md", "# Beta\n\n- fact\n", vault.AbsentRevision)
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- see [[beta]]\n", vault.AbsentRevision)
	revision := snapshotRevision(t, service)
	headBefore := headHash(t, repo)
	alphaBefore := readTestFile(t, filepath.Join(repo, "alpha.md"))
	betaBefore := readTestFile(t, filepath.Join(repo, "beta.md"))

	service.testFault = func(stage string) error {
		// Fail after the source file was moved but before the link source is
		// rewritten, so the transaction is partially applied.
		if stage == "after-write:0" {
			return errSimulatedCrash
		}
		return nil
	}
	_, err := service.MoveFile(ctx, MoveFileRequest{FromPath: "beta.md", ToPath: "gamma.md", BaseRevision: revision})
	service.testFault = nil
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("move err = %v, want simulated crash", err)
	}

	status, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatalf("recover status: %v", err)
	}
	if len(status.Transactions) != 1 || len(status.Transactions[0].Files) != 3 {
		t.Fatalf("unexpected recover status: %+v", status)
	}

	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("recover rollback: %v", err)
	}
	if headHash(t, repo) != headBefore {
		t.Fatal("recovery changed HEAD")
	}
	if got := readTestFile(t, filepath.Join(repo, "beta.md")); got != betaBefore {
		t.Fatalf("beta.md = %q, want the pre-image", got)
	}
	if got := readTestFile(t, filepath.Join(repo, "alpha.md")); got != alphaBefore {
		t.Fatalf("alpha.md = %q, want the pre-image", got)
	}
	if _, err := os.Lstat(filepath.Join(repo, "gamma.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("move destination survived recovery: %v", err)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after recovery: %q", status)
	}
}

// TestMoveFileRejectsInvalidPaths covers the HTTP-adjacent boundary of the move
// route for P01-style inputs.
func TestMoveFileRejectsInvalidPaths(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "one.md", "# One\n\n- fact\n", vault.AbsentRevision)
	revision := snapshotRevision(t, service)

	for _, bad := range []string{"../escape.md", ".git/config", "NUL"} {
		_, err := service.MoveFile(ctx, MoveFileRequest{FromPath: "one.md", ToPath: bad, BaseRevision: revision})
		if err == nil {
			t.Fatalf("move to %q succeeded", bad)
		}
		requireContractCode(t, err, memerr.InvalidPath)
	}
	if runtime.GOOS != "windows" {
		// A symbolic link is not managed memory and cannot be moved out of the
		// repository or treated as text. Commit it first so the clean-baseline
		// check is not what stops the move.
		link := filepath.Join(repo, "link.md")
		if err := os.Symlink("MEMORY.md", link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		runGit(t, repo, "add", "--", "link.md")
		runGit(t, repo, "commit", "-q", "-m", "add symlink")
		revision = snapshotRevision(t, service)
		_, err := service.MoveFile(ctx, MoveFileRequest{FromPath: "link.md", ToPath: "elsewhere.md", BaseRevision: revision})
		requireContractCode(t, err, memerr.Unsupported)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
