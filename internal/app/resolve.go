package app

import (
	"context"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
	"github.com/gray0128/memctl/internal/vault"
)

// Remote state values used in resolve results.
const (
	resolveRemotePending  = "pending"
	resolveRemoteConflict = "conflict"
	resolveRemoteSynced   = "synced"
)

// ConflictSideView is one side of a conflict as shown to the caller.
type ConflictSideView struct {
	Exists  bool    `json:"exists"`
	BlobID  string  `json:"blobId,omitempty"`
	Mode    string  `json:"mode,omitempty"`
	Binary  bool    `json:"binary,omitempty"`
	Content *string `json:"content,omitempty"`
}

// ConflictFileView is one conflicted path as shown to the caller.
type ConflictFileView struct {
	Path     string           `json:"path"`
	Kind     string           `json:"kind"`
	Base     ConflictSideView `json:"base"`
	Onto     ConflictSideView `json:"onto"`
	Replayed ConflictSideView `json:"replayed"`
}

// ConflictView is the caller-facing conflict snapshot. Content is text only
// and is omitted for binary or missing sides.
type ConflictView struct {
	ConflictID    string             `json:"conflictId"`
	RepoID        string             `json:"repoId"`
	BaseRevision  string             `json:"baseRevision"`
	OriginalHead  string             `json:"originalHead"`
	ExpectedHead  string             `json:"expectedHead"`
	UpstreamHead  string             `json:"upstreamHead"`
	ReplayCommit  string             `json:"replayCommit"`
	IndexRevision string             `json:"indexRevision"`
	OperationID   string             `json:"operationId,omitempty"`
	Phase         string             `json:"phase"`
	Files         []ConflictFileView `json:"files"`
	DetectedAt    string             `json:"detectedAt"`
}

// ResolvedFile is one resolved path supplied by the caller.
type ResolvedFile struct {
	Path       string
	Action     ChangeAction
	RawContent string
}

// ResolveResult reports the state after one resolve step.
type ResolveResult struct {
	ConflictID  string
	Head        string
	RemoteState string
	Ahead       int
	Behind      int
	// Conflict is the next replay stage when the rebase stopped again.
	Conflict *ConflictView
}

// ResolveStatus returns the active conflict snapshot for inspection.
func (a *App) ResolveStatus(ctx context.Context) (ConflictView, error) {
	session, err := a.beginResolve(ctx)
	if err != nil {
		return ConflictView{}, err
	}
	defer session.Close()

	conflict, ok, err := session.store.LoadConflict()
	if err != nil {
		return ConflictView{}, err
	}
	if !ok {
		return ConflictView{}, memerr.New(memerr.NotFound, "no conflict is awaiting resolution")
	}
	return toConflictView(conflict), nil
}

// ResolveApply applies one stage's resolved files and continues the owned
// rebase. It returns CONFLICT_STALE when the captured stage no longer matches
// the repository, and a new ConflictView when the next replay also conflicts.
func (a *App) ResolveApply(ctx context.Context, interactive bool, conflictID, baseRevision string, resolved []ResolvedFile) (ResolveResult, error) {
	session, err := a.beginResolve(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	defer session.Close()

	conflict, err := a.requireConflict(ctx, session, conflictID)
	if err != nil {
		return ResolveResult{}, err
	}

	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return ResolveResult{}, err
	}
	if conflict.BaseRevision != revision || (baseRevision != "" && baseRevision != revision) {
		return a.staleConflict(ctx, session, conflict)
	}

	switch conflict.Phase {
	case syncer.PhaseCaptured:
		if err := a.restartRebase(ctx, session, conflict); err != nil {
			if contractErr, ok := memerr.As(err); ok && contractErr.Code == memerr.ConflictStale {
				return a.staleConflict(ctx, session, conflict)
			}
			return ResolveResult{}, err
		}
	case syncer.PhaseResolving:
		head, err := git.Head(ctx, session.repoPath)
		if err != nil {
			return ResolveResult{}, err
		}
		indexRevision, err := syncer.IndexRevision(ctx, session.repoPath)
		if err != nil {
			return ResolveResult{}, err
		}
		if head != conflict.ExpectedHead || indexRevision != conflict.IndexRevision {
			return a.staleConflict(ctx, session, conflict)
		}
	default:
		return ResolveResult{}, memerr.New(memerr.Unsupported, "unknown conflict phase %q", conflict.Phase)
	}

	if err := a.applyResolved(ctx, session, conflict, resolved); err != nil {
		return ResolveResult{}, err
	}

	repoState, err := git.Inspect(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, err
	}
	if repoState.Unmerged {
		unmerged, err := git.UnmergedPaths(ctx, session.repoPath)
		if err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{}, memerr.New(memerr.InvalidArgument,
			"resolvedFiles did not cover every conflicted path: %v", unmerged)
	}

	if err := git.RebaseContinue(ctx, session.repoPath); err != nil {
		return a.handleNextStage(ctx, session, conflict, err)
	}
	return a.finishResolve(ctx, session, interactive)
}

// ResolveAbort aborts the owned rebase and restores the original HEAD. The
// original conflict remains for a later sync and the result is never synced.
func (a *App) ResolveAbort(ctx context.Context, conflictID string) (ResolveResult, error) {
	session, err := a.beginResolve(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	defer session.Close()

	conflict, err := a.requireConflict(ctx, session, conflictID)
	if err != nil {
		return ResolveResult{}, err
	}

	repoState, err := git.Inspect(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, err
	}
	if repoState.Operation == "rebase" {
		if err := git.RebaseAbort(ctx, session.repoPath); err != nil {
			return ResolveResult{}, memerr.Wrap(memerr.RecoveryRequired, err,
				"cannot abort the owned rebase; inspect the repository manually")
		}
	}
	head, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, err
	}
	if conflict.OriginalHead != "" && head != conflict.OriginalHead {
		return ResolveResult{}, memerr.New(memerr.RecoveryRequired,
			"the abort did not restore HEAD %s; inspect the repository manually", conflict.OriginalHead)
	}
	if dirty, err := git.StatusPorcelain(ctx, session.repoPath); err != nil {
		return ResolveResult{}, err
	} else if dirty != "" {
		return ResolveResult{}, memerr.New(memerr.RecoveryRequired,
			"the abort left worktree changes; inspect the repository manually")
	}

	if err := session.store.DeleteConflict(); err != nil {
		return ResolveResult{}, err
	}
	if err := session.store.DeleteOperation(); err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{Head: head, RemoteState: resolveRemotePending}, nil
}

// restartRebase replays the local commits onto the captured upstream and
// verifies that the stopped stage matches the snapshot's replay commit and
// index revision.
func (a *App) restartRebase(ctx context.Context, session *writeSession, conflict state.Conflict) error {
	operation, ok, err := session.store.LoadOperation()
	if err != nil {
		return err
	}
	if !ok {
		return memerr.New(memerr.RecoveryRequired, "the conflict %s has no operation ownership record", conflict.ConflictID)
	}
	operation.Phase = "running"
	if err := session.store.SaveOperation(operation); err != nil {
		return err
	}

	if err := git.Rebase(ctx, session.repoPath, conflict.UpstreamHead); err != nil {
		unmerged, listErr := git.UnmergedPaths(ctx, session.repoPath)
		if listErr != nil {
			return listErr
		}
		if len(unmerged) == 0 {
			_ = git.RebaseAbort(ctx, session.repoPath)
			return memerr.New(memerr.ConflictStale, "the captured conflict no longer stops the replay")
		}
		reproduction, err := syncer.CaptureStopped(ctx, session.repoPath, conflict.RepoID, conflict.OperationID, conflict.UpstreamHead, syncer.PhaseCaptured)
		if err != nil {
			return err
		}
		if reproduction.ReplayCommit != conflict.ReplayCommit || reproduction.IndexRevision != conflict.IndexRevision {
			_ = git.RebaseAbort(ctx, session.repoPath)
			return memerr.New(memerr.ConflictStale,
				"the conflict stage changed since it was captured; run `memctl sync --wait` again")
		}
		return nil
	}
	return memerr.New(memerr.ConflictStale, "the captured conflict no longer stops the replay")
}

// applyResolved writes or deletes exactly the paths of the current stage and
// stages them. Unsupported conflict types are refused.
func (a *App) applyResolved(ctx context.Context, session *writeSession, conflict state.Conflict, resolved []ResolvedFile) error {
	expected := map[string]string{}
	for _, file := range conflict.Files {
		if file.Kind == syncer.KindBinary || file.Kind == syncer.KindUnsupported {
			return memerr.New(memerr.Unsupported,
				"path %q has an unsupported conflict type (%s); resolve it manually outside memctl", file.Path, file.Kind)
		}
		expected[file.Path] = file.Kind
	}

	seen := map[string]bool{}
	paths := make([]string, 0, len(resolved))
	for _, file := range resolved {
		clean, err := vault.ValidateRelPath(file.Path)
		if err != nil {
			return err
		}
		if _, ok := expected[clean]; !ok {
			return memerr.New(memerr.InvalidArgument,
				"resolved path %q is not part of conflict %s", clean, conflict.ConflictID)
		}
		if seen[clean] {
			return memerr.New(memerr.InvalidArgument, "resolved path %q appears more than once", clean)
		}
		seen[clean] = true

		switch file.Action {
		case ActionDelete:
			if err := session.root.RemoveFile(clean); err != nil {
				return err
			}
		case ActionPut:
			if err := session.root.WriteAtomic(clean, []byte(file.RawContent), 0o644); err != nil {
				return err
			}
		default:
			return memerr.New(memerr.InvalidArgument,
				"unknown action %q for resolved path %q; expected put or delete", file.Action, clean)
		}
		paths = append(paths, clean)
	}
	if len(seen) != len(expected) {
		return memerr.New(memerr.InvalidArgument,
			"resolvedFiles must cover every conflicted path of conflict %s (%d given, %d required)",
			conflict.ConflictID, len(seen), len(expected))
	}
	return git.Add(ctx, session.repoPath, paths...)
}

// handleNextStage captures a further conflict after `rebase --continue`.
func (a *App) handleNextStage(ctx context.Context, session *writeSession, previous state.Conflict, cause error) (ResolveResult, error) {
	unmerged, err := git.UnmergedPaths(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, err
	}
	if len(unmerged) == 0 {
		return ResolveResult{}, cause
	}
	next, err := syncer.CaptureStopped(ctx, session.repoPath, previous.RepoID, previous.OperationID, previous.UpstreamHead, syncer.PhaseResolving)
	if err != nil {
		return ResolveResult{}, err
	}
	head, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, err
	}
	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return ResolveResult{}, err
	}
	next.OriginalHead = previous.OriginalHead
	next.ExpectedHead = head
	next.BaseRevision = revision
	if err := session.store.SaveConflict(*next); err != nil {
		return ResolveResult{}, err
	}
	if operation, ok, err := session.store.LoadOperation(); err != nil {
		return ResolveResult{}, err
	} else if ok {
		operation.Phase = syncer.PhaseResolving
		operation.ConflictID = next.ConflictID
		if err := session.store.SaveOperation(operation); err != nil {
			return ResolveResult{}, err
		}
	}
	view := toConflictView(*next)
	return ResolveResult{
		ConflictID:  next.ConflictID,
		Head:        head,
		RemoteState: resolveRemoteConflict,
		Conflict:    &view,
	}, nil
}

// finishResolve clears the conflict state and attempts the push that follows a
// completed replay. A push failure keeps the local history and reports pending.
func (a *App) finishResolve(ctx context.Context, session *writeSession, interactive bool) (ResolveResult, error) {
	if err := session.store.DeleteConflict(); err != nil {
		return ResolveResult{}, err
	}
	if err := session.store.DeleteOperation(); err != nil {
		return ResolveResult{}, err
	}
	head, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, err
	}
	result := ResolveResult{Head: head, RemoteState: resolveRemotePending}

	remote, hasRemote, err := git.DefaultRemote(ctx, session.repoPath)
	if err != nil || !hasRemote {
		return result, err
	}
	if err := requireRemoteApproval(ctx, session, remote); err != nil {
		return result, nil
	}
	branch, err := git.HeadBranch(ctx, session.repoPath)
	if err != nil {
		return result, nil
	}
	if err := git.Push(ctx, session.repoPath, git.Options{Interactive: interactive, NonInteractive: !interactive}, remote, branch, false); err != nil {
		return result, nil
	}
	result.RemoteState = resolveRemoteSynced
	return result, nil
}

func (a *App) requireConflict(ctx context.Context, session *writeSession, conflictID string) (state.Conflict, error) {
	conflict, ok, err := session.store.LoadConflict()
	if err != nil {
		return state.Conflict{}, err
	}
	if !ok {
		return state.Conflict{}, memerr.New(memerr.NotFound, "no conflict is awaiting resolution")
	}
	if conflictID != "" && conflict.ConflictID != conflictID {
		return state.Conflict{}, memerr.New(memerr.NotFound,
			"conflict %s does not exist; the active conflict is %s", conflictID, conflict.ConflictID)
	}
	return conflict, nil
}

// staleConflict clears the stale state and returns CONFLICT_STALE without
// touching the new history.
func (a *App) staleConflict(ctx context.Context, session *writeSession, conflict state.Conflict) (ResolveResult, error) {
	if repoState, err := git.Inspect(ctx, session.repoPath); err == nil && repoState.Operation == "rebase" {
		_ = git.RebaseAbort(ctx, session.repoPath)
	}
	_ = session.store.DeleteConflict()
	_ = session.store.DeleteOperation()
	head, _ := git.Head(ctx, session.repoPath)
	return ResolveResult{Head: head, RemoteState: resolveRemotePending},
		memerr.New(memerr.ConflictStale,
			"conflict %s no longer matches the repository; run `memctl sync --wait` again", conflict.ConflictID)
}

func (a *App) beginResolve(ctx context.Context) (*writeSession, error) {
	session, err := a.beginLocked(ctx, true, true)
	if err != nil {
		return nil, err
	}
	if _, ok, err := session.store.LoadConflict(); err != nil {
		session.Close()
		return nil, err
	} else if !ok {
		session.Close()
		return nil, memerr.New(memerr.NotFound, "no conflict is awaiting resolution")
	}
	return session, nil
}

func toConflictView(conflict state.Conflict) ConflictView {
	view := ConflictView{
		ConflictID:    conflict.ConflictID,
		RepoID:        conflict.RepoID,
		BaseRevision:  conflict.BaseRevision,
		OriginalHead:  conflict.OriginalHead,
		ExpectedHead:  conflict.ExpectedHead,
		UpstreamHead:  conflict.UpstreamHead,
		ReplayCommit:  conflict.ReplayCommit,
		IndexRevision: conflict.IndexRevision,
		OperationID:   conflict.OperationID,
		Phase:         conflict.Phase,
		DetectedAt:    conflict.DetectedAt,
	}
	for _, file := range conflict.Files {
		view.Files = append(view.Files, ConflictFileView{
			Path:     file.Path,
			Kind:     file.Kind,
			Base:     toSideView(file.Base),
			Onto:     toSideView(file.Onto),
			Replayed: toSideView(file.Replayed),
		})
	}
	return view
}

func toSideView(side state.ConflictSide) ConflictSideView {
	view := ConflictSideView{Exists: side.Exists, BlobID: side.BlobID, Mode: side.Mode, Binary: side.Binary}
	if side.Exists && !side.Binary {
		content := string(syncer.DecodeSide(side))
		view.Content = &content
	}
	return view
}
