package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
)

// Revert applies the inverse of one single-parent commit and creates a new
// commit, keeping the old history. It refuses a root commit and a merge commit
// with UNSUPPORTED and a commit outside the current history with NOT_FOUND. A
// conflicting revert is diagnosed, aborted and verified so the caller is left
// with the original history instead of a half-applied revert.
// See docs/03-workflows-and-dataflows.md section 5.3.
func (a *App) Revert(ctx context.Context, hash, baseRevision, agent, sessionID, device string) (MutationResult, error) {
	if baseRevision == "" {
		return MutationResult{}, memerr.New(memerr.PreconditionRequired,
			"reverting a commit requires the repository baseRevision")
	}

	session, err := a.beginWrite(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, baseRevision); err != nil {
		return MutationResult{}, err
	}

	commit, err := git.ResolveCommit(ctx, session.repoPath, hash)
	if err != nil {
		return MutationResult{}, err
	}
	parents, err := git.CommitParents(ctx, session.repoPath, commit)
	if err != nil {
		return MutationResult{}, err
	}
	if len(parents) == 0 {
		return MutationResult{}, memerr.New(memerr.Unsupported,
			"commit %s is a root commit; memctl only reverts a single-parent commit", shortHash(commit))
	}
	if len(parents) > 1 {
		return MutationResult{}, memerr.New(memerr.Unsupported,
			"commit %s is a merge commit; memctl only reverts a single-parent commit", shortHash(commit))
	}
	reachable, err := git.IsAncestor(ctx, session.repoPath, commit, "HEAD")
	if err != nil {
		return MutationResult{}, err
	}
	if !reachable {
		return MutationResult{}, memerr.New(memerr.NotFound,
			"commit %s is not part of the current history", shortHash(commit))
	}
	changed, err := git.CommittedPaths(ctx, session.repoPath, commit)
	if err != nil {
		return MutationResult{}, err
	}
	if len(changed) > MaxMutationFiles {
		return MutationResult{}, memerr.New(memerr.LimitExceeded,
			"commit %s changes %d files, exceeding the %d file limit", shortHash(commit), len(changed), MaxMutationFiles)
	}
	originalHead, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}

	// Register ownership before touching Git so an interrupted revert can be
	// told apart from an operation the user started.
	operationID, err := state.NewOperationID()
	if err != nil {
		return MutationResult{}, err
	}
	if err := session.store.SaveOperation(state.Operation{
		OperationID:  operationID,
		Kind:         state.KindRevert,
		Owner:        state.OwnerMemctl,
		OriginalHead: originalHead,
		TargetHead:   commit,
		Phase:        "running",
		StartedAt:    state.Now(),
	}); err != nil {
		return MutationResult{}, err
	}

	// The ownership record is cleared only when the revert is proven finished:
	// a verified commit, or a diagnosed conflict that was aborted and restored.
	// A recovery that cannot be proven keeps the record so doctor can explain
	// the leftover Git state. See docs/03-workflows-and-dataflows.md section 5.3.
	if revertErr := git.Revert(ctx, session.repoPath, commit); revertErr != nil {
		return a.recoverFailedRevert(ctx, session, operationID, originalHead, revertErr)
	}
	// `git revert` writes its own message. Amend it so the audit view resolves
	// the same source trailers as every other managed commit; the reverted
	// commit id stays in the message text. Amending rewrites the commit id, so
	// HEAD is read afterwards.
	if err := amendRevertMessage(ctx, session.repoPath, revertSource(agent, sessionID, device)); err != nil {
		return MutationResult{}, err
	}
	head, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	committed, err := git.CommittedPaths(ctx, session.repoPath, head)
	if err != nil {
		return MutationResult{Commit: head}, err
	}
	// A Git hook must not widen the revert commit beyond the reverted paths.
	if !isSubset(committed, changed) {
		return MutationResult{Commit: head}, memerr.New(memerr.GitFailed,
			"revert commit %s changed %v, which is not a subset of the reverted paths %v; inspect the commit before relying on it",
			shortHash(head), committed, changed)
	}
	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return MutationResult{Commit: head}, err
	}
	result := MutationResult{
		Revision:     revision,
		Commit:       head,
		ChangedFiles: committed,
		SyncState:    session.remoteStateAfterCommit(ctx),
	}
	if err := session.store.DeleteOperation(); err != nil {
		return result, err
	}
	a.persistLocalCommit(session, result)
	return a.finishWrite(session, result), nil
}

// revertSource is the provenance of one revert. A bare library call records
// memctl itself; a caller that resolved an identity records that instead.
func revertSource(agent, sessionID, device string) git.Source {
	if agent == "" {
		agent = git.LocalAgent
	}
	return git.Source{Agent: agent, SessionID: sessionID, Device: device}
}

// amendRevertMessage appends the caller provenance to the revert commit Git
// just created. A revert commit without provenance would show up in the audit
// view as unattributed even though memctl knows who asked for it.
func amendRevertMessage(ctx context.Context, repoPath string, source git.Source) error {
	message, err := git.HeadMessage(ctx, repoPath)
	if err != nil {
		return err
	}
	return git.AmendMessage(ctx, repoPath, git.AppendTrailers(message, source.Trailers()...))
}

// recoverFailedRevert diagnoses a failed revert, aborts the revert memctl
// started and verifies that HEAD, the worktree and the index are back at the
// recorded state. A conflict outside that recovery is reported as
// CONTENT_CONFLICT; a recovery that cannot be proven leaves RECOVERY_REQUIRED.
func (a *App) recoverFailedRevert(ctx context.Context, session *writeSession, operationID, originalHead string, cause error) (MutationResult, error) {
	unmerged, err := git.UnmergedPaths(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	var summary string
	if len(unmerged) > 0 {
		captured, captureErr := syncer.CaptureSides(ctx, session.repoPath,
			config.RepoID(session.repoPath), operationID, "", syncer.PhaseCaptured, "")
		if captureErr == nil {
			summary = describeConflictFiles(captured.Files)
		}
		if summary == "" {
			summary = strings.Join(unmerged, ", ")
		}
	}

	repoState, err := git.Inspect(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	if repoState.Operation == "revert" {
		if faultErr := a.fault("revert-abort"); faultErr != nil {
			return MutationResult{}, memerr.Wrap(memerr.RecoveryRequired, faultErr,
				"the revert failed (%v) and the abort could not be verified; inspect the repository manually", cause)
		}
		if abortErr := git.RevertAbort(ctx, session.repoPath); abortErr != nil {
			return MutationResult{}, memerr.Wrap(memerr.RecoveryRequired, abortErr,
				"the revert failed (%v) and could not be aborted; inspect the repository manually", cause)
		}
	}
	head, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	if head != originalHead {
		return MutationResult{}, memerr.New(memerr.RecoveryRequired,
			"the failed revert left HEAD at %s instead of %s; inspect the repository manually",
			shortHash(head), shortHash(originalHead))
	}
	if dirty, err := git.StatusPorcelain(ctx, session.repoPath); err != nil {
		return MutationResult{}, err
	} else if dirty != "" {
		return MutationResult{}, memerr.New(memerr.RecoveryRequired,
			"the failed revert left worktree changes; inspect the repository manually")
	}
	if summary != "" {
		if err := session.store.DeleteOperation(); err != nil {
			return MutationResult{}, err
		}
		return MutationResult{}, memerr.New(memerr.ContentConflict,
			"reverting the commit conflicts in %s; the revert was aborted and the original history restored", summary)
	}
	if err := session.store.DeleteOperation(); err != nil {
		return MutationResult{}, err
	}
	return MutationResult{}, cause
}

// describeConflictFiles names the conflicted paths and their captured kinds in
// one bounded diagnostic string.
func describeConflictFiles(files []state.ConflictFile) string {
	parts := make([]string, 0, len(files))
	for _, file := range files {
		parts = append(parts, fmt.Sprintf("%s (%s)", file.Path, file.Kind))
	}
	return strings.Join(parts, ", ")
}

// isSubset reports whether every value of got appears in want.
func isSubset(got, want []string) bool {
	set := make(map[string]struct{}, len(want))
	for _, value := range want {
		set[value] = struct{}{}
	}
	for _, value := range got {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

// shortHash abbreviates one object name for a diagnostic message.
func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
