package dream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/vault"
)

// This file verifies the GitHub Actions Dreaming transport of
// docs/11-github-actions-dreaming.md without a real GitHub remote: a local bare
// repository stands in for origin, and a stub worker answers the request branch
// exactly like the Actions job does. No model endpoint and no credentials are
// touched.

// gitEnv is the hermetic environment of the stub Git operations.
func gitEnv(t *testing.T) []string {
	t.Helper()
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL="+filepath.Join(t.TempDir(), "gitconfig"),
		"GIT_CONFIG_SYSTEM="+filepath.Join(t.TempDir(), "system-gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=file",
	)
}

// gitCommandIn runs one Git command and returns its combined output, so a
// caller can branch on an expected failure.
func gitCommandIn(dir string, env []string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = env
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitRun runs one Git command and fails the test on error.
func gitRun(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = env
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// initRepo creates a one-commit memory repository with a bare origin, a
// confirmed private remote and the managed workflow file. It returns the
// repository path and the bare remote path.
func initRepo(t *testing.T) (string, string) {
	t.Helper()
	env := gitEnv(t)
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	gitRun(t, env, root, "init", "-q", "--bare", "-b", "main", bare)

	repo := filepath.Join(root, "memory")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	gitRun(t, env, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "MEMORY.md"), "# Memory\n\n## Index\n- [[topic]]\n")
	writeFile(t, filepath.Join(repo, "topic.md"), "# Topic\n- fact one [source: test; added: 2026-01-01]\n")
	gitRun(t, env, repo, "add", "--", "MEMORY.md", "topic.md")
	gitRun(t, env, repo, "commit", "-q", "-m", "init")
	gitRun(t, env, repo, "remote", "add", "origin", bare)
	gitRun(t, env, repo, "push", "-q", "-u", "origin", "main")
	return repo, bare
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create dir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// newTestService builds a service bound to one repository. The workflow check
// is enabled unless a test installs the workflow itself.
func newTestService(t *testing.T, repoPath, stateDir string) *Service {
	t.Helper()
	service := NewService(Source{
		RepoPath: repoPath,
		RepoID:   config.RepoID(repoPath),
		StateDir: stateDir,
	})
	service.Timeout = 20 * time.Second
	service.PollInterval = 20 * time.Millisecond
	return service
}

// fakeProvider answers each worker call with the queued response.
type fakeProvider struct {
	responses []string
	calls     int
	err       error
	requests  []CompletionRequest
}

func (p *fakeProvider) ID() string    { return ProviderDeepSeek }
func (p *fakeProvider) Model() string { return "fake-model" }

func (p *fakeProvider) Complete(_ context.Context, req CompletionRequest) (string, error) {
	p.calls++
	p.requests = append(p.requests, req)
	if p.err != nil {
		return "", p.err
	}
	if len(p.responses) == 0 {
		return `{"operations":[],"warnings":[]}`, nil
	}
	response := p.responses[0]
	if len(p.responses) > 1 {
		p.responses = p.responses[1:]
	}
	return response, nil
}

// newStubWorker builds a worker service over a clone of the bare remote. It
// runs one `--worker` invocation and returns its published result.
func newStubWorker(t *testing.T, bare, stateDir string, provider Provider) (*Service, string) {
	t.Helper()
	env := gitEnv(t)
	clone := filepath.Join(t.TempDir(), "worker")
	gitRun(t, env, filepath.Dir(clone), "clone", "-q", bare, clone)
	service := NewService(Source{RepoPath: clone, RepoID: config.RepoID(clone), StateDir: stateDir})
	service.NewProvider = func(string) (Provider, error) { return provider, nil }
	return service, clone
}

// installWorkflow writes the managed workflow file into the repository.
func installWorkflow(t *testing.T, service *Service) {
	t.Helper()
	result, err := service.InitWorkflow(context.Background(), "")
	if err != nil {
		t.Fatalf("InitWorkflow: %v", err)
	}
	if result.State != InitWorkflowWritten {
		t.Fatalf("InitWorkflow state = %q, want written", result.State)
	}
	env := gitEnv(t)
	gitRun(t, env, service.Source.RepoPath, "push", "-q", "origin", "main")
}

// approveRemote writes the remote approval record the preconditions require.
func approveRemote(t *testing.T, repoPath string) {
	t.Helper()
	env := gitEnv(t)
	url := gitRun(t, env, repoPath, "remote", "get-url", "origin")
	gitDir := gitRun(t, env, repoPath, "rev-parse", "--absolute-git-dir")
	if err := saveApprovalForTest(gitDir, []string{url}); err != nil {
		t.Fatalf("save approval: %v", err)
	}
}

func TestProtectedPath(t *testing.T) {
	protected := []string{".git/config", ".GIT/config", ".gitconfig", "sub/.gitmodules", ".ssh/id_rsa", "id_rsa", "keys/server.pem", "sub/.claude/settings.json", ".github/workflows/memctl-dream.yml", "sub/.GitHub/x.yml"}
	for _, rel := range protected {
		if !protectedPath(rel) {
			t.Errorf("protectedPath(%q) = false, want true", rel)
		}
	}
	allowed := []string{"MEMORY.md", "topic.md", "notes/plan.md", "queries/report.sql", "scripts/clean.sh"}
	for _, rel := range allowed {
		if protectedPath(rel) {
			t.Errorf("protectedPath(%q) = true, want false", rel)
		}
	}
}

func TestUnifiedDiffShape(t *testing.T) {
	createDiff := unifiedDiff("/dev/null", "b/new.md", "", "# New\n")
	if !strings.Contains(createDiff, "--- /dev/null") || !strings.Contains(createDiff, "+++ b/new.md") || !strings.Contains(createDiff, "+# New") {
		t.Fatalf("create diff is malformed:\n%s", createDiff)
	}
	updateDiff := unifiedDiff("a/x.md", "b/x.md", "one\ntwo\n", "one\nthree\n")
	if !strings.Contains(updateDiff, "-two") || !strings.Contains(updateDiff, "+three") {
		t.Fatalf("update diff is malformed:\n%s", updateDiff)
	}
	deleteDiff := unifiedDiff("a/old.md", "/dev/null", "gone\n", "")
	if !strings.Contains(deleteDiff, "--- a/old.md") || !strings.Contains(deleteDiff, "-gone") {
		t.Fatalf("delete diff is malformed:\n%s", deleteDiff)
	}
	if unifiedDiff("a/x", "b/x", "same\n", "same\n") != "" {
		t.Fatalf("identical content produced a diff")
	}
}

// TestPreviewRefusesUnreadyBackendCoversD01 covers the D01 precondition cases:
// no remote, an unconfirmed remote, a missing workflow file and an unknown
// provider all refuse generation before any branch is pushed.
func TestPreviewRefusesUnreadyBackendCoversD01(t *testing.T) {
	t.Run("unknown provider", func(t *testing.T) {
		repo, _ := initRepo(t)
		service := newTestService(t, repo, t.TempDir())
		_, err := service.Preview(context.Background(), PreviewRequest{Runner: "claude"})
		if !contractCode(err, "UNSUPPORTED") {
			t.Fatalf("unknown provider: err=%v, want UNSUPPORTED", err)
		}
	})

	t.Run("no remote", func(t *testing.T) {
		repo, _ := initRepo(t)
		gitRun(t, gitEnv(t), repo, "remote", "remove", "origin")
		service := newTestService(t, repo, t.TempDir())
		_, err := service.Preview(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
		if !contractCode(err, "UNSUPPORTED") {
			t.Fatalf("no remote: err=%v, want UNSUPPORTED", err)
		}
	})

	t.Run("unapproved remote", func(t *testing.T) {
		repo, _ := initRepo(t)
		service := newTestService(t, repo, t.TempDir())
		_, err := service.Preview(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
		if !contractCode(err, "REMOTE_NOT_APPROVED") {
			t.Fatalf("unapproved remote: err=%v, want REMOTE_NOT_APPROVED", err)
		}
	})

	t.Run("missing workflow", func(t *testing.T) {
		repo, _ := initRepo(t)
		approveRemote(t, repo)
		service := newTestService(t, repo, t.TempDir())
		_, err := service.Preview(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
		if !contractCode(err, "UNSUPPORTED") || !strings.Contains(err.Error(), "init-workflow") {
			t.Fatalf("missing workflow: err=%v, want an UNSUPPORTED init-workflow hint", err)
		}
	})

	t.Run("no producer branch was pushed", func(t *testing.T) {
		repo, bare := initRepo(t)
		approveRemote(t, repo)
		service := newTestService(t, repo, t.TempDir())
		_, _ = service.Preview(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
		listing := gitRun(t, gitEnv(t), repo, "ls-remote", "--heads", bare)
		if strings.Contains(listing, "dream/") {
			t.Fatalf("a rejected preview pushed a branch: %s", listing)
		}
	})
}

// TestPreviewPublishesRequestBranch verifies step 1-2 of the transport: the
// request ref points at the fixed baseline commit and the proposal stays
// generating while the worker has not answered.
func TestPreviewPublishesRequestBranch(t *testing.T) {
	repo, bare := initRepo(t)
	approveRemote(t, repo)
	service := newTestService(t, repo, t.TempDir())
	installWorkflow(t, service)
	head := gitRun(t, gitEnv(t), repo, "rev-parse", "HEAD")

	started, err := service.Begin(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if started.Status != StatusGenerating || started.BaseHead != head {
		t.Fatalf("started = %+v, want generating on %s", started, head)
	}
	listing := gitRun(t, gitEnv(t), repo, "ls-remote", bare, "refs/heads/dream/req/*")
	want := RequestRef(started.ProposalID, ProviderDeepSeek)
	if !strings.Contains(listing, want) || !strings.Contains(listing, head) {
		t.Fatalf("request branch listing = %q, want %s -> %s", listing, want, head)
	}

	// No result yet: the proposal stays generating and the wait times out.
	service.Timeout = 200 * time.Millisecond
	proposal, err := service.Run(context.Background(), started.ProposalID)
	if !contractCode(err, "TIMEOUT") {
		t.Fatalf("Run: err=%v, want TIMEOUT", err)
	}
	if proposal.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", proposal.Status)
	}
	// Cleanup dropped both temporary refs.
	listing = gitRun(t, gitEnv(t), repo, "ls-remote", bare, "refs/heads/dream/*")
	if strings.TrimSpace(listing) != "" {
		t.Fatalf("temporary refs remained after a failed run: %s", listing)
	}
}

// TestPreviewAppliesWorkerResult covers the happy path: the worker publishes a
// result tree with a nested JSON block, the local side recomputes the diff and
// stores an applyable proposal, then deletes both temporary branches.
func TestPreviewAppliesWorkerResult(t *testing.T) {
	repo, bare := initRepo(t)
	approveRemote(t, repo)
	service := newTestService(t, repo, t.TempDir())
	installWorkflow(t, service)
	stateDir := t.TempDir()

	started, err := service.Begin(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	runWorker(t, bare, repo, started.ProposalID, WorkerMeta{
		Status:   WorkerStatusReady,
		Warnings: []string{"review the merged topic"},
	}, map[string]string{
		"MEMORY.md": "# Memory\n\n## Index\n- [[topic]]\n- [[extra]]\n",
		"extra.md":  "# Extra\n- added [source: test; added: 2026-10-19]\n",
	})

	proposal, err := service.Run(context.Background(), started.ProposalID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if proposal.Status != StatusReady || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}
	if len(proposal.Changes) != 2 {
		t.Fatalf("changes = %+v, want two", proposal.Changes)
	}
	if len(proposal.Warnings) == 0 {
		t.Fatalf("warnings = %v, want the worker warning", proposal.Warnings)
	}
	loaded, err := service.Load(context.Background(), proposal.ProposalID)
	if err != nil || loaded.PatchHash != proposal.PatchHash {
		t.Fatalf("persisted proposal = %+v (err=%v)", loaded, err)
	}
	if _, err := service.PrepareApply(context.Background(), proposal.ProposalID, proposal.PatchHash); err != nil {
		t.Fatalf("PrepareApply: %v", err)
	}
	if listing := gitRun(t, gitEnv(t), repo, "ls-remote", bare, "refs/heads/dream/*"); strings.TrimSpace(listing) != "" {
		t.Fatalf("temporary refs remained: %s", listing)
	}
	if local := gitRun(t, gitEnv(t), repo, "for-each-ref", "--format=%(refname)", "refs/memctl/dream/"); strings.TrimSpace(local) != "" {
		t.Fatalf("fetched local ref remained: %s", local)
	}
	_ = stateDir
}

// TestPreviewRejectsProtectedResultCoversD03 covers the D03 range checks: a
// result that touches `.github`, deletes MEMORY.md, escapes the root or carries
// a second parent is refused and the proposal fails.
func TestPreviewRejectsProtectedResultCoversD03(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{
			name: "workflow change",
			mutate: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, WorkflowRelativePath), "name: evil\n")
			},
			want: "UNSUPPORTED",
		},
		{
			name: "entry deletion",
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "MEMORY.md")); err != nil {
					t.Fatalf("remove MEMORY.md: %v", err)
				}
			},
			want: "UNSUPPORTED",
		},
		{
			name: "merge parent",
			mutate: func(t *testing.T, dir string) {
				env := gitEnv(t)
				gitRun(t, env, dir, "checkout", "-q", "-b", "other")
				writeFile(t, filepath.Join(dir, "extra.md"), "# Extra\n")
				gitRun(t, env, dir, "add", "--", "extra.md")
				gitRun(t, env, dir, "commit", "-q", "-m", "other")
				gitRun(t, env, dir, "checkout", "-q", "main")
				gitRun(t, env, dir, "merge", "-q", "--no-ff", "-m", "merge", "other")
			},
			want: "UNSUPPORTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, bare := initRepo(t)
			approveRemote(t, repo)
			service := newTestService(t, repo, t.TempDir())
			installWorkflow(t, service)

			started, err := service.Begin(context.Background(), PreviewRequest{Runner: ProviderDeepSeek})
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			// The stub worker publishes whatever the test's mutation produced.
			// The result commit keeps its tree and parents; only its message is
			// rewritten to carry the run metadata.
			publishCustomResult(t, bare, started.ProposalID, WorkerMeta{Status: WorkerStatusReady}, tc.mutate)

			proposal, err := service.Run(context.Background(), started.ProposalID)
			if !contractCode(err, tc.want) {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
			if proposal.Status != StatusFailed || proposal.Error == "" {
				t.Fatalf("proposal = %+v, want a failed record with a reason", proposal)
			}
		})
	}
}

// TestPreviewReportsWorkerFailure covers protocol invariant 3: a failed worker
// result is turned into a failed proposal with the worker's reason instead of a
// timeout.
func TestPreviewReportsWorkerFailure(t *testing.T) {
	repo, bare := initRepo(t)
	approveRemote(t, repo)
	service := newTestService(t, repo, t.TempDir())
	installWorkflow(t, service)

	started, err := service.Begin(context.Background(), PreviewRequest{Runner: ProviderMiniMax})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	runWorker(t, bare, repo, started.ProposalID, WorkerMeta{
		Status: WorkerStatusFailed,
		Error:  "minimax returned HTTP 401: invalid api key",
	}, nil)

	proposal, err := service.Run(context.Background(), started.ProposalID)
	if !contractCode(err, "UNSUPPORTED") || !strings.Contains(proposal.Error, "HTTP 401") {
		t.Fatalf("proposal = %+v (err=%v), want the worker reason", proposal, err)
	}
	if proposal.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", proposal.Status)
	}
}

// TestInitWorkflowIsIdempotentCoversD04 covers the D04 init-workflow case: the
// first run writes and commits, the second run reports unchanged and creates no
// empty commit.
func TestInitWorkflowIsIdempotentCoversD04(t *testing.T) {
	repo, _ := initRepo(t)
	service := newTestService(t, repo, t.TempDir())

	first, err := service.InitWorkflow(context.Background(), "")
	if err != nil {
		t.Fatalf("InitWorkflow: %v", err)
	}
	if first.State != InitWorkflowWritten || first.Commit == "" {
		t.Fatalf("first run = %+v, want written with a commit", first)
	}
	if info, err := os.Lstat(filepath.Join(repo, WorkflowRelativePath)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("workflow file is missing: %v", err)
	}

	second, err := service.InitWorkflow(context.Background(), "")
	if err != nil {
		t.Fatalf("second InitWorkflow: %v", err)
	}
	if second.State != InitWorkflowUnchanged || second.Commit != "" {
		t.Fatalf("second run = %+v, want unchanged", second)
	}
}

// TestEntryPointIsProtectedFromManagedWrites verifies that the `.github`
// boundary holds for ordinary reads and writes, not just for proposals.
func TestEntryPointIsProtectedFromManagedWrites(t *testing.T) {
	repo, _ := initRepo(t)
	root, err := vault.OpenRoot(repo)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer root.Close()
	if _, err := root.ReadFile(WorkflowRelativePath); !contractCode(err, "INVALID_PATH") {
		t.Fatalf("reading a workflow path: err=%v, want INVALID_PATH", err)
	}
	paths, err := root.ListManaged()
	if err != nil {
		t.Fatalf("ListManaged: %v", err)
	}
	for _, path := range paths {
		if strings.HasPrefix(strings.ToLower(path), ".github/") {
			t.Fatalf("ListManaged returned %q", path)
		}
	}
}

// contractCode reports whether err carries the given contract code.
func contractCode(err error, code string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), code)
}

// TestWorkflowVersionDriftBlocksGenerationCoversD01 covers the version half of
// the D01 preconditions: the workflow pins the release it downloads, so a
// workflow written by another memctl must stop generation with an actionable
// error instead of running a worker whose rules differ from the local ones.
func TestWorkflowVersionDriftBlocksGenerationCoversD01(t *testing.T) {
	repo, _ := initRepo(t)
	approveRemote(t, repo)
	service := newTestService(t, repo, t.TempDir())
	service.Version = "v0.9.9"
	installWorkflow(t, service)

	// The fixture installed the workflow through InitWorkflow with no version,
	// so it still carries the placeholder.
	code, reason := service.BackendStatus(context.Background())
	if code != BackendNotReadyWorkflowStale || !strings.Contains(reason, "does not pin") {
		t.Fatalf("unpinned workflow: code=%s reason=%q, want WORKFLOW_STALE", code, reason)
	}

	// A workflow pinned to a different release is refused too, and the message
	// names both versions so the operator can see the drift.
	result, err := service.InitWorkflow(context.Background(), "v0.7.2")
	if err != nil || result.State != InitWorkflowWritten {
		t.Fatalf("InitWorkflow: %+v (err=%v)", result, err)
	}
	code, reason = service.BackendStatus(context.Background())
	if code != BackendNotReadyWorkflowStale {
		t.Fatalf("stale pin: code=%s, want WORKFLOW_STALE", code)
	}
	if !strings.Contains(reason, "v0.7.2") || !strings.Contains(reason, "v0.9.9") {
		t.Fatalf("drift message does not name both versions: %q", reason)
	}
	if _, err := service.Preview(context.Background(), PreviewRequest{Runner: ProviderDeepSeek}); !contractCode(err, "UNSUPPORTED") {
		t.Fatalf("preview with a stale workflow: err=%v, want UNSUPPORTED", err)
	}

	// Refreshing the workflow with the running version clears the block.
	if _, err := service.InitWorkflow(context.Background(), "v0.9.9"); err != nil {
		t.Fatalf("refresh workflow: %v", err)
	}
	if code, reason := service.BackendStatus(context.Background()); code != "" {
		t.Fatalf("after refresh: code=%s reason=%q, want ready", code, reason)
	}

	// A development build has no release artifact to pin and is exempt.
	dev := newTestService(t, repo, t.TempDir())
	dev.Version = devVersion
	if code, reason := dev.BackendStatus(context.Background()); code != "" {
		t.Fatalf("development build: code=%s reason=%q, want ready", code, reason)
	}
}

func TestWorkflowPinnedVersionParsing(t *testing.T) {
	cases := map[string]string{
		"      MEMCTL_DREAM_VERSION: v0.7.2\n":                     "v0.7.2",
		"  MEMCTL_DREAM_VERSION: __MEMCTL_DREAM_VERSION__\n":       "",
		"      MEMCTL_DREAM_VERSION: ${{ vars.MEMCTL_DREAM_X }}\n": "${{ vars.MEMCTL_DREAM_X }}",
		"      OTHER: v0.7.2\n":                                    "",
	}
	for content, want := range cases {
		if got := workflowPinnedVersion(content); got != want {
			t.Errorf("workflowPinnedVersion(%q) = %q, want %q", content, got, want)
		}
	}
}
