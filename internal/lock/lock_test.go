package lock

import (
	"context"
	"testing"
	"time"
)

// TestTryAcquireExcludesAndExposesFile covers the lock primitives the
// background worker and the Git inheritance mechanism rely on.
func TestTryAcquireExcludesAndExposesFile(t *testing.T) {
	dir := t.TempDir()

	first, ok, err := TryAcquire(dir)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire did not take a free lock")
	}
	if first.File() == nil {
		t.Fatal("Lock.File returned nil while held")
	}
	if _, ok, err := TryAcquire(dir); err != nil || ok {
		t.Fatalf("second TryAcquire: ok=%v err=%v, want false", ok, err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second, ok, err := TryAcquire(dir)
	if err != nil {
		t.Fatalf("TryAcquire after release: %v", err)
	}
	if !ok {
		t.Fatal("lock was not released")
	}
	if err := second.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

// TestAcquireWaitsWithinBudget covers the foreground blocking acquire path.
func TestAcquireWaitsWithinBudget(t *testing.T) {
	dir := t.TempDir()
	held, ok, err := TryAcquire(dir)
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, dir); err == nil {
		t.Fatal("Acquire returned while the lock was held")
	}
	_ = held.Release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	free, err := Acquire(ctx2, dir)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	_ = free.Release()
}
