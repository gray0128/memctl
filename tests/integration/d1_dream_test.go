package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/state"
)

// This file verifies the GitHub Actions Dreaming transport of
// docs/11-github-actions-dreaming.md from the CLI and HTTP surfaces: D01 (an
// unready backend refuses generation without pushing a branch), D02 (a dirty
// repository refuses generation), D03 (an out-of-range result is rejected and
// fails the proposal) and D04 (`--init-workflow` is idempotent). A local bare
// repository stands in for GitHub, so no network or credential source is used.

// dreamProposalJSON is the CLI/HTTP proposal shape this file inspects.
type dreamProposalJSON struct {
	ProposalID    string   `json:"proposalId"`
	Status        string   `json:"status"`
	PatchHash     string   `json:"patchHash"`
	Diff          string   `json:"diff"`
	Warnings      []string `json:"warnings"`
	Error         string   `json:"error"`
	AppliedCommit string   `json:"appliedCommit"`
	Runner        string   `json:"runner"`
	Changes       []struct {
		Path   string `json:"path"`
		Action string `json:"action"`
	} `json:"changes"`
}

// dreamFixture is one prepared memory repository with a bare origin, the
// confirmed remote approval and the installed workflow.
type dreamFixture struct {
	bin   string
	home  string
	repo  string
	bare  string
	state string
	env   []string
}

// newDreamFixture initializes a memory repository connected to a local bare
// remote, approves the remote and installs the Actions workflow.
func newDreamFixture(t *testing.T) *dreamFixture {
	t.Helper()
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	bare := newBareRemote(t, home, "origin.git")
	env := a2Env(home, repo, true)
	env = envWith(env, "GIT_ALLOW_PROTOCOL=file")
	if result := runMemctl(t, bin, home, env, "init", bare, "--confirm-private"); result.code != 0 {
		t.Fatalf("init remote: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	fixture := &dreamFixture{
		bin:   bin,
		home:  home,
		repo:  repo,
		bare:  bare,
		state: filepath.Join(home, ".agent-memory"),
		env:   env,
	}
	if result := runMemctl(t, bin, home, env, "dream", "--init-workflow"); result.code != 0 {
		t.Fatalf("init workflow: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if result := runMemctl(t, bin, home, env, "sync", "--wait"); result.code != 0 {
		t.Fatalf("sync workflow: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	return fixture
}

// write commits one file into the fixture repository through a plain Git
// commit, leaving the worktree clean for generation.
func (f *dreamFixture) write(t *testing.T, path, content string) {
	t.Helper()
	target := filepath.Join(f.repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	gitOutput(t, f.env, f.repo, "add", "--", path)
	gitOutput(t, f.env, f.repo, "commit", "-q", "-m", "seed "+path)
}

// configRepoID is config.RepoID for tests that build a service by hand.
func configRepoID(repoPath string) string { return config.RepoID(repoPath) }

// installDreamService attaches a Dreaming service to an HTTP fixture so the
// preview route can be driven with an injected provider and a short wait.
func installDreamService(t *testing.T, fixture *b1Fixture, provider dream.Provider) *dream.Service {
	t.Helper()
	service := dream.NewService(dream.Source{
		RepoPath: config.Canonical(fixture.repo),
		RepoID:   config.RepoID(fixture.repo),
		StateDir: filepath.Join(fixture.home, ".agent-memory"),
	})
	service.NewProvider = func(string) (dream.Provider, error) { return provider, nil }
	service.Timeout = 20 * time.Second
	service.PollInterval = 50 * time.Millisecond
	fixture.app.SetDreamService(service)
	return service
}

// stubProvider is a deterministic model provider for HTTP-level tests.
type stubProvider struct{ response string }

func (p stubProvider) ID() string    { return dream.ProviderDeepSeek }
func (p stubProvider) Model() string { return "stub-model" }

func (p stubProvider) Complete(context.Context, dream.CompletionRequest) (string, error) {
	return p.response, nil
}

// TestD1DreamUnreadyBackendCoversD01 verifies that every missing prerequisite is
// a targeted error and that no request branch is pushed.
func TestD1DreamUnreadyBackendCoversD01(t *testing.T) {
	t.Run("unknown provider", func(t *testing.T) {
		fixture := newDreamFixture(t)
		result := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--runner", "claude")
		if result.code != 2 {
			t.Fatalf("unknown provider: code=%d stdout=%q stderr=%q, want 2", result.code, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stderr, "deepseek") {
			t.Fatalf("unknown provider error does not list the built-in providers: %q", result.stderr)
		}
	})

	t.Run("missing workflow", func(t *testing.T) {
		fixture := newDreamFixture(t)
		workflow := filepath.Join(fixture.repo, dream.WorkflowRelativePath)
		if err := os.Remove(workflow); err != nil {
			t.Fatalf("remove workflow: %v", err)
		}
		gitOutput(t, fixture.env, fixture.repo, "add", "-A")
		gitOutput(t, fixture.env, fixture.repo, "commit", "-q", "-m", "drop workflow")
		result := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--runner", "deepseek")
		if result.code != 1 || !strings.Contains(result.stderr, "UNSUPPORTED") || !strings.Contains(result.stderr, "--init-workflow") {
			t.Fatalf("missing workflow: code=%d stderr=%q", result.code, result.stderr)
		}
	})

	t.Run("unapproved remote", func(t *testing.T) {
		fixture := newDreamFixture(t)
		gitDir := gitOutput(t, fixture.env, fixture.repo, "rev-parse", "--absolute-git-dir")
		store := state.OpenStore(gitDir)
		if err := store.SaveApproval(state.Approval{URLs: []string{"file:///unrelated"}, ConfirmedAt: state.Now()}); err != nil {
			t.Fatalf("rewrite approval: %v", err)
		}
		result := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--runner", "deepseek")
		if result.code != 1 || !strings.Contains(result.stderr, "REMOTE_NOT_APPROVED") {
			t.Fatalf("unapproved remote: code=%d stderr=%q", result.code, result.stderr)
		}
	})

	t.Run("no remote", func(t *testing.T) {
		fixture := newDreamFixture(t)
		gitOutput(t, fixture.env, fixture.repo, "remote", "remove", "origin")
		result := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--runner", "deepseek")
		if result.code != 1 || !strings.Contains(result.stderr, "UNSUPPORTED") {
			t.Fatalf("no remote: code=%d stderr=%q", result.code, result.stderr)
		}
		if listing := gitOutput(t, fixture.env, fixture.repo, "ls-remote", "--heads", fixture.bare); strings.Contains(listing, "dream/") {
			t.Fatalf("a rejected preview pushed a branch: %s", listing)
		}
	})
}

// TestD1DreamDirtyRepositoryRefusesCoversD02 verifies that an uncommitted
// change stops generation before a proposal exists.
func TestD1DreamDirtyRepositoryRefusesCoversD02(t *testing.T) {
	fixture := newDreamFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.repo, "untracked.md"), []byte("# untracked\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	result := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--runner", "deepseek")
	if result.code != 1 {
		t.Fatalf("dirty repository: code=%d stdout=%q stderr=%q, want 1", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "EXTERNAL_CHANGES") {
		t.Fatalf("dirty repository error is not targeted: %q", result.stderr)
	}
	if matches, _ := filepath.Glob(filepath.Join(fixture.state, "repos", "*", "proposals", "*.json")); len(matches) != 0 {
		t.Fatalf("a proposal was persisted for a dirty repository: %v", matches)
	}
	if listing := gitOutput(t, fixture.env, fixture.repo, "ls-remote", "--heads", fixture.bare); strings.Contains(listing, "dream/") {
		t.Fatalf("a rejected preview pushed a branch: %s", listing)
	}
}

// TestD1DreamPreviewRejectsOutOfRangeCoversD03 verifies that a result which
// touches the workflow file is rejected and the proposal fails instead of
// producing a ready proposal.
func TestD1DreamPreviewRejectsOutOfRangeCoversD03(t *testing.T) {
	fixture := newB1Fixture(t, false)
	bare := prepareHTTPDreamFixture(t, fixture)
	installDreamService(t, fixture, stubProvider{})

	proposalID := startDreamProposal(t, fixture)
	base := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	answerRequestBranch(t, bare, proposalID, base, `.github/workflows/memctl-dream.yml "name: evil\n"`)
	proposal := waitForReadyProposal(t, fixture, proposalID, 30*time.Second)
	if proposal.Status != "failed" {
		t.Fatalf("proposal = %+v, want failed", proposal)
	}
	if !strings.Contains(proposal.Error, "UNSUPPORTED") && !strings.Contains(proposal.Error, "protected") {
		t.Fatalf("proposal error is not the range rejection: %q", proposal.Error)
	}
	workflow := mustReadFile(t, filepath.Join(fixture.repo, dream.WorkflowRelativePath))
	if strings.Contains(workflow, "name: evil") {
		t.Fatalf("the workflow file was modified")
	}
}

// answerRequestBranch publishes the result branch a preview is waiting for, the
// way the Actions job does: a throwaway clone checked out at the request
// baseline, one commit with the run metadata, pushed to dream/res.
func answerRequestBranch(t *testing.T, bare, proposalID, base string, files ...string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "worker")
	if out, err := gitCommandAt(filepath.Dir(clone), "clone", "-q", bare, clone); err != nil {
		t.Fatalf("clone worker: %v\n%s", err, out)
	}
	if out, err := gitCommandAt(clone, "fetch", "-q", "origin"); err != nil {
		t.Fatalf("fetch worker: %v\n%s", err, out)
	}
	if out, err := gitCommandAt(clone, "checkout", "-q", base); err != nil {
		t.Fatalf("checkout baseline: %v\n%s", err, out)
	}
	for _, entry := range files {
		path, content, ok := strings.Cut(entry, " ")
		if !ok {
			t.Fatalf("invalid answer entry %q", entry)
		}
		dir := filepath.Dir(filepath.Join(clone, filepath.FromSlash(path)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(clone, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if out, err := gitCommandAt(clone, "add", "-A"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	meta := fmt.Sprintf(`{"schemaVersion":1,"proposalId":%q,"baseHead":%q,"provider":"deepseek","model":"stub","status":"ready","steps":1,"warnings":[],"error":""}`, proposalID, base)
	message := "dream(" + proposalID + "): ready\n\n" + meta + "\n"
	if out, err := gitCommandAt(clone, "commit", "-q", "-m", message); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	head := gitOutput(t, os.Environ(), clone, "rev-parse", "HEAD")
	if out, err := gitCommandAt(clone, "push", "-q", "origin", head+":refs/heads/dream/res/"+proposalID); err != nil {
		t.Fatalf("push result: %v\n%s", err, out)
	}
}

// prepareHTTPDreamFixture connects an existing HTTP fixture to a local bare
// remote, records the approval through the same init path the CLI uses and
// installs the workflow. It returns the bare remote path.
func prepareHTTPDreamFixture(t *testing.T, fixture *b1Fixture) string {
	t.Helper()
	// The transport uses Git's file protocol against the local bare remote, so
	// the in-process Git children need the same allowance the CLI tests set.
	t.Setenv("GIT_ALLOW_PROTOCOL", "file:ssh")
	bare := newBareRemote(t, fixture.home, "origin.git")
	if out, err := gitCommandAt(fixture.repo, "remote", "add", "origin", bare); err != nil {
		t.Fatalf("add remote: %v\n%s", err, out)
	}
	if _, err := fixture.app.Init(context.Background(), app.InitOptions{RemoteURL: bare, ConfirmPrivate: true}); err != nil {
		t.Fatalf("confirm remote: %v", err)
	}
	if _, err := fixture.app.InitDreamWorkflow(context.Background(), "v0.7.0"); err != nil {
		t.Fatalf("install workflow: %v", err)
	}
	if out, err := gitCommandAt(fixture.repo, "push", "-q", "origin", "main"); err != nil {
		t.Fatalf("push workflow: %v\n%s", err, out)
	}
	return bare
}

// TestD1DreamHTTPPreviewCoversD01AndEvent verifies the asynchronous HTTP route:
// a 202 carrying the proposal id, a later GET that returns the ready proposal and
// one dream.updated event on the SSE bus.
func TestD1DreamHTTPPreviewCoversD01AndEvent(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	bare := prepareHTTPDreamFixture(t, fixture)
	installDreamService(t, fixture, stubProvider{})

	subscription := fixture.server.Events().Subscribe()
	defer subscription.Close()

	proposalID := startDreamProposal(t, fixture)
	base := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	answerRequestBranch(t, bare, proposalID, base, `topic.md "# Topic\n- organized [source: test; added: 2026-10-19]\n"`)
	deadline := time.After(30 * time.Second)
	eventSeen := false
	for !eventSeen {
		select {
		case hint := <-subscription.Hints():
			if hint.Reason == "dream.updated" && hint.ProposalID == proposalID && hint.Status == "ready" {
				eventSeen = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for the dream.updated event")
		}
	}

	shown := fixture.get(t, "/api/v1/dream/"+proposalID)
	var proposal dreamProposalJSON
	fixture.decode(t, shown, &proposal)
	if proposal.Status != "ready" || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}
	if proposal.Runner != dream.ProviderDeepSeek {
		t.Fatalf("proposal runner = %q, want %q", proposal.Runner, dream.ProviderDeepSeek)
	}
	if action, ok := proposalChange(proposal, "topic.md"); !ok || action != "update" {
		t.Fatalf("proposal did not include the worker change: %+v", proposal.Changes)
	}
}

// proposalChange reports whether path is present in the decoded change set.
func proposalChange(proposal dreamProposalJSON, path string) (string, bool) {
	for _, change := range proposal.Changes {
		if change.Path == path {
			return change.Action, true
		}
	}
	return "", false
}

// gitCommandAt runs one Git command in dir with the current environment.
func gitCommandAt(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestD1InitWorkflowIsIdempotentCoversD04 covers the D04 install case through the
// CLI: the first run writes and commits, the second reports unchanged.
func TestD1InitWorkflowIsIdempotentCoversD04(t *testing.T) {
	fixture := newDreamFixture(t)
	first := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--init-workflow")
	if first.code != 0 {
		t.Fatalf("first init: code=%d stderr=%q", first.code, first.stderr)
	}
	var result struct {
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(first.stdout), &result); err != nil {
		t.Fatalf("decode init result: %v (%s)", err, first.stdout)
	}
	if result.State != "unchanged" || result.Path != dream.WorkflowRelativePath {
		t.Fatalf("second install on a current file = %+v, want unchanged", result)
	}
	before := gitOutput(t, fixture.env, fixture.repo, "rev-list", "--count", "HEAD")
	again := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--init-workflow")
	if again.code != 0 {
		t.Fatalf("second init: code=%d stderr=%q", again.code, again.stderr)
	}
	if after := gitOutput(t, fixture.env, fixture.repo, "rev-list", "--count", "HEAD"); after != before {
		t.Fatalf("a no-op --init-workflow created a commit: %s -> %s", before, after)
	}
}
