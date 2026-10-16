//go:build unix

package cli

import (
	"errors"
	"os/exec"
	"syscall"
)

// wrappedExitCode maps a child process result to the wrapper exit code. A
// signal-terminated child keeps its signal semantics as 128+signal; ok is
// false when the program could not be started at all.
func wrappedExitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return 128 + int(status.Signal()), true
		}
		return status.ExitStatus(), true
	}
	return exitErr.ExitCode(), true
}
