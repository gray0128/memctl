//go:build !unix

package dream

import "os/exec"

// setProcessGroup is a no-op where process groups are unavailable.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup terminates the direct child.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
