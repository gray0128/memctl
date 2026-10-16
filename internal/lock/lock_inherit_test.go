//go:build unix

package lock

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// TestReleaseKeepsLockWhileInheritedDescriptorIsOpen covers P3-2: the lock
// belongs to the open file description, so releasing the parent handle must not
// unlock while a Git child still holds an inherited descriptor. A duplicated
// descriptor models that child.
func TestReleaseKeepsLockWhileInheritedDescriptorIsOpen(t *testing.T) {
	dir := t.TempDir()
	held, ok, err := TryAcquire(dir)
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	inheritedFD, err := unix.Dup(int(held.File().Fd()))
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	inherited := os.NewFile(uintptr(inheritedFD), "inherited")

	if err := held.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, ok, err := TryAcquire(dir); err != nil || ok {
		t.Fatalf("lock was released while the inherited descriptor was open (ok=%v err=%v)", ok, err)
	}
	if err := inherited.Close(); err != nil {
		t.Fatalf("close inherited: %v", err)
	}
	reacquired, ok, err := TryAcquire(dir)
	if err != nil || !ok {
		t.Fatalf("lock was not released after the last descriptor closed (ok=%v err=%v)", ok, err)
	}
	_ = reacquired.Release()
}
