//go:build unix

package lock

import (
	"os"

	"golang.org/x/sys/unix"
)

// tryFlock takes a non-blocking exclusive flock on the open file description.
// The lock is released by closing the descriptor; see Lock.Release.
func tryFlock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case err == unix.EWOULDBLOCK:
		return false, nil
	default:
		return false, err
	}
}
