package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordSink stores every invalidation for later assertions.
type recordSink struct {
	mu     sync.Mutex
	events []Invalidation
}

func (s *recordSink) Publish(inv Invalidation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, inv)
}

func (s *recordSink) snapshot() []Invalidation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Invalidation(nil), s.events...)
}

func (s *recordSink) countContaining(substr string) int {
	count := 0
	for _, event := range s.snapshot() {
		for _, file := range event.ChangedFiles {
			if strings.Contains(file, substr) {
				count++
				break
			}
		}
	}
	return count
}

func waitFor(t *testing.T, sink *recordSink, description string, predicate func([]Invalidation) bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if predicate(sink.snapshot()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; events: %+v", description, sink.snapshot())
}

func waitForPath(t *testing.T, sink *recordSink, path string) {
	t.Helper()
	waitFor(t, sink, path, func(events []Invalidation) bool {
		for _, event := range events {
			for _, file := range event.ChangedFiles {
				if file == path {
					return true
				}
			}
		}
		return false
	})
}

func newFixture(t *testing.T) (string, *recordSink, *Watcher) {
	t.Helper()
	root := t.TempDir()
	sink := &recordSink{}
	watcher, err := New(root, sink)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = watcher.Watch(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = watcher.Close()
	})
	return root, sink, watcher
}

// TestWatcherRecursivelyRegistersNewDirectories covers the recursive add of a
// newly created directory tree, so a deeper file change is still reported.
func TestWatcherRecursivelyRegistersNewDirectories(t *testing.T) {
	root, sink, _ := newFixture(t)

	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Wait until the first-level directory event was processed, which also
	// registers the nested directory watch.
	waitForPath(t, sink, "a")

	if err := os.WriteFile(filepath.Join(root, "a", "b", "deep.md"), []byte("- deep\n"), 0o644); err != nil {
		t.Fatalf("write deep file: %v", err)
	}
	waitForPath(t, sink, "a/b/deep.md")
}

// TestWatcherCoversRenameRemoveAndAtomicReplace covers the A05 directory
// mutation scenarios: rename, remove and an atomic replace via rename.
func TestWatcherCoversRenameRemoveAndAtomicReplace(t *testing.T) {
	root, sink, _ := newFixture(t)

	one := filepath.Join(root, "one.md")
	if err := os.WriteFile(one, []byte("- first\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForPath(t, sink, "one.md")

	two := filepath.Join(root, "two.md")
	if err := os.Rename(one, two); err != nil {
		t.Fatalf("rename: %v", err)
	}
	waitForPath(t, sink, "two.md")

	if err := os.Remove(two); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitForPath(t, sink, "two.md")

	temp := filepath.Join(root, ".replace.tmp")
	if err := os.WriteFile(temp, []byte("- replaced\n"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	target := filepath.Join(root, "three.md")
	if err := os.Rename(temp, target); err != nil {
		t.Fatalf("atomic replace: %v", err)
	}
	waitForPath(t, sink, "three.md")
}

// TestWatcherDebouncesBursts verifies a burst of writes inside the debounce
// window is merged into a bounded number of notifications.
func TestWatcherDebouncesBursts(t *testing.T) {
	root, sink, _ := newFixture(t)
	target := filepath.Join(root, "burst.md")

	for index := 0; index < 3; index++ {
		if err := os.WriteFile(target, []byte(strings.Repeat("- entry\n", index+1)), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	waitForPath(t, sink, "burst.md")
	time.Sleep(2 * Debounce)
	if count := sink.countContaining("burst.md"); count > 2 {
		t.Fatalf("a burst inside the debounce window should merge, got %d notifications", count)
	}
}

// TestWatcherExcludesGitInternal verifies Git internal writes never appear as
// vault invalidations.
func TestWatcherExcludesGitInternal(t *testing.T) {
	root, sink, _ := newFixture(t)
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	time.Sleep(2 * Debounce)
	if count := sink.countContaining(".git"); count != 0 {
		t.Fatalf(".git writes must be excluded, got %d notifications: %+v", count, sink.snapshot())
	}
}

// TestNewRejectsMissingRoot verifies one initialization error path.
func TestNewRejectsMissingRoot(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing"), &recordSink{}); err == nil {
		t.Fatal("New should reject a missing root")
	}
}
