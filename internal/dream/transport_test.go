package dream

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/state"
)

// This file holds the small Git fixtures of the transport tests: how a stub
// worker publishes a result commit and how the remote approval record is
// seeded. They are deliberately separate from the assertions so a failure in a
// fixture is easy to tell apart from a failure under test.

// runWorker publishes one result commit the way the Actions job does and pushes
// it to the result branch.
func runWorker(t *testing.T, bare, repo, proposalID string, meta WorkerMeta, files map[string]string) {
	t.Helper()
	env := gitEnv(t)
	workerDir := t.TempDir()
	gitRun(t, env, workerDir, "clone", "-q", bare, workerDir)
	base := gitRun(t, env, workerDir, "rev-parse", "HEAD")
	for path, content := range files {
		writeFile(t, filepath.Join(workerDir, filepath.FromSlash(path)), content)
		gitRun(t, env, workerDir, "add", "--", path)
	}
	meta.SchemaVersion = 1
	meta.ProposalID = proposalID
	meta.BaseHead = base
	if meta.Provider == "" {
		meta.Provider = ProviderDeepSeek
	}
	if meta.Model == "" {
		meta.Model = "fake-model"
	}
	if meta.Warnings == nil {
		meta.Warnings = []string{}
	}
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("encode worker meta: %v", err)
	}
	gitRun(t, env, workerDir, "commit", "-q", "--allow-empty", "-m",
		"dream("+proposalID+"): "+meta.Status+"\n\n"+string(body)+"\n")
	head := gitRun(t, env, workerDir, "rev-parse", "HEAD")
	gitRun(t, env, workerDir, "push", "-q", "origin", head+":"+ResultRef(proposalID))
	_ = repo
}

// publishCustomResult clones a fresh checkout of the current main branch, runs
// the test's mutation on it, and publishes the resulting commit as the result
// branch with the run metadata in the message.
func publishCustomResult(t *testing.T, bare, proposalID string, meta WorkerMeta, mutate func(t *testing.T, dir string)) {
	t.Helper()
	env := gitEnv(t)
	dir := t.TempDir()
	gitRun(t, env, dir, "clone", "-q", bare, dir)
	base := gitRun(t, env, dir, "rev-parse", "HEAD")
	mutate(t, dir)
	gitRun(t, env, dir, "add", "-A")
	meta.SchemaVersion = 1
	meta.ProposalID = proposalID
	meta.BaseHead = base
	if meta.Provider == "" {
		meta.Provider = ProviderDeepSeek
	}
	if meta.Model == "" {
		meta.Model = "fake-model"
	}
	if meta.Warnings == nil {
		meta.Warnings = []string{}
	}
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("encode worker meta: %v", err)
	}
	message := "dream(" + proposalID + "): " + meta.Status + "\n\n" + string(body) + "\n"
	if created := gitRun(t, env, dir, "rev-parse", "HEAD"); created != base {
		// The mutation made its own commit (for example a merge); rewrite only
		// its message so the commit shape under test is preserved.
		gitRun(t, env, dir, "commit", "-q", "--amend", "-m", message)
	} else {
		gitRun(t, env, dir, "commit", "-q", "--allow-empty", "-m", message)
	}
	head := gitRun(t, env, dir, "rev-parse", "HEAD")
	gitRun(t, env, dir, "push", "-q", "origin", head+":"+ResultRef(proposalID))
}

// saveApprovalForTest writes the remote approval record the preconditions read.
func saveApprovalForTest(gitDir string, urls []string) error {
	return state.OpenStore(gitDir).SaveApproval(state.Approval{URLs: urls, ConfirmedAt: state.Now()})
}

// TestServiceSourceUsesConfiguredRepo guards the fixture itself: a service bound
// to a repository resolves the same RepoID the application service uses.
func TestServiceSourceUsesConfiguredRepo(t *testing.T) {
	repo, _ := initRepo(t)
	service := newTestService(t, repo, t.TempDir())
	if service.Source.RepoID != config.RepoID(repo) {
		t.Fatalf("repo id = %q, want %q", service.Source.RepoID, config.RepoID(repo))
	}
}
