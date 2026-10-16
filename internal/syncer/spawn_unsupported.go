//go:build !unix && !windows

package syncer

// spawnDetached is a no-op on platforms without a detached-process primitive.
// Background derivation is disabled there and only foreground sync runs.
func spawnDetached(executable string, args, env []string, dir string) error {
	return nil
}
