package dream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/vault"
)

// pendingRun is the in-memory state of one generation that began but has not
// finished. The fixed baseline is held here, not persisted, so memory content
// never lands in the state root outside the proposal review record.
type pendingRun struct {
	record   record
	baseline *baseline
}

// Service runs the isolated Dreaming pipeline against one repository and one
// state root. It is safe for concurrent use.
type Service struct {
	Source         Source
	Runners        map[string]Runner
	Sandbox        Sandbox
	Timeout        time.Duration
	MaxOutputBytes int
	Now            func() time.Time

	store       *Store
	instanceID  string
	keepWorkDir bool
	mu          sync.Mutex
	pending     map[string]*pendingRun
}

// NewService binds a Dreaming service to one repository.
func NewService(source Source) *Service {
	instanceID, err := newID("inst")
	if err != nil {
		instanceID = "inst-" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return &Service{
		Source:         source,
		Runners:        defaultRunners(),
		Sandbox:        platformSandbox(),
		Timeout:        DefaultTimeout,
		MaxOutputBytes: MaxRunnerOutput,
		Now:            time.Now,
		store:          OpenStore(source.StateDir, source.RepoID),
		instanceID:     instanceID,
		pending:        make(map[string]*pendingRun),
	}
}

// Preview runs one complete generation and returns the finished proposal. A
// failed generation is persisted with status failed and also returned as an
// error.
func (s *Service) Preview(ctx context.Context, req PreviewRequest) (Proposal, error) {
	started, err := s.Begin(ctx, req)
	if err != nil {
		return Proposal{}, err
	}
	return s.Run(ctx, started.ProposalID)
}

// Begin validates the preconditions, exports the supported memory files into an
// independent temporary directory and persists a generating proposal. It
// releases the repository lock before returning; the caller must call Run with
// the returned proposal id. Persisting before running lets an HTTP caller
// return the proposal id immediately and poll GET /api/v1/dream/{id}.
func (s *Service) Begin(ctx context.Context, req PreviewRequest) (Proposal, error) {
	if _, _, err := s.resolveRunner(req.Runner); err != nil {
		return Proposal{}, err
	}
	if s.Sandbox == nil || !s.Sandbox.Available() {
		return Proposal{}, sandboxUnavailable()
	}

	workDir, base, head, revision, err := s.fixBaseline(ctx, req.BaseRevision)
	if err != nil {
		return Proposal{}, err
	}
	id, err := newID("dp")
	if err != nil {
		os.RemoveAll(workDir)
		return Proposal{}, err
	}
	value := record{
		Proposal: Proposal{
			ProposalID:   id,
			RepoID:       s.Source.RepoID,
			BaseRevision: revision,
			BaseHead:     head,
			Runner:       req.Runner,
			Status:       StatusGenerating,
			Warnings:     append([]string{}, base.warnings...),
			CreatedAt:    s.now(),
		},
		WorkDir:    workDir,
		InstanceID: s.instanceID,
	}
	if err := s.store.Save(value); err != nil {
		os.RemoveAll(workDir)
		return Proposal{}, err
	}
	s.mu.Lock()
	s.pending[id] = &pendingRun{record: value, baseline: base}
	s.mu.Unlock()
	return value.Proposal, nil
}

// Run executes the runner for a previously started proposal, validates the
// change set and persists the terminal ready or failed record. It is the only
// step that runs the runner, and it never touches the main repository.
func (s *Service) Run(ctx context.Context, proposalID string) (Proposal, error) {
	s.mu.Lock()
	pending := s.pending[proposalID]
	s.mu.Unlock()
	if pending == nil {
		// Another process owns the run, or it was never started here. Load
		// reconciles an orphaned generating record to failed.
		return s.Load(ctx, proposalID)
	}

	definition, execPath, err := s.resolveRunner(pending.record.Runner)
	if err != nil {
		return s.fail(pending, err)
	}
	out, runErr := s.runRunner(ctx, pending.record.WorkDir, execPath, definition.BuildArgs(prompt))
	if runErr != nil {
		return s.fail(pending, runErr)
	}

	work, err := scanWorkTree(pending.record.WorkDir)
	if err != nil {
		return s.fail(pending, err)
	}
	changes, diff, warnings, err := buildChanges(pending.baseline, work)
	if err != nil {
		return s.fail(pending, err)
	}

	value := pending.record
	value.Status = StatusReady
	value.Changes = changes
	value.Diff = diff
	value.PatchHash = patchHash(changes)
	value.Warnings = append(value.Warnings, warnings...)
	if out.truncated {
		value.Warnings = append(value.Warnings, "the runner output was truncated at the configured byte limit")
	}
	if err := s.complete(value); err != nil {
		return value.Proposal, err
	}
	return value.Proposal, nil
}

// Load returns one stored proposal. A generating record that no longer has an
// in-process run is reconciled to failed, so an interrupted generation is never
// presented as one that can continue.
func (s *Service) Load(_ context.Context, proposalID string) (Proposal, error) {
	value, err := s.store.Load(proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if value.Status == StatusGenerating {
		s.mu.Lock()
		_, active := s.pending[proposalID]
		s.mu.Unlock()
		if !active || value.InstanceID != s.instanceID {
			value.Status = StatusFailed
			value.Error = "generation was interrupted before it completed"
			if err := s.store.Save(value); err != nil {
				return value.Proposal, err
			}
			s.removeWorkDir(value.WorkDir)
		}
	}
	return value.Proposal, nil
}

// fail persists a failed proposal and removes its temporary execution
// environment.
func (s *Service) fail(pending *pendingRun, cause error) (Proposal, error) {
	value := pending.record
	value.Status = StatusFailed
	value.Error = errorMessage(cause)
	if err := s.complete(value); err != nil {
		return value.Proposal, err
	}
	return value.Proposal, cause
}

// complete persists a terminal record, drops the in-memory run and removes the
// temporary execution environment.
func (s *Service) complete(value record) error {
	s.mu.Lock()
	delete(s.pending, value.ProposalID)
	s.mu.Unlock()
	if err := s.store.Save(value); err != nil {
		return err
	}
	s.removeWorkDir(value.WorkDir)
	return nil
}

func (s *Service) removeWorkDir(workDir string) {
	if workDir == "" || s.keepWorkDir {
		return
	}
	_ = os.RemoveAll(workDir)
}

func (s *Service) now() string {
	if s.Now != nil {
		return s.Now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// fixBaseline acquires the repository lock, refuses a dirty or protected
// repository, fixes the baseline and exports the supported files. The lock is
// released before the runner starts. See
// docs/03-workflows-and-dataflows.md section 6 step 1.
func (s *Service) fixBaseline(ctx context.Context, expectedRevision string) (workDir string, base *baseline, head, revision string, err error) {
	if err := git.Available(); err != nil {
		return "", nil, "", "", err
	}
	gitDir, err := git.GitDir(ctx, s.Source.RepoPath)
	if err != nil {
		return "", nil, "", "", err
	}
	held, err := lock.Acquire(ctx, gitDir)
	if err != nil {
		return "", nil, "", "", err
	}
	defer held.Release()
	ctx = git.WithInheritedFile(ctx, held.File())

	store := state.OpenStore(gitDir)
	if conflict, ok, err := store.LoadConflict(); err != nil {
		return "", nil, "", "", err
	} else if ok {
		return "", nil, "", "", memerr.New(memerr.OperationActive,
			"conflict %s is awaiting resolution; resolve it before generating a proposal", conflict.ConflictID)
	}
	if pending, err := store.Pending(); err != nil {
		return "", nil, "", "", err
	} else if len(pending) > 0 {
		return "", nil, "", "", memerr.New(memerr.RecoveryRequired,
			"transaction %s is unfinished; run `memctl recover --status` before generating a proposal", pending[0].ID)
	}
	repoState, err := git.Inspect(ctx, s.Source.RepoPath)
	if err != nil {
		return "", nil, "", "", err
	}
	switch {
	case repoState.IndexLocked:
		return "", nil, "", "", memerr.New(memerr.Busy, "another Git process is writing the index (.git/index.lock exists)")
	case repoState.Active():
		return "", nil, "", "", memerr.New(memerr.OperationActive,
			"a Git operation or unmerged index is active; Dreaming requires a clean repository")
	}
	status, err := git.StatusPorcelain(ctx, s.Source.RepoPath)
	if err != nil {
		return "", nil, "", "", err
	}
	if status != "" {
		return "", nil, "", "", memerr.New(memerr.ExternalChanges,
			"the repository has staged, unstaged or untracked changes; commit or capture them before generating a proposal")
	}

	root, err := vault.OpenRoot(s.Source.RepoPath)
	if err != nil {
		return "", nil, "", "", err
	}
	defer root.Close()
	if head, err = git.Head(ctx, s.Source.RepoPath); err != nil {
		return "", nil, "", "", err
	}
	revision, err = vault.RepoRevision(root, s.Source.RepoPath, head)
	if err != nil {
		return "", nil, "", "", err
	}
	if expectedRevision != "" && expectedRevision != revision {
		return "", nil, "", "", memerr.New(memerr.RevisionMismatch,
			"the repository revision changed since it was read; reread the snapshot before generating a proposal")
	}

	workDir, err = os.MkdirTemp("", "memctl-dream-")
	if err != nil {
		return "", nil, "", "", memerr.Wrap(memerr.IOFailed, err, "cannot create the isolated Dreaming work directory")
	}
	base, err = exportBaseline(root, workDir)
	if err != nil {
		os.RemoveAll(workDir)
		return "", nil, "", "", err
	}
	base.head = head
	return workDir, base, head, revision, nil
}

// sandboxUnavailable is the shared missing-sandbox error.
func sandboxUnavailable() error {
	return memerr.New(memerr.Unsupported,
		"no verified runner isolation backend is available on this platform; Dreaming generation is disabled")
}

// errorMessage bounds one persisted failure description.
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	const max = 2048
	if len(message) > max {
		message = message[:max] + "..."
	}
	return message
}

// newID returns a random identifier with the given prefix. A proposal id
// matches the store's path-safe pattern; the instance id is only compared in
// memory and on disk.
func newID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot generate an identifier")
	}
	return prefix + "-" + hex.EncodeToString(raw[:]), nil
}
