//go:build !unix

package git

import "os/exec"

// configureProcessGroup is a no-op where process groups are unavailable; the
// child is still tracked and killed by PID.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup terminates the direct child. Platforms without process
// groups rely on Git to clean up its own transport children.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
