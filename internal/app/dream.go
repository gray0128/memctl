package app

import (
	"context"
	"os"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/git"
)

// suspiciousMatcher adapts the doctor prompt-injection detector to the corpus
// and Dreaming screening callbacks, so the phrase list has one owner.
func suspiciousMatcher() func(string) bool {
	return func(text string) bool { return len(doctor.SuspiciousPhrases(text)) > 0 }
}

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
		a.dreamSvc.Version = a.version
		a.dreamSvc.CommitSource = a.commitSource
	}
	return a.dreamSvc
}

// SetDreamService replaces the Dreaming service. Tests use it to point the
// service at a controlled Git remote; production code leaves it unset.
func (a *App) SetDreamService(service *dream.Service) {
	a.dreamMu.Lock()
	a.dreamSvc = service
	if service != nil {
		if service.Version == "" {
			service.Version = a.version
		}
		service.CommitSource = a.commitSource
	}
	a.dreamMu.Unlock()
}

// SetCommitSource records the provenance of the commits memctl performs itself:
// repository initialization, the managed workflow install, an applied proposal
// and an audit revert. The CLI and the UI server set it from the host
// environment, so those commits appear in the audit view with a recorded source
// instead of an unattributed placeholder.
func (a *App) SetCommitSource(source git.Source) {
	a.dreamMu.Lock()
	defer a.dreamMu.Unlock()
	a.commitSource = source
	if a.dreamSvc != nil {
		a.dreamSvc.CommitSource = source
	}
}

// SetVersion records the running binary version. Callers that care about the
// Dreaming workflow pin (the CLI and the UI server) set it before the first
// Dreaming call, so readiness can compare the workflow with this binary.
func (a *App) SetVersion(version string) {
	a.version = version
	a.dreamMu.Lock()
	if a.dreamSvc != nil && a.dreamSvc.Version == "" {
		a.dreamSvc.Version = version
	}
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

// ApplyDream applies one reviewed proposal through the managed write
// transaction. See app.ApplyDream in dreamapply.go.

// DreamBackendStatus reports whether Dreaming can start on this repository:
// the memory repository needs a confirmed private remote and the managed
// GitHub Actions workflow. The UI disables generation before a request is made;
// generation itself re-checks every condition under the repository lock. See
// docs/11-github-actions-dreaming.md section 2.1.
func (a *App) DreamBackendStatus(ctx context.Context) (string, string) {
	return a.dreamService().BackendStatus(ctx)
}

// DreamBackendReady reports whether generation is available, for the UI probe.
func (a *App) DreamBackendReady(ctx context.Context) bool {
	service := a.dreamService()
	return service.BackendReady(ctx)
}

// InitDreamWorkflow writes and commits the managed Dreaming workflow. version is
// substituted as the release the workflow downloads.
func (a *App) InitDreamWorkflow(ctx context.Context, version string) (dream.InitWorkflowResult, error) {
	return a.dreamService().InitWorkflow(ctx, version)
}

// DreamWorker runs the GitHub Actions worker against the checked-out baseline.
func (a *App) DreamWorker(ctx context.Context, requestRef, baseHead string) (dream.WorkerRunResult, error) {
	return a.dreamService().Worker(ctx, requestRef, baseHead)
}

// LocalDreaming is the local current-agent interactive path of
// docs/12-session-corpus-dreaming.md section 2.3: prepare fixes a local baseline
// and returns the memory files, the static diagnosis and the unconsumed session
// digests; propose validates the structured candidates and persists a
// `local-agent` proposal. Neither entry point requires a remote or the managed
// Actions workflow, and neither writes the main repository.

// DreamPrepareLocal returns the bundle the current agent organizes from. The
// caller identity refreshes its own session digest first and the store sweep
// adds the recent sessions of the project.
func (a *App) DreamPrepareLocal(ctx context.Context, req dream.PrepareRequest) (dream.DreamPrepareBundle, error) {
	service := a.dreamService()
	service.SuspiciousMatcher = suspiciousMatcher()
	service.SessionWroteMemory = a.sessionWroteMemory
	if req.Cwd == "" {
		req.Cwd = a.cfg.RepoPath
		if dir, err := os.Getwd(); err == nil {
			req.Cwd = dir
		}
	}
	return service.PrepareLocal(ctx, req)
}

// DreamProposeLocal validates one structured proposal input and persists it.
func (a *App) DreamProposeLocal(ctx context.Context, input dream.DreamProposeInput) (dream.ProposeResult, error) {
	service := a.dreamService()
	service.SuspiciousMatcher = suspiciousMatcher()
	return service.ProposeLocal(ctx, input)
}

// DreamList returns the persisted proposals in createdAt order descending. The
// internal apply input and the corpus digests stay on disk.
func (a *App) DreamList(ctx context.Context, limit int) ([]dream.Proposal, error) {
	// A scan is read-only and needs no repository precondition beyond the
	// configured state root, so a repository with an unfinished transaction can
	// still list the proposals that were already generated.
	return a.dreamService().List(limit), nil
}
