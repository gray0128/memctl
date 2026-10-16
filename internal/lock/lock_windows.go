//go:build windows

package lock

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryFlock takes a non-blocking exclusive lock on the file handle.
func tryFlock(file *os.File) (bool, error) {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	switch err {
	case nil:
		return true, nil
	case windows.ERROR_LOCK_VIOLATION:
		return false, nil
	default:
		return false, err
	}
}

func unlockFile(file *os.File) error {
	overlapped := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
}
