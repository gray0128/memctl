//go:build unix

package git

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts each Git child in its own process group so that
// SSH, credential helpers and askpass children can be terminated together.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the whole group. On Unix a group signal
// reaches grandchildren such as ssh and credential helpers.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
