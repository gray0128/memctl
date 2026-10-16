// Package lock provides the per-repository exclusive lock. The lock file
// lives in the repository's real Git directory and is never deleted or
// recreated while held, so a released lock cannot be mistaken for a free one
// by a process that acquired an old inode. See
// docs/01-architecture-and-modules.md section 2.1.
package lock

import (
	"context"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

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
	flock *flock.Flock
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
	fl := flock.New(filepath.Join(gitDir, FileName))
	locked, err := fl.TryLockContext(ctx, retryDelay)
	if err != nil {
		return nil, memerr.Wrap(memerr.Busy, err, "cannot acquire repository lock %s", fl.Path())
	}
	if !locked {
		return nil, memerr.New(memerr.Busy,
			"repository lock %s is held by another process; retry after it finishes", fl.Path())
	}
	return &Lock{flock: fl}, nil
}

// Release drops the lock. It does not delete the lock file.
func (l *Lock) Release() error {
	if l == nil || l.flock == nil {
		return nil
	}
	return l.flock.Unlock()
}

// Path returns the lock file path for diagnostics.
func (l *Lock) Path() string { return l.flock.Path() }
