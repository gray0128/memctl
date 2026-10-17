package app

import (
	"context"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/dream"
)

// Dreaming is the explicit, user-triggered memory-organization workflow of
// docs/03-workflows-and-dataflows.md section 6. The application service only
// forwards to internal/dream; the proposal is persisted by that package and
// never touches the main repository until stage D-2 applies it explicitly.

// dreamService lazily builds and then reuses the Dreaming service. One instance
// owns the in-process generation records, so an HTTP request that starts a
// generation and a later GET that polls it share the same instance.
func (a *App) dreamService() *dream.Service {
	a.dreamMu.Lock()
	defer a.dreamMu.Unlock()
	if a.dreamSvc == nil {
		a.dreamSvc = dream.NewService(dream.Source{
			RepoPath: config.Canonical(a.cfg.RepoPath),
			RepoID:   config.RepoID(a.cfg.RepoPath),
			StateDir: a.cfg.StateDir,
		})
	}
	return a.dreamSvc
}

// SetDreamService replaces the Dreaming service. Tests use it to inject a
// synthetic runner and a controlled sandbox; production code leaves it unset.
func (a *App) SetDreamService(service *dream.Service) {
	a.dreamMu.Lock()
	a.dreamSvc = service
	a.dreamMu.Unlock()
}

// DreamPreview runs one complete generation and returns the finished proposal.
func (a *App) DreamPreview(ctx context.Context, runner, baseRevision string) (dream.Proposal, error) {
	return a.dreamService().Preview(ctx, dream.PreviewRequest{Runner: runner, BaseRevision: baseRevision})
}

// DreamBegin fixes the baseline, exports the supported files and persists a
// generating proposal. The caller runs DreamRun or polls DreamProposal.
func (a *App) DreamBegin(ctx context.Context, runner, baseRevision string) (dream.Proposal, error) {
	return a.dreamService().Begin(ctx, dream.PreviewRequest{Runner: runner, BaseRevision: baseRevision})
}

// DreamRun executes a previously started generation and persists its terminal
// record.
func (a *App) DreamRun(ctx context.Context, proposalID string) (dream.Proposal, error) {
	return a.dreamService().Run(ctx, proposalID)
}

// DreamProposal returns one stored proposal.
func (a *App) DreamProposal(ctx context.Context, proposalID string) (dream.Proposal, error) {
	return a.dreamService().Load(ctx, proposalID)
}
