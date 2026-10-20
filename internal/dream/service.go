package dream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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

// Service runs the Dreaming pipeline against one repository and one state root.
// It is safe for concurrent use.
//
// The local side never calls a model: it fixes the baseline, publishes the
// request branch, waits for the GitHub Actions worker result and revalidates
// that result as untrusted input. See
// docs/11-github-actions-dreaming.md section 2.1.
type Service struct {
	Source Source
	// Interactive lets the credential prompts of Git and SSH reach the
	// terminal. The CLI sets it; background callers leave it off.
	Interactive bool
	// Timeout bounds the wait for one worker result.
	Timeout time.Duration
	// PollInterval is the result poll interval.
	PollInterval time.Duration
	// KeepRemoteRefs leaves the request and result branches on the remote. It
	// exists for transport tests; production always cleans them up.
	KeepRemoteRefs bool
	// WorkflowFile is the managed workflow path inside the repository.
	WorkflowFile string
	// SkipWorkflowRequirement disables the workflow-file precondition. Tests of
	// the transport use it because they never run the actual workflow.
	SkipWorkflowRequirement bool
	// Version is the running memctl version. The managed workflow pins the
	// release it downloads, so a workflow written by another version would run a
	// different binary than the one validating its result. A mismatch is
	// reported as a readiness failure instead of silently drifting.
	Version string
	// CommitSource records the caller provenance in the commits this service
	// creates, currently the managed workflow install. The audit view reads it
	// back from the commit trailers, so the entry point that knows the invoking
	// device sets it.
	CommitSource git.Source
	// Now is the injectable clock.
	Now func() time.Time
	// NewProvider builds the worker's model transport. It defaults to the
	// environment-driven provider registry and is replaced in tests so a
	// deterministic run never reaches a model endpoint. The argument is the
	// provider named by the request branch, which the worker uses when no
	// override is configured.
	NewProvider func(providerID string) (Provider, error)
	// SuspiciousMatcher reports whether a text carries a prompt-injection
	// pattern. ProposeLocal screens every candidate fact through it; the CLI
	// installs the same doctor phrase list the corpus collector uses.
	SuspiciousMatcher func(string) bool
	// SessionWroteMemory reports whether a session already produced a commit
	// carrying its trailer. The local prepare path records the answer on the
	// bundle so the current agent can weigh an already-mined session lower. A
	// nil callback records false.
	SessionWroteMemory func(sid string) bool

	store      *Store
	instanceID string
	mu         sync.Mutex
	pending    map[string]*pendingRun
	// statusMu serializes the read-modify-write of one proposal status so two
	// in-process applies cannot lose a terminal transition. It is never taken
	// while holding mu.
	statusMu sync.Mutex
}

// NewService binds a Dreaming service to one repository.
func NewService(source Source) *Service {
	instanceID, err := newID("inst")
	if err != nil {
		instanceID = "inst-" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return &Service{
		Source:     source,
		Timeout:    DefaultTimeout,
		Now:        time.Now,
		store:      OpenStore(source.StateDir, source.RepoID),
		instanceID: instanceID,
		pending:    make(map[string]*pendingRun),
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

// Begin validates the preconditions, fixes the baseline, pushes the request
// branch and persists a generating proposal. The worker result is awaited by
// Run.
func (s *Service) Begin(ctx context.Context, req PreviewRequest) (Proposal, error) {
	if !KnownProvider(req.Runner) {
		return Proposal{}, unknownProvider(req.Runner)
	}
	base, head, revision, err := s.fixBaseline(ctx, req.BaseRevision)
	if err != nil {
		return Proposal{}, err
	}
	id, err := newID("dp")
	if err != nil {
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
		InstanceID: s.instanceID,
	}
	if err := s.store.Save(value); err != nil {
		return Proposal{}, err
	}
	if err := s.pushRequest(ctx, id, req.Runner, head); err != nil {
		value.Status = StatusFailed
		value.Error = errorMessage(err)
		if saveErr := s.store.Save(value); saveErr != nil {
			return value.Proposal, saveErr
		}
		s.cleanupRefs(ctx, id, req.Runner)
		return value.Proposal, err
	}

	s.mu.Lock()
	s.pending[id] = &pendingRun{record: value, baseline: base}
	s.mu.Unlock()
	return value.Proposal, nil
}

// Run waits for the worker result, validates the untrusted result tree against
// the fixed baseline and persists the terminal ready or failed record. It never
// touches the main repository.
func (s *Service) Run(ctx context.Context, proposalID string) (Proposal, error) {
	s.mu.Lock()
	pending := s.pending[proposalID]
	s.mu.Unlock()
	if pending == nil {
		// Another process owns the run, or it was never started here. Load
		// reconciles an orphaned generating record to failed.
		return s.Load(ctx, proposalID)
	}

	runCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	result, err := s.pollLoop(runCtx, proposalID)
	if err != nil {
		proposal, failErr := s.fail(ctx, pending, err)
		s.cleanupRefs(context.Background(), proposalID, pending.record.Runner)
		return proposal, failErr
	}
	// The request and result branches are consumed as soon as the result is
	// fetched, whatever its status.
	s.cleanupRefs(context.Background(), proposalID, pending.record.Runner)

	if result.Meta.Status != WorkerStatusReady {
		cause := memerr.New(memerr.Unsupported,
			"the GitHub Actions worker reported failure: %s", workerErrorText(result.Meta))
		return s.fail(ctx, pending, cause)
	}
	if result.Meta.ProposalID != proposalID || result.Meta.BaseHead != pending.record.BaseHead {
		return s.fail(ctx, pending, memerr.New(memerr.Unsupported,
			"the Dreaming result belongs to another proposal or baseline"))
	}

	changes, diff, warnings, err := buildChanges(pending.baseline, result.Files)
	if err != nil {
		return s.fail(ctx, pending, err)
	}

	value := pending.record
	value.Status = StatusReady
	value.Changes = changes
	value.Diff = diff
	value.PatchHash = patchHash(changes)
	value.Planned = plannedFiles(pending.baseline, result.Files, changes)
	value.Warnings = append(value.Warnings, result.Meta.Warnings...)
	value.Warnings = append(value.Warnings, warnings...)
	if err := s.complete(value); err != nil {
		return value.Proposal, err
	}
	return value.Proposal, nil
}

// List returns the persisted proposals in createdAt order descending. The
// internal apply input and the recorded corpus digests are not part of the
// returned records.
func (s *Service) List(limit int) []Proposal {
	return s.store.List(limit)
}

// Load returns one stored proposal. A generating record that no longer has an
// in-process run is reconciled to failed, so an interrupted generation is never
// presented as one that can continue.
func (s *Service) Load(ctx context.Context, proposalID string) (Proposal, error) {
	value, err := s.loadRecord(ctx, proposalID)
	return value.Proposal, err
}

// loadRecord is the record-level read behind Load.
func (s *Service) loadRecord(_ context.Context, proposalID string) (record, error) {
	value, err := s.store.Load(proposalID)
	if err != nil {
		return record{}, err
	}
	if value.Status != StatusGenerating {
		return value, nil
	}
	// The pending map and the terminal write are serialized by s.mu, so a
	// generation that completes between the read above and the check below is
	// observed as its terminal record instead of being clobbered with failed.
	s.mu.Lock()
	defer s.mu.Unlock()
	_, active := s.pending[proposalID]
	if active && value.InstanceID == s.instanceID {
		return value, nil
	}
	if fresh, err := s.store.Load(proposalID); err == nil && fresh.Status != StatusGenerating {
		return fresh, nil
	}
	value.Status = StatusFailed
	value.Error = "generation was interrupted before it completed"
	if err := s.store.Save(value); err != nil {
		return value, err
	}
	return value, nil
}

// fail persists a failed proposal.
func (s *Service) fail(_ context.Context, pending *pendingRun, cause error) (Proposal, error) {
	value := pending.record
	value.Status = StatusFailed
	value.Error = errorMessage(cause)
	if err := s.complete(value); err != nil {
		return value.Proposal, err
	}
	return value.Proposal, cause
}

// complete persists a terminal record and drops the in-memory run.
func (s *Service) complete(value record) error {
	s.mu.Lock()
	delete(s.pending, value.ProposalID)
	err := s.store.Save(value)
	s.mu.Unlock()
	return err
}

// BackendReady reports whether Dreaming can start on this repository right now:
// the repository must be a memory repository with a confirmed private remote
// and the managed workflow file. The UI shows this before a request is made;
// Begin re-checks every condition.
func (s *Service) BackendReady(ctx context.Context) bool {
	_, status := s.backendStatus(ctx)
	return status == ""
}

// BackendStatus returns the stable readiness code and the human-readable reason
// why generation is unavailable. An empty code means Dreaming is ready.
func (s *Service) BackendStatus(ctx context.Context) (string, string) {
	return s.backendStatus(ctx)
}

// BackendReadyCodes are the stable codes reported by BackendStatus.
const (
	BackendNotReadyNoRepo        = "NO_REPO"
	BackendNotReadyNoRemote      = "NO_REMOTE"
	BackendNotReadyUnapproved    = "REMOTE_NOT_APPROVED"
	BackendNotReadyNoWorkflow    = "WORKFLOW_MISSING"
	BackendNotReadyWorkflowStale = "WORKFLOW_STALE"
	BackendNotReadyInspectFailed = "INSPECT_FAILED"
)

// backendStatus is the single readiness implementation.
func (s *Service) backendStatus(ctx context.Context) (string, string) {
	if err := git.Available(); err != nil {
		return BackendNotReadyInspectFailed, "Git is not available"
	}
	if _, err := os.Lstat(filepath.Join(s.Source.RepoPath, vault.EntryPointName)); err != nil {
		return BackendNotReadyNoRepo, s.Source.RepoPath + " is not a memory repository"
	}
	remote, ok, err := git.DefaultRemote(ctx, s.Source.RepoPath)
	if err != nil {
		return BackendNotReadyInspectFailed, "cannot inspect the configured remotes"
	}
	if !ok {
		return BackendNotReadyNoRemote,
			"the repository has no remote; Dreaming runs on GitHub Actions, so configure a private GitHub remote"
	}
	urls, err := git.RemoteURLs(ctx, s.Source.RepoPath, remote)
	if err != nil {
		return BackendNotReadyInspectFailed, "cannot read the remote URLs"
	}
	gitDir, err := git.GitDir(ctx, s.Source.RepoPath)
	if err != nil {
		return BackendNotReadyInspectFailed, "cannot resolve the Git directory"
	}
	approval, ok, err := state.OpenStore(gitDir).LoadApproval()
	if err != nil {
		return BackendNotReadyInspectFailed, "cannot read the remote approval record"
	}
	if !ok || !sameURLSet(approval.URLs, urls) {
		return BackendNotReadyUnapproved,
			"the remote is not confirmed as private and owned by you; run `memctl init <url> --confirm-private`"
	}
	if !s.SkipWorkflowRequirement {
		workflow := s.workflowPath()
		path := filepath.Join(s.Source.RepoPath, filepath.FromSlash(workflow))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return BackendNotReadyNoWorkflow,
				"the repository has no " + workflow + "; run `memctl dream --init-workflow`"
		}
		if code, reason := s.workflowVersionStatus(path, workflow); code != "" {
			return code, reason
		}
	}
	return "", ""
}

// workflowVersionStatus reports whether the installed workflow pins the release
// this binary would run. A workflow from another memctl version downloads a
// different worker, so its Step Guard and validation rules no longer match the
// local side; that is a readiness failure the operator can fix with one command.
// A development build has no release artifact and keeps the placeholder, so it
// is exempt.
func (s *Service) workflowVersionStatus(path, workflow string) (string, string) {
	if !pinnedVersionRelevant(s.Version) {
		return "", ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return BackendNotReadyInspectFailed, "cannot read " + workflow
	}
	pinned := workflowPinnedVersion(string(content))
	if pinned == "" {
		return BackendNotReadyWorkflowStale,
			workflow + " does not pin MEMCTL_DREAM_VERSION; run `memctl dream --init-workflow`"
	}
	if pinned != s.Version {
		return BackendNotReadyWorkflowStale,
			workflow + " pins " + pinned + " but this memctl is " + s.Version +
				"; run `memctl dream --init-workflow` to refresh it"
	}
	return "", ""
}

// workflowPinnedVersion extracts the MEMCTL_DREAM_VERSION value from the
// workflow text. An unrendered placeholder counts as unpinned.
func workflowPinnedVersion(content string) string {
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || strings.TrimSpace(key) != "MEMCTL_DREAM_VERSION" {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.Contains(value, workflowVersionPlaceholder) {
			return ""
		}
		return value
	}
	return ""
}

// pinnedVersionRelevant reports whether the running binary can be compared with
// the workflow pin. A development build downloads no release artifact.
func pinnedVersionRelevant(version string) bool {
	version = strings.TrimSpace(version)
	return version != "" && version != devVersion
}

// devVersion is the version string of a build that is not a release.
const devVersion = "v2.0.0-dev"

func (s *Service) workflowPath() string {
	if s.WorkflowFile != "" {
		return s.WorkflowFile
	}
	return WorkflowRelativePath
}

func sameURLSet(got, want []string) bool {
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

// providerForWorker resolves the worker provider through the injection point.
func (s *Service) providerForWorker(providerID string) (Provider, error) {
	if s.NewProvider != nil {
		return s.NewProvider(providerID)
	}
	return providerFromEnv(os.Getenv, providerID, nil)
}

// timeout is the effective worker wait budget.
func (s *Service) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return DefaultTimeout
}

func (s *Service) timeNow() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) now() string {
	return s.timeNow().UTC().Format(time.RFC3339)
}

// fixBaseline acquires the repository lock, refuses a dirty or protected
// repository, checks the Dreaming preconditions and exports the supported files
// into an in-memory baseline. See docs/03-workflows-and-dataflows.md section 6
// step 1 and docs/11-github-actions-dreaming.md section 2.1 step 1.
func (s *Service) fixBaseline(ctx context.Context, expectedRevision string) (base *baseline, head, revision string, err error) {
	unlock, ctx, err := s.lockCleanRepository(ctx, "generating a proposal", true)
	if err != nil {
		return nil, "", "", err
	}
	defer unlock()

	if code, reason := s.backendStatus(ctx); code != "" {
		if code == BackendNotReadyUnapproved {
			return nil, "", "", memerr.New(memerr.RemoteNotApproved, "%s", reason)
		}
		if code == BackendNotReadyNoRemote || code == BackendNotReadyNoWorkflow || code == BackendNotReadyWorkflowStale {
			return nil, "", "", memerr.New(memerr.Unsupported, "%s", reason)
		}
		return nil, "", "", memerr.New(memerr.NoRepo, "%s", reason)
	}

	root, err := vault.OpenRoot(s.Source.RepoPath)
	if err != nil {
		return nil, "", "", err
	}
	defer root.Close()
	if head, err = git.Head(ctx, s.Source.RepoPath); err != nil {
		return nil, "", "", err
	}
	revision, err = vault.RepoRevision(root, s.Source.RepoPath, head)
	if err != nil {
		return nil, "", "", err
	}
	if expectedRevision != "" && expectedRevision != revision {
		return nil, "", "", memerr.New(memerr.RevisionMismatch,
			"the repository revision changed since it was read; reread the snapshot before generating a proposal")
	}
	base, err = exportBaseline(root)
	if err != nil {
		return nil, "", "", err
	}
	base.head = head
	return base, head, revision, nil
}

// lockCleanRepository takes the repository lock and proves the documented
// preconditions every proposal path shares: the entry file exists, no conflict
// or unfinished transaction is pending, no Git operation is active and the work
// tree is clean. The returned context carries the inherited lock descriptor, and
// the caller must release the lock through the returned function.
//
// The two proposal paths differ only in what they check next: the Actions path
// additionally needs a backend ready for a remote run, while the local path never
// asks for it.
func (s *Service) lockCleanRepository(ctx context.Context, action string, requireEntry bool) (func(), context.Context, error) {
	if err := git.Available(); err != nil {
		return nil, ctx, err
	}
	gitDir, err := git.GitDir(ctx, s.Source.RepoPath)
	if err != nil {
		return nil, ctx, err
	}
	held, err := lock.Acquire(ctx, gitDir)
	if err != nil {
		return nil, ctx, err
	}
	unlock := func() { _ = held.Release() }
	ctx = git.WithInheritedFile(ctx, held.File())
	if requireEntry {
		if _, err := os.Lstat(filepath.Join(s.Source.RepoPath, vault.EntryPointName)); err != nil {
			unlock()
			return nil, ctx, memerr.New(memerr.NoRepo,
				"%s has no %s; run memctl init", s.Source.RepoPath, vault.EntryPointName)
		}
	}
	store := state.OpenStore(gitDir)
	if conflict, ok, err := store.LoadConflict(); err != nil {
		unlock()
		return nil, ctx, err
	} else if ok {
		unlock()
		return nil, ctx, memerr.New(memerr.OperationActive,
			"conflict %s is awaiting resolution; resolve it before %s", conflict.ConflictID, action)
	}
	if pending, err := store.Pending(); err != nil {
		unlock()
		return nil, ctx, err
	} else if len(pending) > 0 {
		unlock()
		return nil, ctx, memerr.New(memerr.RecoveryRequired,
			"transaction %s is unfinished; run `memctl recover --status` before %s", pending[0].ID, action)
	}
	repoState, err := git.Inspect(ctx, s.Source.RepoPath)
	if err != nil {
		unlock()
		return nil, ctx, err
	}
	switch {
	case repoState.IndexLocked:
		unlock()
		return nil, ctx, memerr.New(memerr.Busy, "another Git process is writing the index (.git/index.lock exists)")
	case repoState.Active():
		unlock()
		return nil, ctx, memerr.New(memerr.OperationActive,
			"a Git operation or unmerged index is active; Dreaming requires a clean repository")
	}
	status, err := git.StatusPorcelain(ctx, s.Source.RepoPath)
	if err != nil {
		unlock()
		return nil, ctx, err
	}
	if status != "" {
		unlock()
		return nil, ctx, memerr.New(memerr.ExternalChanges,
			"the repository has staged, unstaged or untracked changes; commit or capture them before %s", action)
	}
	return unlock, ctx, nil
}

// unknownProvider is the shared unknown-provider rejection.
func unknownProvider(name string) error {
	if name == "" {
		return memerr.New(memerr.InvalidArgument, "a model provider is required")
	}
	return memerr.New(memerr.Unsupported,
		"provider %q is not built in; supported providers are %s", name, strings.Join(ProviderIDs, ", "))
}

// errorMessage bounds one persisted failure description and strips credentials.
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	message := sanitizeProviderDetail(err.Error())
	const max = 2048
	if len(message) > max {
		message = message[:max] + "..."
	}
	return message
}

// workerErrorText renders the worker's failure reason for a local proposal.
func workerErrorText(meta WorkerMeta) string {
	if strings.TrimSpace(meta.Error) == "" {
		return "the workflow did not report a reason; check the Actions run"
	}
	return meta.Error
}

// plannedFiles captures the exact apply input of every create or update change.
// Delete changes need no content. See docs/03-workflows-and-dataflows.md
// section 6 step 5.
func plannedFiles(base *baseline, work map[string]workFile, changes []Change) map[string]plannedFile {
	planned := make(map[string]plannedFile, len(changes))
	for _, change := range changes {
		if change.Action == ChangeDelete {
			continue
		}
		file, ok := work[change.Path]
		if !ok {
			continue
		}
		entry := plannedFile{
			Content: string(file.content),
			// The managed apply creates files as 100644 and preserves the
			// baseline mode otherwise, matching buildChanges' normalization.
			GitMode: "100644",
		}
		if old, ok := base.files[change.Path]; ok {
			entry.GitMode = old.gitMode
			entry.BaseGitMode = old.gitMode
		}
		planned[change.Path] = entry
	}
	return planned
}

// newID returns a random identifier with the given prefix.
func newID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot generate an identifier")
	}
	return prefix + "-" + hex.EncodeToString(raw[:]), nil
}
