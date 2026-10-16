//go:build unix

package syncer

import (
	"os/exec"
	"syscall"

	"github.com/gray0128/memctl/internal/memerr"
)

// spawnDetached starts a worker in its own session and does not wait for it.
// The worker is reparented when the caller exits and is reaped by init.
func spawnDetached(executable string, args, env []string, dir string) error {
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot start the background sync worker")
	}
	return cmd.Process.Release()
}
