// Package lock provides the per-repository exclusive lock. The lock file
// lives in the repository's real Git directory and is never deleted or
// recreated while held, so a released lock cannot be mistaken for a free one
// by a process that acquired an old inode.
//
// The lock is held on an open file description. The holder can pass that file
// to its Git children so the lock cannot outlive the Git process: a parent that
// is killed while Git is still running leaves the lock held until Git exits.
// See docs/01-architecture-and-modules.md section 2.1 and section 6.
package lock

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// FileName is the fixed lock file name inside the repository's Git directory.
const FileName = "memctl.lock"

// retryDelay is how often a waiting caller retries the non-blocking lock.
const retryDelay = 25 * time.Millisecond

// defaultWait bounds how long a caller without its own deadline waits for the
// repository lock before reporting BUSY.
const defaultWait = 3 * time.Second

// Lock is a held repository lock. Release must be called exactly once.
type Lock struct {
	path string
	file *os.File
}

// Acquire takes the exclusive repository lock. gitDir must be the repository's
// absolute Git directory. The call waits within the context budget; when the
// lock is still held elsewhere it returns BUSY instead of blocking forever.
// The same process must not acquire the lock recursively.
func Acquire(ctx context.Context, gitDir string) (*Lock, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultWait)
		defer cancel()
	}
	path := filepath.Join(gitDir, FileName)
	for {
		held, ok, err := tryOpen(path)
		if err != nil {
			return nil, err
		}
		if ok {
			return held, nil
		}
		select {
		case <-ctx.Done():
			return nil, memerr.New(memerr.Busy,
				"repository lock %s is held by another process; retry after it finishes", path)
		case <-time.After(retryDelay):
		}
	}
}

// TryAcquire takes the lock only if it is free right now. It returns ok=false
// when another process holds it. Background workers use it so a burst of hook
// invocations exits immediately instead of queuing behind a running sync.
func TryAcquire(gitDir string) (*Lock, bool, error) {
	return tryOpen(filepath.Join(gitDir, FileName))
}

func tryOpen(path string) (*Lock, bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, memerr.Wrap(memerr.IOFailed, err, "cannot open repository lock %s", path)
	}
	acquired, err := tryFlock(file)
	if err != nil {
		file.Close()
		return nil, false, memerr.Wrap(memerr.IOFailed, err, "cannot acquire repository lock %s", path)
	}
	if !acquired {
		file.Close()
		return nil, false, nil
	}
	return &Lock{path: path, file: file}, true, nil
}

// File returns the locked file for inheritance by Git children. It is nil only
// after Release.
func (l *Lock) File() *os.File {
	if l == nil {
		return nil
	}
	return l.file
}

// Path returns the lock file path for diagnostics.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Release drops the lock and closes the descriptor. Children that inherited the
// descriptor keep the lock until they exit.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return memerr.Wrap(memerr.IOFailed, unlockErr, "cannot release repository lock %s", l.path)
	}
	if closeErr != nil {
		return memerr.Wrap(memerr.IOFailed, closeErr, "cannot close repository lock %s", l.path)
	}
	return nil
}
