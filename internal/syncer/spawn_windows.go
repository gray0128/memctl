//go:build windows

package syncer

import (
	"os/exec"

	"golang.org/x/sys/windows"

	"github.com/gray0128/memctl/internal/memerr"
)

// spawnDetached starts a worker in its own detached process group. The worker
// is not part of the caller's console or job, so it survives the hook caller.
func spawnDetached(executable string, args, env []string, dir string) error {
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot start the background sync worker")
	}
	return cmd.Process.Release()
}
