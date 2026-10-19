package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
)

// TestSnapshotServesStaleInProtectedState verifies the documented stale branch:
// once a complete snapshot exists, a protected state returns it with
// stale: true instead of BUSY.
func TestSnapshotServesStaleInProtectedState(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	fresh, err := service.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if fresh.Stale {
		t.Fatal("a clean repository must not report stale")
	}

	gitDir, err := git.GitDir(ctx, repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("hold the repository lock: ok=%v err=%v", ok, err)
	}
	defer held.Release()

	stale, err := service.Snapshot(ctx)
	if err != nil {
		t.Fatalf("stale snapshot: %v", err)
	}
	if !stale.Stale {
		t.Fatal("a protected state must return the cached snapshot with stale: true")
	}
	if stale.Revision != fresh.Revision || len(stale.Files) != len(fresh.Files) {
		t.Fatalf("stale snapshot content changed: %+v", stale)
	}
}

// TestConflictStateDoesNotWaitForLock verifies that the read-only conflict view
// takes the lock without waiting, unlike the blocking resolve write path.
func TestConflictStateDoesNotWaitForLock(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	gitDir, err := git.GitDir(ctx, repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("hold the repository lock: ok=%v err=%v", ok, err)
	}
	defer held.Release()

	start := time.Now()
	_, _, err = service.ConflictState(ctx)
	if err == nil {
		t.Fatal("a conflict read should report the busy repository lock")
	}
	if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.Busy {
		t.Fatalf("expected BUSY, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("conflict read waited %v for the repository lock", elapsed)
	}
}

// TestAuditResolvesRootAndWorkflowSource covers the provenance gap behind the
// audit view's "未记录来源": every commit memctl creates records its source, so
// only history written by something else can lack one. The repository
// initialization commit and the managed Dreaming workflow commit must both
// resolve.
func TestAuditResolvesRootAndWorkflowSource(t *testing.T) {
	isolateGit(t)
	repo := t.TempDir()
	service := New(config.Config{
		RepoPath: config.Canonical(repo),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})
	service.SetCommitSource(git.Source{Agent: git.LocalAgent, Device: "laptop"})
	ctx := context.Background()

	if _, err := service.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := service.InitDreamWorkflow(ctx, "v0.7.0"); err != nil {
		t.Fatalf("init workflow: %v", err)
	}

	page, err := service.Audit(ctx, AuditQuery{Limit: 2})
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(page.Commits) != 2 {
		t.Fatalf("audit returned %d commits, want 2", len(page.Commits))
	}
	workflow := page.Commits[0]
	if workflow.Subject != "memctl dream: install the Dreaming workflow" {
		t.Fatalf("newest commit = %q", workflow.Subject)
	}
	if workflow.Agent != git.LocalAgent || workflow.Device != "laptop" {
		t.Fatalf("workflow commit source = %+v, want agent %q on laptop", workflow, git.LocalAgent)
	}
	root := page.Commits[1]
	if root.Subject != "Initialize memory repository" {
		t.Fatalf("second commit = %q", root.Subject)
	}
	if root.Agent != git.LocalAgent || root.Device != "laptop" {
		t.Fatalf("root commit source = %+v, want agent %q on laptop", root, git.LocalAgent)
	}
}
