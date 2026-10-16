// Package watcher turns file-system events inside one repository worktree
// into bounded index-invalidation notifications. It never parses memory
// content and never owns a persistent message queue: a notification only tells
// the HTTP client to re-read the snapshot. See
// docs/02-domain-models-and-contracts.md section 5 and
// docs/01-architecture-and-modules.md section 4.
package watcher

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Notification reasons defined by docs/02-domain-models-and-contracts.md
// section 5. Other event names (sync.status, sync.conflict, dream.updated)
// belong to later stages and are not produced here.
const (
	ReasonVaultUpdated   = "vault.updated"
	ReasonResyncRequired = "resync.required"
)

// Debounce is the initial window that merges a burst of file-system events into
// one notification. It is a notification-combining parameter, not a signal
// that a transaction finished.
const Debounce = 150 * time.Millisecond

// Watcher recursively watches the existing and newly created directories of
// one repository worktree. It does not follow symbolic links and excludes the
// Git internal directory.
type Watcher struct {
	root    string
	sink    Sink
	fsw     *fsnotify.Watcher
	watched map[string]struct{}
}

// New builds a Watcher for root and registers watches for every current
// directory. The sink must not block.
func New(root string, sink Sink) (*Watcher, error) {
	if sink == nil {
		return nil, errors.New("watcher requires a non-nil sink")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("watcher root is not a directory: " + abs)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watcher := &Watcher{root: abs, sink: sink, fsw: fsw, watched: map[string]struct{}{}}
	if err := watcher.addTree(abs); err != nil {
		fsw.Close()
		return nil, err
	}
	return watcher, nil
}

// Close stops watching. It does not publish any further notification.
func (w *Watcher) Close() error {
	if w == nil || w.fsw == nil {
		return nil
	}
	return w.fsw.Close()
}

// Watch blocks until ctx is cancelled or the underlying watcher stops. It
// merges event bursts with Debounce, publishes one vault.updated per burst and
// re-scans the tree so a rename, remove or atomic replace cannot leave a
// directory unwatched. A backend error publishes resync.required because the
// hint stream is no longer complete.
func (w *Watcher) Watch(ctx context.Context) error {
	pending := map[string]struct{}{}
	timer := time.NewTimer(time.Hour)
	stopTimer(timer)
	defer timer.Stop()

	flush := func() {
		if len(pending) == 0 {
			return
		}
		files := make([]string, 0, len(pending))
		for path := range pending {
			files = append(files, path)
		}
		sort.Strings(files)
		pending = map[string]struct{}{}
		w.sink.Publish(Invalidation{Reason: ReasonVaultUpdated, ChangedFiles: files})
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			if w.ignored(event.Name) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				w.addIfDir(event.Name)
			}
			rel, ok := w.rel(event.Name)
			if !ok {
				continue
			}
			pending[rel] = struct{}{}
			resetTimer(timer, Debounce)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			if err != nil {
				w.sink.Publish(Invalidation{Reason: ReasonResyncRequired})
			}
		case <-timer.C:
			flush()
			w.refresh()
		}
	}
}

// addTree registers a watch for every directory below root. It is idempotent.
func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A directory removed while walking is not an error; a later
			// event or refresh reconciles the watch list.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if w.ignored(path) {
			return filepath.SkipDir
		}
		if _, ok := w.watched[path]; ok {
			return nil
		}
		if err := w.fsw.Add(path); err != nil {
			return err
		}
		w.watched[path] = struct{}{}
		return nil
	})
}

// addIfDir adds a newly created directory and its existing children.
func (w *Watcher) addIfDir(path string) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return
	}
	if w.ignored(path) {
		return
	}
	_ = w.addTree(path)
}

// refresh reconciles the watch list with the current tree. It covers renames
// and atomic replacements whose Create event was not delivered.
func (w *Watcher) refresh() {
	found := map[string]struct{}{}
	_ = filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if w.ignored(path) {
			return filepath.SkipDir
		}
		found[path] = struct{}{}
		return nil
	})
	for path := range found {
		if _, ok := w.watched[path]; ok {
			continue
		}
		if err := w.fsw.Add(path); err == nil {
			w.watched[path] = struct{}{}
		}
	}
	for path := range w.watched {
		if _, ok := found[path]; ok {
			continue
		}
		_ = w.fsw.Remove(path)
		delete(w.watched, path)
	}
}

// ignored excludes the Git internal directory. It compares the first path
// segment case-insensitively so a `.GIT` alias on a case-insensitive file
// system is also excluded.
func (w *Watcher) ignored(path string) bool {
	rel, ok := w.rel(path)
	if !ok {
		return true
	}
	first, _, _ := strings.Cut(rel, "/")
	return strings.EqualFold(first, ".git")
}

// rel converts an absolute event path to a slash-separated root-relative path.
func (w *Watcher) rel(path string) (string, bool) {
	relative, err := filepath.Rel(w.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func resetTimer(timer *time.Timer, after time.Duration) {
	stopTimer(timer)
	timer.Reset(after)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
