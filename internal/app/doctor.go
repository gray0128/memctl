package app

import (
	"context"

	"github.com/gray0128/memctl/internal/doctor"
)

// Doctor runs the read-only repository diagnosis. It validates that the
// configured path is a Git working-tree root without requiring the entry file,
// so a missing entry point is reported as a diagnostic instead of a hard
// failure. It takes no lock and never writes to the repository, the Git index
// or HEAD.
func (a *App) Doctor(ctx context.Context) (doctor.Report, error) {
	repoPath, err := a.requireRepoRoot(ctx)
	if err != nil {
		return doctor.Report{}, err
	}
	return doctor.Check(ctx, repoPath)
}
