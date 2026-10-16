//go:build darwin || linux

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

// hasTerminal reports whether both standard input and standard error are
// terminals. Credential prompts are only allowed on that path; /dev/null is
// not a terminal because the termios ioctl fails on it.
func hasTerminal() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stderr)
}

func isTerminal(file *os.File) bool {
	_, err := unix.IoctlGetTermios(int(file.Fd()), ioctlReadTermios)
	return err == nil
}
