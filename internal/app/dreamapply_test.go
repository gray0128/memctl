package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file verifies the explicit Dreaming apply of stage D-2 (T036) at the
// application layer: an ordered proposal commits through the managed
// transaction, a repeated apply returns the original commit, a moved baseline
// is refused and an interrupted apply recovers without committing twice. It
// uses a pass-through sandbox and a synthetic runner, so no kernel sandbox is
// required.

// appStubProvider answers every worker call with the same structured step.
type appStubProvider struct{ response string }

func (p appStubProvider) ID() string    { return dream.ProviderDeepSeek }
func (p appStubProvider) Model() string { return "stub-model" }

func (p appStubProvider) Complete(context.Context, dream.CompletionRequest) (string, error) {
	return p.response, nil
}

// withDreamBackend gives the application service a Dreaming backend that can
// start: a bare origin, a confirmed private remote, the managed workflow and an
// injected provider. Generation then publishes a request branch and the worker
// answers it through the same repository, exactly as the Actions job would.
func withDreamBackend(t *testing.T, service *App) {
	t.Helper()
	repo := service.cfg.RepoPath
	bare := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, filepath.Dir(bare), "init", "-q", "--bare", "-b", "main", bare)
	runGit(t, repo, "remote", "add", "origin", bare)
	runGit(t, repo, "push", "-q", "-u", "origin", "main")
	// The application service records the approval through the same init path
	// the CLI uses, so the fixture cannot drift from the real precondition.
	if _, err := service.Init(context.Background(), InitOptions{RemoteURL: bare, ConfirmPrivate: true, Interactive: false}); err != nil {
		t.Fatalf("confirm remote: %v", err)
	}
	if _, err := service.InitDreamWorkflow(context.Background(), "v0.7.0"); err != nil {
		t.Fatalf("install workflow: %v", err)
	}
	runGit(t, repo, "push", "-q", "origin", "main")
}

// previewWithStubWorker generates one proposal end to end: the application
// service publishes the request branch and a separate service over the same
// repository answers it with the deterministic provider, exactly as the Actions
// job does for a real run.
func previewWithStubWorker(t *testing.T, service *App) dream.Proposal {
	t.Helper()
	ctx := context.Background()
	started, err := service.DreamBegin(ctx, dream.ProviderDeepSeek, "")
	if err != nil {
		t.Fatalf("DreamBegin: %v", err)
	}
	// GitHub Actions checks out a throwaway clone. The worker must not run in the
	// user's work tree, or its result would change the repository between
	// generation and apply.
	repositoryPath := service.cfg.RepoPath
	remoteURL := runGit(t, repositoryPath, "remote", "get-url", "origin")
	workerDir := filepath.Join(t.TempDir(), "worker")
	runGit(t, filepath.Dir(workerDir), "clone", "-q", remoteURL, workerDir)
	workerSvc := dream.NewService(dream.Source{
		RepoPath: config.Canonical(workerDir),
		RepoID:   config.RepoID(workerDir),
		StateDir: filepath.Join(t.TempDir(), "worker-state"),
	})
	workerSvc.NewProvider = func(string) (dream.Provider, error) {
		return appStubProvider{response: dreamOrganizerBody}, nil
	}
	if _, err := workerSvc.Worker(ctx, dream.RequestRef(started.ProposalID, dream.ProviderDeepSeek), started.BaseHead); err != nil {
		t.Fatalf("worker: %v", err)
	}
	proposal, err := service.DreamRun(ctx, started.ProposalID)
	if err != nil {
		t.Fatalf("DreamRun: %v", err)
	}
	return proposal
}

// withDreamProvider injects the deterministic worker provider.
func withDreamProvider(t *testing.T, service *App, response string) {
	t.Helper()
	dreamSvc := dream.NewService(dream.Source{
		RepoPath: config.Canonical(service.cfg.RepoPath),
		RepoID:   config.RepoID(service.cfg.RepoPath),
		StateDir: service.cfg.StateDir,
	})
	dreamSvc.Timeout = 20 * time.Second
	dreamSvc.PollInterval = 20 * time.Millisecond
	dreamSvc.NewProvider = func(string) (dream.Provider, error) { return appStubProvider{response: response}, nil }
	service.SetDreamService(dreamSvc)
}

// dreamOrganizerBody is the deterministic worker response of the app tests.
const dreamOrganizerBody = `{"operations":[{"action":"update","path":"MEMORY.md","content":"# Memory\n\n## Index\n- [[topic]]\n- [[new]]\n","rationale":"index"},{"action":"create","path":"new.md","content":"# New\n- added [source: test; added: 2026-10-19]\n","rationale":"split"}],"warnings":[]}`

// TestApplyDreamCommitsAndIsIdempotentCoversD04 covers the successful apply and
// the repeated apply of D04: the second call returns the same commit and does
// not create a second commit.
func TestApplyDreamCommitsAndIsIdempotentCoversD04(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)
	if proposal.Status != dream.StatusReady || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}

	result, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	if err != nil {
		t.Fatalf("ApplyDream: %v", err)
	}
	if result.Commit == "" {
		t.Fatalf("apply returned no commit: %+v", result)
	}
	// The apply commit carries the Dreaming provenance trailers, so the audit
	// view resolves it instead of reporting an unattributed commit.
	message := runGit(t, repo, "log", "-1", "--format=%B", result.Commit)
	for _, want := range []string{"memctl dream apply", "Memctl-Transaction:", "Agent: "} {
		if !strings.Contains(message, want) {
			t.Fatalf("apply commit message %q is missing %q", message, want)
		}
	}
	if got := readTestFile(t, filepath.Join(repo, "new.md")); got != "# New\n- added [source: test; added: 2026-10-19]\n" {
		t.Fatalf("created file content = %q", got)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got == testInitialMemory {
		t.Fatalf("MEMORY.md was not updated")
	}
	applied, err := service.DreamProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatalf("DreamProposal: %v", err)
	}
	if applied.Status != dream.StatusApplied || applied.AppliedCommit != result.Commit {
		t.Fatalf("applied proposal = %+v, want applied as %s", applied, result.Commit)
	}

	commits := commitCount(t, repo)
	again, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	if err != nil {
		t.Fatalf("second ApplyDream: %v", err)
	}
	if again.Commit != result.Commit {
		t.Fatalf("repeated apply commit = %q, want %q", again.Commit, result.Commit)
	}
	if again.SyncState == "" {
		t.Fatalf("repeated apply returned an empty syncState: %+v", again)
	}
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("repeated apply created a commit: %s -> %s", commits, got)
	}
}

// TestApplyDreamRefusesStaleBaselineCoversD02 covers the old-baseline rejection
// of D02: a repository that moved after generation refuses the apply, marks the
// proposal stale and leaves every proposed path untouched.
func TestApplyDreamRefusesStaleBaselineCoversD02(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)
	// Move the repository with an unrelated managed commit.
	if _, err := service.Apply(ctx, MutationRequest{
		BaseRevision: proposal.BaseRevision,
		Changes: []FileChange{{
			Path:             "other.md",
			ExpectedRevision: vault.AbsentRevision,
			Action:           ActionPut,
			RawContent:       "# Other\n- fact\n",
		}},
	}); err != nil {
		t.Fatalf("unrelated apply: %v", err)
	}

	before := readTestFile(t, filepath.Join(repo, "MEMORY.md"))
	commits := commitCount(t, repo)
	_, applyErr := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	requireContractCode(t, applyErr, memerr.ProposalStale)

	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != before {
		t.Fatalf("a stale apply modified MEMORY.md")
	}
	if _, statErr := os.Stat(filepath.Join(repo, "new.md")); !os.IsNotExist(statErr) {
		t.Fatalf("a stale apply created new.md (stat err=%v)", statErr)
	}
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("a stale apply created a commit: %s -> %s", commits, got)
	}
	stored, err := service.DreamProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatalf("DreamProposal: %v", err)
	}
	if stored.Status != dream.StatusStale {
		t.Fatalf("proposal status = %q, want stale", stored.Status)
	}
}

// TestApplyDreamRecoversInterruptedApplyCoversD04 covers the interrupted-apply
// half of D04: a crash after the local commit but before the proposal status is
// written must not commit the proposal twice.
func TestApplyDreamRecoversInterruptedApplyCoversD04(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)

	service.testFault = func(stage string) error {
		if stage == "after-commit" {
			return errSimulatedCrash
		}
		return nil
	}
	if _, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash); err == nil {
		t.Fatal("expected the injected interruption to fail the apply")
	}
	service.testFault = nil

	commits := commitCount(t, repo)
	result, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	if err != nil {
		t.Fatalf("retry ApplyDream: %v", err)
	}
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("retry created a second commit: %s -> %s", commits, got)
	}
	head := headHash(t, repo)
	if result.Commit != head {
		t.Fatalf("retry commit = %q, want the existing HEAD %q", result.Commit, head)
	}
	stored, err := service.DreamProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatalf("DreamProposal: %v", err)
	}
	if stored.Status != dream.StatusApplied || stored.AppliedCommit != head {
		t.Fatalf("reconciled proposal = %+v, want applied as %s", stored, head)
	}
}

// TestApplyDreamRefusesToOverwriteManualEdit covers the W06 rule for an
// interrupted apply: a file the user edited by hand after the interruption is
// never overwritten, and the retry stops for manual recovery.
func TestApplyDreamRefusesToOverwriteManualEdit(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)

	service.testFault = func(stage string) error {
		if stage == "after-write:0" {
			return errSimulatedCrash
		}
		return nil
	}
	if _, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash); err == nil {
		t.Fatal("expected the injected interruption to fail the apply")
	}
	service.testFault = nil

	// The user edits the partially written file before retrying.
	handEdit := "# Memory\n\n## Index\n- [[topic]]\n- hand edit\n"
	writeTestFile(t, filepath.Join(repo, "MEMORY.md"), handEdit, 0o644)
	commits := commitCount(t, repo)

	_, applyErr := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	requireContractCode(t, applyErr, memerr.RecoveryRequired)
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != handEdit {
		t.Fatalf("the manual edit was overwritten: %q", got)
	}
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("a refused recovery created a commit: %s -> %s", commits, got)
	}
}

// TestApplyDreamRejectsPatchHashMismatch verifies that a proposal can only be
// applied under the exact patch hash it was reviewed with.
func TestApplyDreamRejectsPatchHashMismatch(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)
	commits := commitCount(t, repo)
	_, applyErr := service.ApplyDream(ctx, proposal.ProposalID, "sha256:not-the-hash")
	requireContractCode(t, applyErr, memerr.InvalidArgument)
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("a rejected apply created a commit: %s -> %s", commits, got)
	}
	stored, err := service.DreamProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatalf("DreamProposal: %v", err)
	}
	if stored.Status != dream.StatusReady {
		t.Fatalf("proposal status = %q, want ready after a rejected hash", stored.Status)
	}
}

// TestApplyDreamRefusesDirtyTreeWithoutStaling covers that a dirty worktree is
// a retryable precondition, not a moved baseline: the reviewed proposal must
// stay applyable instead of being turned into a terminal stale record.
func TestApplyDreamRefusesDirtyTreeWithoutStaling(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)
	// An untracked file makes the worktree dirty without moving the revision
	// digest a clean checkout would report.
	writeTestFile(t, filepath.Join(repo, "untracked.md"), "# untracked\n", 0o644)

	commits := commitCount(t, repo)
	_, applyErr := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	requireContractCode(t, applyErr, memerr.ExternalChanges)
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("a dirty-tree rejection created a commit: %s -> %s", commits, got)
	}
	stored, err := service.DreamProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatalf("DreamProposal: %v", err)
	}
	if stored.Status != dream.StatusReady {
		t.Fatalf("proposal status = %q, want ready after a dirty-tree rejection", stored.Status)
	}
}

// TestRecoverRollbackRecordsAppliedDream covers the W05 window for an explicit
// apply: the commit exists but the proposal status was not written, and the
// documented recover entry must record the applied status instead of deleting
// the only proof and leaving the proposal stale.
func TestRecoverRollbackRecordsAppliedDream(t *testing.T) {
	service, repo := newTestService(t)
	withDreamBackend(t, service)
	ctx := context.Background()

	proposal := previewWithStubWorker(t, service)

	service.testFault = func(stage string) error {
		if stage == "after-commit" {
			return errSimulatedCrash
		}
		return nil
	}
	if _, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash); err == nil {
		t.Fatal("expected the injected interruption to fail the apply")
	}
	service.testFault = nil
	head := headHash(t, repo)

	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("RecoverRollback: %v", err)
	}
	stored, err := service.DreamProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatalf("DreamProposal: %v", err)
	}
	if stored.Status != dream.StatusApplied || stored.AppliedCommit != head {
		t.Fatalf("proposal after recovery = %+v, want applied as %s", stored, head)
	}

	commits := commitCount(t, repo)
	again, err := service.ApplyDream(ctx, proposal.ProposalID, proposal.PatchHash)
	if err != nil {
		t.Fatalf("repeated ApplyDream: %v", err)
	}
	if again.Commit != head {
		t.Fatalf("repeated apply commit = %q, want %q", again.Commit, head)
	}
	if got := commitCount(t, repo); got != commits {
		t.Fatalf("repeated apply created a commit: %s -> %s", commits, got)
	}
}
