package app

import (
	"context"
	"os"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
)

// defaultSyncBudget is the total deadline of one foreground remote task, per
// docs/04-ui-and-cli-specification.md section 3.1.
const defaultSyncBudget = 30 * time.Second

// SyncResult is the observable outcome of one foreground synchronization.
type SyncResult struct {
	RepoID        string
	Branch        string
	Head          string
	HasRemote     bool
	HasUpstream   bool
	Ahead         int
	Behind        int
	RemoteState   string
	LocalState    string
	ReasonCode    string
	LastAttemptAt string
	LastSuccessAt string
	ConflictID    string
	OperationID   string
}

// SyncOptions controls one synchronization attempt.
type SyncOptions struct {
	// Interactive allows Git and SSH native prompts when the caller has a
	// terminal. Hook and background callers leave it false.
	Interactive bool
	// Background takes the repository lock without waiting so a burst of hook
	// invocations exits immediately instead of queuing.
	Background bool
}

// SyncWait performs one foreground synchronization under the repository lock.
// It returns a Result for both success and classified failure. A content
// conflict is persisted and reported as CONTENT_CONFLICT.
func (a *App) SyncWait(ctx context.Context, interactive bool) (SyncResult, error) {
	return a.SyncWithOptions(ctx, SyncOptions{Interactive: interactive})
}

// SyncWithOptions performs one synchronization attempt with explicit options.
func (a *App) SyncWithOptions(ctx context.Context, opts SyncOptions) (SyncResult, error) {
	session, err := a.beginSync(ctx, opts.Background)
	if err != nil {
		return SyncResult{}, err
	}
	defer session.Close()

	syncCtx := session.ctx
	remote, hasRemote, err := git.DefaultRemote(syncCtx, session.repoPath)
	if err != nil {
		return SyncResult{}, err
	}
	if hasRemote {
		if err := requireRemoteApproval(syncCtx, session, remote); err != nil {
			return SyncResult{}, err
		}
	}

	if _, ok := syncCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		syncCtx, cancel = context.WithTimeout(syncCtx, defaultSyncBudget)
		defer cancel()
	}

	run, runErr := syncer.Run(syncCtx, syncer.Request{
		RepoPath:       session.repoPath,
		RepoID:         config.RepoID(session.repoPath),
		Remote:         remote,
		Interactive:    opts.Interactive,
		NonInteractive: !opts.Interactive,
	})

	summary := SyncResult{
		RepoID:      config.RepoID(session.repoPath),
		Branch:      run.Branch,
		Head:        run.Head,
		HasRemote:   run.HasRemote,
		HasUpstream: run.HasUpstream,
		Ahead:       run.Ahead,
		Behind:      run.Behind,
		RemoteState: run.RemoteState,
		ReasonCode:  run.ReasonCode,
		LocalState:  "clean",
	}

	if run.Conflict != nil {
		revision, err := a.repoRevision(syncCtx, session)
		if err != nil {
			return summary, err
		}
		run.Conflict.RepoID = summary.RepoID
		run.Conflict.BaseRevision = revision
		if err := session.store.SaveConflict(*run.Conflict); err != nil {
			return summary, err
		}
		summary.RemoteState = syncer.StateConflict
		summary.ReasonCode = string(memerr.ContentConflict)
		summary.ConflictID = run.Conflict.ConflictID
		summary.OperationID = run.Conflict.OperationID
		a.persistSyncSummary(session, summary, false)
		return summary, memerr.New(memerr.ContentConflict,
			"sync stopped on conflict %s; run `memctl resolve` to inspect and resolve it", run.Conflict.ConflictID)
	}

	a.persistSyncSummary(session, summary, runErr == nil && run.RemoteState == syncer.StateSynced)
	if runErr != nil {
		return summary, runErr
	}
	return summary, nil
}

// triggerBackgroundSync best-effort starts one detached synchronization worker
// after a managed write. It never blocks or fails the write.
func (a *App) triggerBackgroundSync() {
	if a == nil || !a.backgroundSync {
		return
	}
	_ = syncer.Spawn(syncer.SpawnRequest{
		RepoPath: a.cfg.RepoPath,
		StateDir: a.cfg.StateDir,
		Env:      os.Environ(),
	})
}

// BeginBackgroundSync starts the detached worker for the automatic Hook path.
// It is best-effort; the caller reports success regardless.
func (a *App) BeginBackgroundSync() error {
	return syncer.Spawn(syncer.SpawnRequest{
		RepoPath: a.cfg.RepoPath,
		StateDir: a.cfg.StateDir,
		Env:      os.Environ(),
	})
}

// beginSync acquires the lock and validates the conditions shared by remote
// synchronization: no unfinished transaction, no owned conflict, no active Git
// operation and a clean worktree and index.
func (a *App) beginSync(ctx context.Context, background bool) (*writeSession, error) {
	session, err := a.beginLocked(ctx, true, false, background)
	if err != nil {
		return nil, err
	}
	if conflict, ok, err := session.store.LoadConflict(); err != nil {
		session.Close()
		return nil, err
	} else if ok {
		session.Close()
		return nil, memerr.New(memerr.OperationActive,
			"conflict %s is awaiting resolution; run `memctl resolve`", conflict.ConflictID)
	}
	pending, err := session.store.Pending()
	if err != nil {
		session.Close()
		return nil, err
	}
	if len(pending) > 0 {
		session.Close()
		return nil, memerr.New(memerr.RecoveryRequired,
			"transaction %s is unfinished; run `memctl recover --status`", pending[0].ID)
	}
	status, err := git.StatusPorcelain(ctx, session.repoPath)
	if err != nil {
		session.Close()
		return nil, err
	}
	if status != "" {
		session.Close()
		return nil, memerr.New(memerr.ExternalChanges,
			"the repository has staged, unstaged or untracked changes; commit them with `memctl sync --capture` before synchronizing")
	}
	return session, nil
}

// requireRemoteApproval refuses to reach a remote whose fetch/push target set
// was not confirmed as private and owned by the user.
func requireRemoteApproval(ctx context.Context, session *writeSession, remote string) error {
	urls, err := git.RemoteURLs(ctx, session.repoPath, remote)
	if err != nil {
		return err
	}
	approval, ok, err := session.store.LoadApproval()
	if err != nil {
		return err
	}
	if !ok || !sameStringSet(approval.URLs, urls) {
		return memerr.New(memerr.RemoteNotApproved,
			"the remote %s is not confirmed as private and owned by you; run `memctl init <url> --confirm-private`", remote)
	}
	return nil
}

// persistSyncSummary stores the last attempt and, on success, the last success
// time under the repository cache.
func (a *App) persistSyncSummary(session *writeSession, summary SyncResult, success bool) {
	cache := state.OpenCache(a.cfg.StateDir, summary.RepoID)
	previous, ok, err := cache.LoadSync()
	if err != nil {
		return
	}
	record := state.SyncSummary{
		RemoteState: summary.RemoteState,
		ReasonCode:  summary.ReasonCode,
		Branch:      summary.Branch,
		Head:        summary.Head,
		HasRemote:   summary.HasRemote,
		HasUpstream: summary.HasUpstream,
		Ahead:       summary.Ahead,
		Behind:      summary.Behind,
		ConflictID:  summary.ConflictID,
		OperationID: summary.OperationID,
	}
	record.LastAttemptAt = state.Now()
	if success {
		record.LastSuccessAt = record.LastAttemptAt
	} else if ok {
		record.LastSuccessAt = previous.LastSuccessAt
	}
	_ = cache.SaveSync(record)
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			return false
		}
	}
	return true
}
