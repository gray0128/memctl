package syncer

import (
	"os"

	"github.com/gray0128/memctl/internal/memerr"
)

// SpawnRequest describes one background synchronization worker.
type SpawnRequest struct {
	// RepoPath and StateDir are passed to the worker explicitly so it does not
	// have to resolve them from a login environment.
	RepoPath string
	StateDir string
	// Env is the parent environment; only the credential/network whitelist is
	// forwarded.
	Env []string
	// Executable overrides the worker binary. Tests set it; production leaves
	// it empty and re-executes the running memctl binary.
	Executable string
}

// Spawn starts a detached, non-interactive synchronization worker. It is
// best-effort: the caller does not wait for it and must not treat its outcome
// as part of the local transaction. A worker that finds the repository lock
// held exits immediately instead of queuing.
func Spawn(req SpawnRequest) error {
	executable := req.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot locate the memctl executable for background sync")
		}
	}
	env := SyncEnv(req.Env)
	env = append(env,
		"AGENT_MEMORY_DIR="+req.RepoPath,
		"AGENT_MEMORY_STATE="+req.StateDir,
		"MEMCTL_SYNC_BACKGROUND=1",
	)
	return spawnDetached(executable, []string{"sync", "--wait"}, env, req.RepoPath)
}
