//go:build !unix

package corpus

import "os/exec"

// configureDeltaProcessGroup is a no-op where process groups are unavailable.
func configureDeltaProcessGroup(cmd *exec.Cmd) {}

// killDeltaProcessGroup terminates the direct child. Platforms without process
// groups rely on the CLI to clean up its own helpers.
func killDeltaProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
