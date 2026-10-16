package app

import (
	"context"
	"io/fs"
	"unicode/utf8"

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
	ctx = session.ctx

	conflict, err := a.requireConflict(session, conflictID)
	if err != nil {
		return ResolveResult{}, err
	}
	if baseRevision == "" {
		return ResolveResult{}, memerr.New(memerr.InvalidArgument,
			"resolving conflict %s requires the baseRevision of the captured stage", conflict.ConflictID)
	}

	// Validate the whole request before any repository state changes: an
	// unsupported conflict type or an invalid resolvedFiles list must never
	// restart the rebase and leave a half-resolved stage behind.
	plans, err := planResolved(conflict, resolved)
	if err != nil {
		return ResolveResult{}, err
	}

	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return ResolveResult{}, err
	}
	if conflict.BaseRevision != revision || baseRevision != revision {
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

	if err := a.applyResolved(ctx, session, plans); err != nil {
		return ResolveResult{}, a.restoreFailedResolution(ctx, session, conflict, err)
	}

	repoState, err := git.Inspect(ctx, session.repoPath)
	if err != nil {
		return ResolveResult{}, a.restoreFailedResolution(ctx, session, conflict, err)
	}
	if repoState.Unmerged {
		unmerged, err := git.UnmergedPaths(ctx, session.repoPath)
		if err != nil {
			return ResolveResult{}, a.restoreFailedResolution(ctx, session, conflict, err)
		}
		return ResolveResult{}, a.restoreFailedResolution(ctx, session, conflict, memerr.New(memerr.InvalidArgument,
			"resolvedFiles did not cover every conflicted path: %v", unmerged))
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
	ctx = session.ctx

	conflict, err := a.requireConflict(session, conflictID)
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

// plannedResolvedChange is one statically validated resolution of a conflicted
// path, including the file mode that must survive the write.
type plannedResolvedChange struct {
	path    string
	action  ChangeAction
	content string
	mode    fs.FileMode
}

// planResolved validates the resolved files against the captured conflict
// without touching the repository: unsupported conflict types, unknown or
// duplicate paths, invalid actions, incomplete coverage and content beyond the
// documented text limits are all refused here.
func planResolved(conflict state.Conflict, resolved []ResolvedFile) ([]plannedResolvedChange, error) {
	expected := make(map[string]state.ConflictFile, len(conflict.Files))
	for _, file := range conflict.Files {
		if file.Kind == syncer.KindBinary || file.Kind == syncer.KindUnsupported {
			return nil, memerr.New(memerr.Unsupported,
				"path %q has an unsupported conflict type (%s); resolve it manually outside memctl", file.Path, file.Kind)
		}
		expected[file.Path] = file
	}

	seen := map[string]bool{}
	plans := make([]plannedResolvedChange, 0, len(resolved))
	total := 0
	for _, file := range resolved {
		clean, err := vault.ValidateRelPath(file.Path)
		if err != nil {
			return nil, err
		}
		target, ok := expected[clean]
		if !ok {
			return nil, memerr.New(memerr.InvalidArgument,
				"resolved path %q is not part of conflict %s", clean, conflict.ConflictID)
		}
		if seen[clean] {
			return nil, memerr.New(memerr.InvalidArgument, "resolved path %q appears more than once", clean)
		}
		seen[clean] = true

		switch file.Action {
		case ActionDelete:
			plans = append(plans, plannedResolvedChange{path: clean, action: ActionDelete})
		case ActionPut:
			if len(file.RawContent) > vault.MaxTextFileBytes {
				return nil, memerr.New(memerr.LimitExceeded,
					"resolved content for %q is %d bytes, exceeding the %d byte text limit", clean, len(file.RawContent), vault.MaxTextFileBytes)
			}
			total += len(file.RawContent)
			if total > MaxMutationBytes {
				return nil, memerr.New(memerr.LimitExceeded,
					"resolved content exceeds the %d byte per-request limit", MaxMutationBytes)
			}
			if !utf8.ValidString(file.RawContent) {
				return nil, memerr.New(memerr.InvalidArgument, "resolved content for %q is not valid UTF-8 text", clean)
			}
			plans = append(plans, plannedResolvedChange{
				path: clean, action: ActionPut, content: file.RawContent, mode: resolvedMode(target),
			})
		default:
			return nil, memerr.New(memerr.InvalidArgument,
				"unknown action %q for resolved path %q; expected put or delete", file.Action, clean)
		}
	}
	if len(seen) != len(expected) {
		return nil, memerr.New(memerr.InvalidArgument,
			"resolvedFiles must cover every conflicted path of conflict %s (%d given, %d required)",
			conflict.ConflictID, len(seen), len(expected))
	}
	return plans, nil
}

// resolvedMode preserves the file mode the path had before the replay,
// falling back to the other captured sides and finally to a regular file.
func resolvedMode(file state.ConflictFile) fs.FileMode {
	for _, side := range []state.ConflictSide{file.Replayed, file.Onto, file.Base} {
		if side.Exists && side.Mode != "" {
			return fsMode(side.Mode)
		}
	}
	return 0o644
}

// applyResolved writes or deletes exactly the planned paths of the current
// stage and stages them.
func (a *App) applyResolved(ctx context.Context, session *writeSession, plans []plannedResolvedChange) error {
	paths := make([]string, 0, len(plans))
	for _, plan := range plans {
		switch plan.action {
		case ActionDelete:
			if err := session.root.RemoveFile(plan.path); err != nil {
				return err
			}
		case ActionPut:
			if err := session.root.WriteAtomic(plan.path, []byte(plan.content), plan.mode); err != nil {
				return err
			}
		}
		paths = append(paths, plan.path)
	}
	return git.Add(ctx, session.repoPath, paths...)
}

// restoreFailedResolution returns the repository to a state the caller can
// retry from after a step that ran past the captured-stage restart failed. A
// captured stage is aborted back to its recorded state; a resolving stage keeps
// the owned rebase and reports the manual entry point because its stopped index
// cannot be reconstructed here.
func (a *App) restoreFailedResolution(ctx context.Context, session *writeSession, conflict state.Conflict, cause error) error {
	if conflict.Phase != syncer.PhaseCaptured {
		return memerr.Wrap(memerr.RecoveryRequired, cause,
			"applying the resolution of conflict %s failed; inspect the worktree and run `memctl resolve --abort` or retry", conflict.ConflictID)
	}
	if err := git.RebaseAbort(ctx, session.repoPath); err != nil {
		return memerr.Wrap(memerr.RecoveryRequired, err,
			"the resolution of conflict %s failed (%v) and the owned rebase could not be aborted; inspect the repository manually", conflict.ConflictID, cause)
	}
	if head, err := git.Head(ctx, session.repoPath); err != nil || head != conflict.ExpectedHead {
		return memerr.New(memerr.RecoveryRequired,
			"the resolution of conflict %s failed (%v) and HEAD is not the captured commit; inspect the repository manually", conflict.ConflictID, cause)
	}
	if dirty, err := git.StatusPorcelain(ctx, session.repoPath); err != nil {
		return err
	} else if dirty != "" {
		return memerr.New(memerr.RecoveryRequired,
			"the resolution of conflict %s failed (%v) and left worktree changes; inspect the repository manually", conflict.ConflictID, cause)
	}
	if operation, ok, err := session.store.LoadOperation(); err != nil {
		return err
	} else if ok {
		operation.Phase = syncer.PhaseCaptured
		if err := session.store.SaveOperation(operation); err != nil {
			return err
		}
	}
	return cause
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

func (a *App) requireConflict(session *writeSession, conflictID string) (state.Conflict, error) {
	if conflictID == "" {
		return state.Conflict{}, memerr.New(memerr.InvalidArgument,
			"a conflict id is required; run `memctl resolve` to read the active conflict")
	}
	conflict, ok, err := session.store.LoadConflict()
	if err != nil {
		return state.Conflict{}, err
	}
	if !ok {
		return state.Conflict{}, memerr.New(memerr.NotFound, "no conflict is awaiting resolution")
	}
	if conflict.ConflictID != conflictID {
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
	session, err := a.beginLocked(ctx, true, true, false)
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
