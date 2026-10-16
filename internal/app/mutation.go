package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
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
}

// MutationResult is the outcome of one local transaction.
type MutationResult struct {
	TransactionID string
	Revision      string
	Commit        string
	ChangedFiles  []string
	SyncState     string
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
}

func (s *writeSession) Close() {
	if s == nil {
		return
	}
	if s.root != nil {
		_ = s.root.Close()
	}
	if s.lock != nil {
		_ = s.lock.Release()
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

	// A normal transaction requires a clean baseline so that it never stages
	// content the caller did not plan. External edits are captured explicitly.
	status, err := git.StatusPorcelain(ctx, session.repoPath)
	if err != nil {
		return MutationResult{}, err
	}
	if status != "" {
		return MutationResult{}, memerr.New(memerr.ExternalChanges,
			"the repository has staged, unstaged or untracked changes; commit them with `memctl sync --capture` after stopping other writers")
	}
	if err := git.RequireIdentity(ctx, session.repoPath); err != nil {
		return MutationResult{}, err
	}
	if req.BaseRevision != "" {
		current, err := a.repoRevision(ctx, session)
		if err != nil {
			return MutationResult{}, err
		}
		if req.BaseRevision != current {
			return MutationResult{}, memerr.New(memerr.RevisionMismatch,
				"the repository revision changed since it was read; reread the affected files before writing")
		}
	}

	plans, err := session.plan(req)
	if err != nil {
		return MutationResult{}, err
	}

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
		Message:      commitMessage("write", req.Agent, req.SessionID, txID),
	}
	for _, plan := range plans {
		record.Paths = append(record.Paths, state.PathRecord{
			Path:            plan.path,
			Existed:         plan.existedBefore,
			Mode:            plan.gitMode,
			Before:          string(plan.before),
			PlannedRevision: plan.plannedRevision,
		})
	}
	if err := session.store.Save(record); err != nil {
		return MutationResult{}, err
	}
	if err := a.fault("after-record"); err != nil {
		return MutationResult{}, err
	}

	for index, plan := range plans {
		if plan.action == ActionDelete {
			err = session.root.RemoveFile(plan.path)
		} else {
			err = session.root.WriteAtomic(plan.path, []byte(plan.content), plan.fsMode)
		}
		if err != nil {
			return MutationResult{TransactionID: txID}, err
		}
		if err := a.fault(fmt.Sprintf("after-write:%d", index)); err != nil {
			return MutationResult{}, err
		}
	}

	paths := planPaths(plans)
	if err := git.Add(ctx, session.repoPath, paths...); err != nil {
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

	if err := session.store.Delete(txID); err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	revision, err := a.repoRevision(ctx, session)
	if err != nil {
		return MutationResult{TransactionID: txID, Commit: commit}, err
	}
	return MutationResult{
		TransactionID: txID,
		Revision:      revision,
		Commit:        commit,
		ChangedFiles:  paths,
		SyncState:     "local_only",
	}, nil
}

// Capture accepts serial external edits explicitly. It freezes the changed path
// list first, then stages exactly that list and commits it. It is the only
// local path that commits content memctl did not itself write.
func (a *App) Capture(ctx context.Context, agent, sessionID string) (MutationResult, error) {
	session, err := a.beginWrite(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer session.Close()

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
		return MutationResult{Revision: revision, SyncState: "local_only"}, nil
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
	commit, err := git.Commit(ctx, session.repoPath, commitMessage("capture", agent, sessionID, txID))
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
	return MutationResult{
		TransactionID: txID,
		Revision:      revision,
		Commit:        commit,
		ChangedFiles:  paths,
		SyncState:     "local_only",
	}, nil
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
	for _, pathRecord := range selected {
		if pathRecord.Existed {
			if err := s.root.WriteAtomic(pathRecord.Path, []byte(pathRecord.Before), fsMode(pathRecord.Mode)); err != nil {
				return err
			}
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
	return nil
}

// beginWrite acquires the repository lock and refuses to start when Git is in
// an intermediate state or an unfinished transaction still needs recovery.
func (a *App) beginWrite(ctx context.Context) (*writeSession, error) {
	session, err := a.beginLocked(ctx, true, false)
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

// beginLocked validates the repository, takes the lock and refuses an active
// Git operation. It deliberately allows unfinished transactions so recovery
// can run. requireEntry is false for recovery, which must work when an
// interrupted transaction deleted the entry file. allowGitState lets the
// conflict resolver continue an operation memctl owns.
func (a *App) beginLocked(ctx context.Context, requireEntry bool, allowGitState bool) (*writeSession, error) {
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
	held, err := lock.Acquire(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	session := &writeSession{
		repoPath: repoPath,
		gitDir:   gitDir,
		store:    state.OpenStore(gitDir),
		lock:     held,
	}
	root, err := vault.OpenRoot(repoPath)
	if err != nil {
		session.Close()
		return nil, err
	}
	session.root = root

	repoState, err := git.Inspect(ctx, repoPath)
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

func planPaths(plans []plannedChange) []string {
	paths := make([]string, 0, len(plans))
	for _, plan := range plans {
		paths = append(paths, plan.path)
	}
	return paths
}

func commitMessage(operation, agent, sessionID, txID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "memctl %s\n\n", operation)
	fmt.Fprintf(&b, "%s%s\n", transactionTrailer, txID)
	if agent != "" {
		fmt.Fprintf(&b, "Agent: %s\n", agent)
	}
	if sessionID != "" {
		fmt.Fprintf(&b, "Session: %s\n", sessionID)
	}
	return b.String()
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
// each file revision. It is a comparison baseline, not an authorization token.
func (a *App) repoRevision(ctx context.Context, session *writeSession) (string, error) {
	head := ""
	if value, err := git.Head(ctx, session.repoPath); err == nil {
		head = value
	}
	paths, err := session.root.ListManaged()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	writeHashField(hash, []byte(head))
	writeHashField(hash, []byte(session.repoPath))
	for _, rel := range paths {
		meta, err := session.root.Stat(rel)
		if err != nil {
			return "", err
		}
		writeHashField(hash, []byte(rel))
		writeHashField(hash, []byte(meta.Mode))
		writeHashField(hash, []byte(meta.Revision))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writeHashField(w interface{ Write([]byte) (int, error) }, value []byte) {
	var prefix [8]byte
	binary.BigEndian.PutUint64(prefix[:], uint64(len(value)))
	_, _ = w.Write(prefix[:])
	_, _ = w.Write(value)
}
