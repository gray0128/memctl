//go:build unix

package corpus

import (
	"os/exec"
	"syscall"
)

// configureDeltaProcessGroup puts the Delta child in its own process group, so
// the group can be signalled as a unit. A host CLI that starts helper processes
// inherits the stdout pipe; killing only the direct child would leave the pipe
// open and the collector blocked until those helpers exit.
func configureDeltaProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killDeltaProcessGroup signals the whole group, reaching any helper process the
// CLI started.
func killDeltaProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
