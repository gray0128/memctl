//go:build !unix

package cli

import (
	"errors"
	"os/exec"
)

// wrappedExitCode maps a child process result to the wrapper exit code. ok is
// false when the program could not be started at all.
func wrappedExitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}
