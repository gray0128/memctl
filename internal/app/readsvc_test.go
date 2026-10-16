package app

import (
	"context"
	"testing"
	"time"

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
