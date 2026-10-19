package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
	"github.com/gray0128/memctl/internal/vault"
)

// Documented resource limits from
// docs/02-domain-models-and-contracts.md section 4.1.
const (
	MaxMutationFiles = 100
	MaxMutationBytes = 8 << 20
)

// ChangeAction is the requested operation on one path.
type ChangeAction string

const (
	ActionPut    ChangeAction = "put"
	ActionDelete ChangeAction = "delete"
	// actionMove is an internal transaction action for a file rename. It is
	// never accepted from a MutationRequest, whose public actions stay put and
	// delete. It carries the destination in path and the source in moveFrom.
	actionMove ChangeAction = "move"
)

// FileChange is one path change inside a MutationRequest. ExpectedRevision is
// either a fileRevision or the literal "absent" for creation.
type FileChange struct {
	Path             string
	ExpectedRevision string
	Action           ChangeAction
	RawContent       string
}

// MutationRequest is the application write request. BaseRevision is optional
// and, when present, is checked against the whole-repository revision.
type MutationRequest struct {
	BaseRevision string
	Changes      []FileChange
	Agent        string
	SessionID    string
	// Device is the normalized device label recorded in the commit source
	// trailer so the read-only audit view can resolve it.
	Device string
	// DreamProposalID marks a transaction that commits one Dreaming proposal.
	// commit() keeps the completed transaction record when it is set, so an
	// interrupted apply can be reconciled from the record on retry. See
	// docs/03-workflows-and-dataflows.md section 6 steps 6-7.
	DreamProposalID string
	// Operation overrides the short operation label in the local commit
	// message. An empty value means "write".
	Operation string
}

// MutationResult is the outcome of one local transaction.
type MutationResult struct {
	TransactionID string   `json:"transactionId,omitempty"`
	Revision      string   `json:"revision,omitempty"`
	Commit        string   `json:"commit,omitempty"`
	ChangedFiles  []string `json:"changedFiles"`
	SyncState     string   `json:"syncState"`
}

// transactionTrailer identifies a commit created by one local transaction.
const transactionTrailer = "Memctl-Transaction: "

// errSimulatedCrash is returned by fault injection to model a process that
// stops between transaction steps without any cleanup.
var errSimulatedCrash = errors.New("simulated interruption")

// writeSession holds the locked repository resources of one mutation.
type writeSession struct {
	repoPath string
	root     *vault.Root
	gitDir   string
	store    *state.Store
	lock     *lock.Lock
	// ctx carries the lock descriptor so Git children inherit the lock for
	// their lifetime. See docs/01-architecture-and-modules.md section 6.
	ctx context.Context
}

func (s *writeSession) Close() {
	if s == nil {
		return
	}
	if s.root != nil {
		_ = s.root.Close()
		s.root = nil
	}
	if s.lock != nil {
		_ = s.lock.Release()
		s.lock = nil
	}
}

// Apply performs one local managed write transaction: lock, revalidate every
// baseline, persist a recovery record, replace files, stage exactly the planned
// paths and commit locally. Remote synchronization is out of scope for A-3.
func (a *App) Apply(ctx context.Context, req MutationRequest) (MutationResult, error) {
	session, err := a.beginWrite(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer session.Close()
	// Every Git child of this session inherits the lock descriptor, so a
	// killed parent cannot leave Git writing after the lock is released.
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, req.BaseRevision); err != nil {
		return MutationResult{}, err
	}
	plans, err := session.plan(req)
	if err != nil {
		return MutationResult{}, err
	}
	operation := req.Operation
	if operation == "" {
		operation = "write"
	}
	result, err := a.commit(ctx, session, operation, req, plans)
	if err != nil {
		return result, err
	}
	return a.finishWrite(session, result), nil
}

// requireWriteBaseline enforces the preconditions shared by every managed
// write: a clean worktree and index, a resolvable commit identity and, when the
// caller supplied one, a current whole-repository baseline.
func (a *App) requireWriteBaseline(ctx context.Context, session *writeSession, baseRevision string) error {
	status, err := git.StatusPorcelain(ctx, session.repoPath)
	if err != nil {
		return err
	}
	if status != "" {
		return memerr.New(memerr.ExternalChanges,
			"the repository has staged, unstaged or untracked changes; commit them with `memctl sync --capture` after stopping other writers")
	}
	if err := git.RequireIdentity(ctx, session.repoPath); err != nil {
		return err
	}
	if baseRevision != "" {
		current, err := a.repoRevision(ctx, session)
		if err != nil {
			return err
		}
		if baseRevision != current {
			return memerr.New(memerr.RevisionMismatch,
				"the repository revision changed since it was read; reread the affected files before writing")
		}
	}
	return nil
}

// commit applies an already-planned and revalidated change list as one local
// transaction. The caller keeps the session open: it owns the final close,
// background-sync trigger and mutation notification through finishWrite.
func (a *App) commit(ctx context.Context, session *writeSession, operation string, req MutationRequest, plans []plannedChange) (MutationResult, error) {
	head, err := git.Head(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}

	txID, err := state.NewTransactionID()
	if err != nil {
		return MutationResult{}, err
	}
	record := state.Transaction{
		ID:           txID,
		Kind:         state.KindMutation,
		OriginalHead: head,
		StartedAt:    state.StartedAt(),
		Message:      commitMessage(operation, req.Agent, req.SessionID, req.Device, txID),
		ProposalID:   req.DreamProposalID,
	}
	for _, plan := range plans {
		record.Paths = append(record.Paths, plan.records()...)
	}
	if err := session.store.Save(record); err != nil {
		return MutationResult{}, err
	}
	if err := a.fault("after-record"); err != nil {
		return MutationResult{}, err
	}

	for index, plan := range plans {
		switch plan.action {
		case ActionDelete:
			err = session.root.RemoveFile(plan.path)
		case actionMove:
			err = session.root.RenameFile(plan.moveFrom, plan.path)
		default:
			err = session.root.WriteAtomic(plan.path, []byte(plan.content), plan.fsMode)
		}
		if err != nil {
			return MutationResult{TransactionID: txID}, err
		}
		if err := a.fault(fmt.Sprintf("after-write:%d", index)); err != nil {
			return MutationResult{}, err
		}
	}

	paths := planGitPaths(plans)
	if err := stagePlans(ctx, session, plans); err != nil {
		return session.abort(ctx, txID, record, err)
	}
	staged, err := git.StagedPaths(ctx, session.repoPath)
	if err != nil {
		return session.abort(ctx, txID, record, err)
	}
	if !samePathSet(staged, paths) {
		return session.abort(ctx, txID, record, memerr.New(memerr.GitFailed,
			"staged paths %v do not match the planned paths %v; refusing to commit unrelated content", staged, paths))
	}

	commit, err := git.Commit(ctx, session.repoPath, record.Message)
	if err != nil {
		return session.abort(ctx, txID, record, err)
	}
	if err := a.fault("after-commit"); err != nil {
		return MutationResult{}, err
	}

	committed, err := git.CommittedPaths(ctx, session.repoPath, commit)
	if err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	if !samePathSet(committed, paths) {
		return MutationResult{TransactionID: txID, Commit: commit}, memerr.New(memerr.GitFailed,
			"commit %s changed %v instead of the planned paths %v; inspect `memctl recover --status`", commit, committed, paths)
	}

	if req.DreamProposalID != "" {
		// A Dreaming apply keeps its completed record until the proposal status
		// is written, so a retry can prove the commit happened instead of
		// committing the same proposal twice. The apply workflow deletes it.
		record.Completed = true
		record.Commit = commit
		if err := session.store.Save(record); err != nil {
			return MutationResult{TransactionID: txID, Commit: commit}, err
		}
	} else if err := session.store.Delete(txID); err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	result := MutationResult{
		TransactionID: txID,
		Revision:      revision,
		Commit:        commit,
		ChangedFiles:  paths,
		SyncState:     session.remoteStateAfterCommit(ctx),
	}
	a.persistLocalCommit(session, result)
	return result, nil
}

// finishWrite releases the lock before triggering the best-effort background
// sync, then notifies the UI that the local commit exists. A failure to derive
// the background worker never changes the result.
func (a *App) finishWrite(session *writeSession, result MutationResult) MutationResult {
	session.Close()
	a.triggerBackgroundSync()
	a.notifyMutation(result)
	return result
}

// Capture accepts serial external edits explicitly. It freezes the changed path
// list first, then stages exactly that list and commits it. It is the only
// local path that commits content memctl did not itself write.
func (a *App) Capture(ctx context.Context, agent, sessionID, device string) (MutationResult, error) {
	session, err := a.beginWrite(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := git.RequireIdentity(ctx, session.repoPath); err != nil {
		return MutationResult{}, err
	}

	paths, err := git.ChangedPaths(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return MutationResult{}, err
	}
	if len(paths) == 0 {
		return MutationResult{Revision: revision, SyncState: syncer.StateLocalOnly}, nil
	}
	if len(paths) > MaxMutationFiles {
		return MutationResult{}, memerr.New(memerr.LimitExceeded,
			"capture found %d changed files, exceeding the %d file limit", len(paths), MaxMutationFiles)
	}

	var total int64
	for _, rel := range paths {
		meta, err := session.root.Stat(rel)
		if err != nil {
			return MutationResult{}, err
		}
		// A path deleted after the preflight is a legitimate capture input;
		// `git add` stages the removal. Only present files count toward the
		// content limit.
		if !meta.Exists {
			continue
		}
		total += meta.Size
		if total > MaxMutationBytes {
			return MutationResult{}, memerr.New(memerr.LimitExceeded,
				"capture content exceeds the %d byte per-request limit", MaxMutationBytes)
		}
	}

	if err := git.Add(ctx, session.repoPath, paths...); err != nil {
		return MutationResult{}, err
	}
	staged, err := git.StagedPaths(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	if !samePathSet(staged, paths) {
		return MutationResult{}, memerr.New(memerr.GitFailed,
			"staged paths %v do not match the capture list %v", staged, paths)
	}

	txID, err := state.NewTransactionID()
	if err != nil {
		return MutationResult{}, err
	}
	commit, err := git.Commit(ctx, session.repoPath, commitMessage("capture", agent, sessionID, device, txID))
	if err != nil {
		_ = git.ResetPaths(ctx, session.repoPath, paths)
		return MutationResult{}, err
	}
	committed, err := git.CommittedPaths(ctx, session.repoPath, commit)
	if err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	if !samePathSet(committed, paths) {
		return MutationResult{TransactionID: txID, Commit: commit}, memerr.New(memerr.GitFailed,
			"capture commit %s changed %v instead of the frozen list %v", commit, committed, paths)
	}

	revision, err = a.repoRevision(ctx, session)
	if err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	result := MutationResult{
		TransactionID: txID,
		Revision:      revision,
		Commit:        commit,
		ChangedFiles:  paths,
		SyncState:     session.remoteStateAfterCommit(ctx),
	}
	a.persistLocalCommit(session, result)
	a.notifyMutation(result)
	return result, nil
}

// plannedChange is one validated, fully-resolved path change.
type plannedChange struct {
	path            string
	action          ChangeAction
	content         string
	before          []byte
	existedBefore   bool
	fsMode          fs.FileMode
	gitMode         string
	plannedRevision string
	// moveFrom is the source path of an actionMove change; path is the
	// destination. It is empty for put and delete.
	moveFrom string
}

// records returns the pre-image records one change contributes to the
// transaction. A move contributes two: the source it removes and the
// destination it creates. A case-only rename also records the intermediate name
// so a crash between the two renames can remove the leftover.
func (p plannedChange) records() []state.PathRecord {
	if p.action == actionMove {
		records := []state.PathRecord{
			{
				Path:            p.moveFrom,
				Existed:         true,
				Mode:            p.gitMode,
				Before:          string(p.before),
				PlannedRevision: vault.AbsentRevision,
			},
			{
				Path:            p.path,
				Existed:         false,
				Mode:            p.gitMode,
				PlannedRevision: p.plannedRevision,
			},
		}
		if intermediate, caseOnly := vault.MoveIntermediatePath(p.moveFrom, p.path); caseOnly {
			records = append(records, state.PathRecord{
				Path:            intermediate,
				Existed:         false,
				Mode:            p.gitMode,
				PlannedRevision: p.plannedRevision,
			})
		}
		return records
	}
	return []state.PathRecord{{
		Path:            p.path,
		Existed:         p.existedBefore,
		Mode:            p.gitMode,
		Before:          string(p.before),
		PlannedRevision: p.plannedRevision,
	}}
}

// gitPaths returns the paths one change stages. A move stages both the source
// removal and the destination addition.
func (p plannedChange) gitPaths() []string {
	if p.action == actionMove {
		return []string{p.moveFrom, p.path}
	}
	return []string{p.path}
}

// plan validates every change against the current disk state and builds the
// complete pre-image and planned-content list.
func (s *writeSession) plan(req MutationRequest) ([]plannedChange, error) {
	if len(req.Changes) == 0 {
		return nil, memerr.New(memerr.InvalidArgument, "a mutation request needs at least one change")
	}
	if len(req.Changes) > MaxMutationFiles {
		return nil, memerr.New(memerr.LimitExceeded,
			"mutation has %d changes, exceeding the %d file limit", len(req.Changes), MaxMutationFiles)
	}

	seen := map[string]bool{}
	plans := make([]plannedChange, 0, len(req.Changes))
	total := 0
	for _, change := range req.Changes {
		clean, err := vault.ValidateRelPath(change.Path)
		if err != nil {
			return nil, err
		}
		if seen[clean] {
			return nil, memerr.New(memerr.InvalidArgument, "path %q appears more than once in the request", clean)
		}
		seen[clean] = true

		switch change.Action {
		case ActionPut:
			if len(change.RawContent) > vault.MaxTextFileBytes {
				return nil, memerr.New(memerr.LimitExceeded,
					"content for %q is %d bytes, exceeding the %d byte text limit", clean, len(change.RawContent), vault.MaxTextFileBytes)
			}
			total += len(change.RawContent)
			if total > MaxMutationBytes {
				return nil, memerr.New(memerr.LimitExceeded,
					"mutation content exceeds the %d byte per-request limit", MaxMutationBytes)
			}
			plan, err := s.planPut(clean, change)
			if err != nil {
				return nil, err
			}
			plans = append(plans, plan)
		case ActionDelete:
			plan, err := s.planDelete(clean, change)
			if err != nil {
				return nil, err
			}
			plans = append(plans, plan)
		default:
			return nil, memerr.New(memerr.InvalidArgument,
				"unknown action %q for path %q; expected put or delete", change.Action, clean)
		}
	}
	return plans, nil
}

func (s *writeSession) planPut(clean string, change FileChange) (plannedChange, error) {
	plan := plannedChange{path: clean, action: ActionPut, content: change.RawContent}
	if err := s.root.RequireParentDir(clean); err != nil {
		return plannedChange{}, err
	}
	if change.ExpectedRevision == vault.AbsentRevision {
		meta, err := s.root.Stat(clean)
		if err != nil {
			return plannedChange{}, err
		}
		if meta.Exists {
			return plannedChange{}, memerr.New(memerr.RevisionMismatch,
				"path %q already exists but the request expected it to be absent", clean)
		}
		plan.fsMode = 0o644
	} else {
		file, err := s.root.ReadFile(clean)
		if err != nil {
			return plannedChange{}, err
		}
		meta, err := s.root.Stat(clean)
		if err != nil {
			return plannedChange{}, err
		}
		if meta.Links > 1 {
			return plannedChange{}, memerr.New(memerr.Unsupported,
				"path %q has %d hard links; refusing to modify content shared with another name", clean, meta.Links)
		}
		if file.FileRevision != change.ExpectedRevision {
			return plannedChange{}, memerr.New(memerr.RevisionMismatch,
				"path %q changed since it was read; reread the file before writing", clean)
		}
		plan.before = []byte(file.RawContent)
		plan.existedBefore = true
		plan.fsMode = file.Mode
	}
	plan.gitMode = vault.GitMode(plan.fsMode)
	plan.plannedRevision = vault.FileRevision(plan.gitMode, []byte(plan.content))
	return plan, nil
}

func (s *writeSession) planDelete(clean string, change FileChange) (plannedChange, error) {
	if change.ExpectedRevision == vault.AbsentRevision {
		return plannedChange{}, memerr.New(memerr.InvalidArgument,
			"cannot delete %q with an absent baseline", clean)
	}
	file, err := s.root.ReadFile(clean)
	if err != nil {
		return plannedChange{}, err
	}
	meta, err := s.root.Stat(clean)
	if err != nil {
		return plannedChange{}, err
	}
	if meta.Links > 1 {
		return plannedChange{}, memerr.New(memerr.Unsupported,
			"path %q has %d hard links; refusing to delete content shared with another name", clean, meta.Links)
	}
	if file.FileRevision != change.ExpectedRevision {
		return plannedChange{}, memerr.New(memerr.RevisionMismatch,
			"path %q changed since it was read; reread the file before deleting it", clean)
	}
	return plannedChange{
		path:            clean,
		action:          ActionDelete,
		before:          []byte(file.RawContent),
		existedBefore:   true,
		fsMode:          file.Mode,
		gitMode:         vault.GitMode(file.Mode),
		plannedRevision: vault.AbsentRevision,
	}, nil
}

// abort restores the pre-images of a transaction that failed before its commit
// and cleans the index. If the restore itself fails, the recovery record stays
// so `recover` can resolve it.
func (s *writeSession) abort(ctx context.Context, txID string, record state.Transaction, cause error) (MutationResult, error) {
	if rollbackErr := s.restoreSelected(ctx, record, record.Paths); rollbackErr != nil {
		return MutationResult{TransactionID: txID}, memerr.Wrap(memerr.RecoveryRequired, rollbackErr,
			"transaction %s failed (%v) and could not be rolled back automatically; run `memctl recover --status`", txID, cause)
	}
	if err := s.store.Delete(txID); err != nil {
		return MutationResult{TransactionID: txID}, err
	}
	return MutationResult{TransactionID: txID}, cause
}

// restoreSelected rewrites the selected pre-images and unstages every path of
// the transaction. Paths not selected are left untouched.
func (s *writeSession) restoreSelected(ctx context.Context, record state.Transaction, selected []state.PathRecord) error {
	if err := git.ResetPaths(ctx, s.repoPath, record.AffectedPaths()); err != nil {
		return err
	}
	// Remove transaction-created paths before rewriting the pre-images. On a
	// case-insensitive filesystem a case-only rename leaves the source and
	// destination spelling pointing at the same file, so the removal must
	// happen first or it would delete the restored content.
	for _, pathRecord := range selected {
		if pathRecord.Existed {
			continue
		}
		meta, err := s.root.Stat(pathRecord.Path)
		if err != nil {
			return err
		}
		if meta.Exists {
			if err := s.root.RemoveFile(pathRecord.Path); err != nil {
				return err
			}
		}
	}
	for _, pathRecord := range selected {
		if !pathRecord.Existed {
			continue
		}
		if err := s.root.WriteAtomic(pathRecord.Path, []byte(pathRecord.Before), fsMode(pathRecord.Mode)); err != nil {
			return err
		}
	}
	return nil
}

// beginWrite acquires the repository lock and refuses to start when Git is in
// an intermediate state or an unfinished transaction still needs recovery.
func (a *App) beginWrite(ctx context.Context) (*writeSession, error) {
	return a.beginProtected(ctx, true, false)
}

// beginRead takes the repository lock without waiting and refuses to read while
// a managed transaction, an awaiting conflict or an active Git intermediate
// state makes the on-disk content an inconsistent read. See
// docs/03-workflows-and-dataflows.md section 2.1: a CLI read reports busy
// instead of returning transaction-intermediate content. The entry file is not
// required here so an unfinished transaction that removed it is still reported
// as recovery, not as a missing repository.
func (a *App) beginRead(ctx context.Context) (*writeSession, error) {
	return a.beginProtected(ctx, false, true)
}

func (a *App) beginProtected(ctx context.Context, requireEntry, nonBlocking bool) (*writeSession, error) {
	session, err := a.beginLocked(ctx, requireEntry, false, nonBlocking)
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
	return session, nil
}

// beginConflictRead takes the repository lock without waiting and allows the
// Git intermediate state an owned rebase leaves behind, so the conflict view
// stays readable while resolution is in progress. Ordinary reads must still
// refuse a transaction-intermediate tree, so they keep using beginRead. An
// unfinished transaction is reported instead of being treated as readable.
func (a *App) beginConflictRead(ctx context.Context) (*writeSession, error) {
	session, err := a.beginLocked(ctx, true, true, true)
	if err != nil {
		return nil, err
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
	return session, nil
}

// beginLocked validates the repository, takes the lock and refuses an active
// Git operation. It deliberately allows unfinished transactions so recovery
// can run. requireEntry is false for recovery, which must work when an
// interrupted transaction deleted the entry file. allowGitState lets the
// conflict resolver continue an operation memctl owns. When background is set
// the lock is taken without waiting so a burst of hook calls exits at once.
func (a *App) beginLocked(ctx context.Context, requireEntry bool, allowGitState bool, background bool) (*writeSession, error) {
	repoPath, err := a.requireRepoRoot(ctx)
	if err != nil {
		return nil, err
	}
	if requireEntry {
		if _, err := os.Lstat(filepath.Join(repoPath, vault.EntryPointName)); err != nil {
			return nil, memerr.New(memerr.NoRepo, "%s has no %s; run memctl init", repoPath, vault.EntryPointName)
		}
	}
	gitDir, err := git.GitDir(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	var held *lock.Lock
	if background {
		acquired, ok, err := lock.TryAcquire(gitDir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, memerr.New(memerr.Busy,
				"repository lock %s is held by another process; retry after it finishes", filepath.Join(gitDir, lock.FileName))
		}
		held = acquired
	} else {
		held, err = lock.Acquire(ctx, gitDir)
		if err != nil {
			return nil, err
		}
	}
	session := &writeSession{
		repoPath: repoPath,
		gitDir:   gitDir,
		store:    state.OpenStore(gitDir),
		lock:     held,
		ctx:      git.WithInheritedFile(ctx, held.File()),
	}
	root, err := vault.OpenRoot(repoPath)
	if err != nil {
		session.Close()
		return nil, err
	}
	session.root = root

	repoState, err := git.Inspect(session.ctx, repoPath)
	if err != nil {
		session.Close()
		return nil, err
	}
	if allowGitState {
		return session, nil
	}
	switch {
	case repoState.IndexLocked:
		session.Close()
		return nil, memerr.New(memerr.Busy, "another Git process is writing the index (.git/index.lock exists)")
	case repoState.Unmerged:
		session.Close()
		return nil, memerr.New(memerr.OperationActive,
			"the index has unmerged entries; resolve the conflict before writing memory")
	case repoState.Operation != "" || repoState.Sequencer:
		session.Close()
		return nil, memerr.New(memerr.OperationActive,
			"a %s operation is in progress; memctl does not abort or continue user Git operations", repoState.Operation)
	}
	return session, nil
}

func (a *App) fault(stage string) error {
	if a.testFault == nil {
		return nil
	}
	return a.testFault(stage)
}

// planGitPaths returns every path the planned changes stage, including both
// sides of a move.
func planGitPaths(plans []plannedChange) []string {
	paths := make([]string, 0, len(plans))
	for _, plan := range plans {
		paths = append(paths, plan.gitPaths()...)
	}
	return paths
}

// stagePlans stages exactly the planned paths. A case-only rename first goes
// through `git mv -f`, because `git add` cannot record a case change on a
// case-insensitive index; the following `git add` keeps the rename and stages
// every other planned path.
func stagePlans(ctx context.Context, session *writeSession, plans []plannedChange) error {
	for _, plan := range plans {
		if plan.action != actionMove {
			continue
		}
		if plan.moveFrom == plan.path || !strings.EqualFold(plan.moveFrom, plan.path) {
			continue
		}
		if err := git.MoveForce(ctx, session.repoPath, plan.moveFrom, plan.path); err != nil {
			return err
		}
	}
	return git.Add(ctx, session.repoPath, planGitPaths(plans)...)
}

// remoteStateAfterCommit reports the remote state of a fresh local commit: a
// repository with a remote stays pending until a push succeeds, while a
// local-only repository never claims a pending remote. See the
// `Synced --> Pending: 新本地提交` transition in
// docs/03-workflows-and-dataflows.md section 4.2.
func (s *writeSession) remoteStateAfterCommit(ctx context.Context) string {
	_, hasRemote, err := git.DefaultRemote(ctx, s.repoPath)
	if err != nil || !hasRemote {
		return syncer.StateLocalOnly
	}
	return syncer.StatePending
}

// persistLocalCommit marks the cached sync summary pending after a local commit
// so a later read of the state does not report a stale synced state. Counts are
// only refreshed when an upstream is already configured.
func (a *App) persistLocalCommit(session *writeSession, result MutationResult) {
	if result.SyncState != syncer.StatePending {
		return
	}
	repoID := config.RepoID(session.repoPath)
	cache := state.OpenCache(a.cfg.StateDir, repoID)
	record, _, err := cache.LoadSync()
	if err != nil {
		return
	}
	record.RepoID = repoID
	record.RemoteState = syncer.StatePending
	record.ReasonCode = ""
	record.Head = result.Commit
	record.LocalState = "clean"
	if upstream, ok, err := git.UpstreamRef(session.ctx, session.repoPath); err == nil && ok {
		if ahead, behind, err := git.AheadBehind(session.ctx, session.repoPath, upstream); err == nil {
			record.Ahead, record.Behind = ahead, behind
		}
	}
	_ = cache.SaveSync(record)
}

// commitMessage renders one managed commit: the operation title, the
// transaction trailer recovery relies on, then the provenance trailers the
// audit view reads. See docs/03-workflows-and-dataflows.md section 2 step 5.
func commitMessage(operation, agent, sessionID, device, txID string) string {
	body := "memctl " + operation + "\n\n" + transactionTrailer + txID
	return git.AppendTrailers(body, (git.Source{Agent: agent, SessionID: sessionID, Device: device}).Trailers()...)
}

func fsMode(gitMode string) fs.FileMode {
	if gitMode == "100755" {
		return 0o755
	}
	return 0o644
}

func samePathSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	sortedGot := append([]string(nil), got...)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedGot)
	sort.Strings(sortedWant)
	for i := range sortedWant {
		if sortedGot[i] != sortedWant[i] {
			return false
		}
	}
	return true
}

// repoRevision is the deterministic digest of HEAD, the managed path list and
// each file revision. The definition lives in vault so diagnosis and the write
// transaction share one baseline.
func (a *App) repoRevision(ctx context.Context, session *writeSession) (string, error) {
	head := ""
	if value, err := git.Head(ctx, session.repoPath); err == nil {
		head = value
	}
	return vault.RepoRevision(session.root, session.repoPath, head)
}
