package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/dream"
)

// This file verifies the explicit apply of stage D-2 (T036) against the GitHub
// Actions transport: D04 (the same proposal applied twice returns the same
// commit), D02 (an old-baseline proposal refuses to apply) and A03 (the apply
// route is rejected in read-only mode). Generation uses an injected provider, so
// no model endpoint and no Actions run is required.

// startDreamProposal posts a preview and returns its proposal id.
func startDreamProposal(t *testing.T, fixture *b1Fixture) string {
	t.Helper()
	res := fixture.send(t, http.MethodPost, "/api/v1/dream:preview", fmt.Sprintf(`{"runner":%q}`, dream.ProviderDeepSeek), nil)
	mustStatus(t, res.status, http.StatusAccepted)
	var started struct {
		ProposalID string `json:"proposalId"`
	}
	if err := json.Unmarshal(res.body, &started); err != nil || started.ProposalID == "" {
		t.Fatalf("decode preview response: %v (%s)", err, res.body)
	}
	return started.ProposalID
}

// waitForReadyProposal polls GET /api/v1/dream/{id} until it reaches a terminal
// status and returns the stored proposal.
func waitForReadyProposal(t *testing.T, fixture *b1Fixture, proposalID string, timeout time.Duration) dreamProposalJSON {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var proposal dreamProposalJSON
	for time.Now().Before(deadline) {
		res := fixture.get(t, "/api/v1/dream/"+proposalID)
		if res.status != http.StatusOK {
			t.Fatalf("get proposal: %d %s", res.status, res.body)
		}
		if err := json.Unmarshal(res.body, &proposal); err != nil {
			t.Fatalf("decode proposal: %v", err)
		}
		if proposal.Status == "ready" || proposal.Status == "failed" || proposal.Status == "applied" || proposal.Status == "stale" {
			return proposal
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("proposal %s did not finish in %s", proposalID, timeout)
	return proposal
}

// TestD2DreamHTTPApplyCoversD04 covers the successful apply and the repeated
// apply: the second request returns the same commit and creates no new commit.
func TestD2DreamHTTPApplyCoversD04(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	bare := prepareHTTPDreamFixture(t, fixture)
	installDreamService(t, fixture, stubProvider{})

	proposalID := startDreamProposal(t, fixture)
	base := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	answerRequestBranch(t, bare, proposalID, base, `topic.md "# Topic\n- organized [source: test; added: 2026-10-19]\n"`)
	proposal := waitForReadyProposal(t, fixture, proposalID, 30*time.Second)
	if proposal.Status != "ready" || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}

	applyBody := fmt.Sprintf(`{"proposalId":%q,"patchHash":%q}`, proposal.ProposalID, proposal.PatchHash)
	applied := fixture.send(t, http.MethodPost, "/api/v1/dream:apply", applyBody, nil)
	mustStatus(t, applied.status, http.StatusOK)
	var mutation struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(applied.body, &mutation); err != nil || mutation.Commit == "" {
		t.Fatalf("decode apply result: %v (%s)", err, applied.body)
	}
	if got := mustReadFile(t, filepath.Join(fixture.repo, "topic.md")); !strings.Contains(got, "organized") {
		t.Fatalf("apply did not write the proposal content: %q", got)
	}
	if head := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD"); head != mutation.Commit {
		t.Fatalf("apply commit = %q, HEAD = %q", mutation.Commit, head)
	}
	stored := waitForReadyProposal(t, fixture, proposalID, 5*time.Second)
	if stored.Status != "applied" || stored.AppliedCommit != mutation.Commit {
		t.Fatalf("stored proposal = %+v, want applied as %s", stored, mutation.Commit)
	}

	commits := gitOutput(t, os.Environ(), fixture.repo, "rev-list", "--count", "HEAD")
	repeat := fixture.send(t, http.MethodPost, "/api/v1/dream:apply", applyBody, nil)
	mustStatus(t, repeat.status, http.StatusOK)
	var repeated struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(repeat.body, &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.Commit != mutation.Commit {
		t.Fatalf("repeated apply commit = %q, want %q", repeated.Commit, mutation.Commit)
	}
	if got := gitOutput(t, os.Environ(), fixture.repo, "rev-list", "--count", "HEAD"); got != commits {
		t.Fatalf("repeated apply created a commit: %s -> %s", commits, got)
	}
}

// TestD2DreamHTTPStaleCoversD02 covers the old-baseline rejection: after the
// repository moves, the apply is refused with 409 PROPOSAL_STALE, the proposal
// is marked stale and every proposed path is left untouched.
func TestD2DreamHTTPStaleCoversD02(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	bare := prepareHTTPDreamFixture(t, fixture)
	installDreamService(t, fixture, stubProvider{})

	proposalID := startDreamProposal(t, fixture)
	base := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	answerRequestBranch(t, bare, proposalID, base, `topic.md "# Topic\n- organized [source: test; added: 2026-10-19]\n"`)
	proposal := waitForReadyProposal(t, fixture, proposalID, 30*time.Second)
	if proposal.Status != "ready" {
		t.Fatalf("proposal = %+v, want ready", proposal)
	}
	// Move the repository after generation.
	fixture.update(t, "topic.md", "# Topic\n- fact\n- newer fact\n")

	before := mustReadFile(t, filepath.Join(fixture.repo, "topic.md"))
	commits := gitOutput(t, os.Environ(), fixture.repo, "rev-list", "--count", "HEAD")
	applyBody := fmt.Sprintf(`{"proposalId":%q,"patchHash":%q}`, proposal.ProposalID, proposal.PatchHash)
	stale := fixture.send(t, http.MethodPost, "/api/v1/dream:apply", applyBody, nil)
	mustStatus(t, stale.status, http.StatusConflict)
	mustCode(t, errorCode(t, stale), "PROPOSAL_STALE")
	if got := mustReadFile(t, filepath.Join(fixture.repo, "topic.md")); got != before {
		t.Fatalf("a stale apply modified the file: %q", got)
	}
	if got := gitOutput(t, os.Environ(), fixture.repo, "rev-list", "--count", "HEAD"); got != commits {
		t.Fatalf("a stale apply created a commit: %s -> %s", commits, got)
	}
	stored := waitForReadyProposal(t, fixture, proposalID, 5*time.Second)
	if stored.Status != "stale" {
		t.Fatalf("stored proposal status = %q, want stale", stored.Status)
	}
}

// TestD2DreamReadOnlyCoversA03 verifies the server-side read-only gate rejects
// the Dreaming apply route.
func TestD2DreamReadOnlyCoversA03(t *testing.T) {
	fixture := newB1Fixture(t, true)
	blocked := fixture.send(t, http.MethodPost, "/api/v1/dream:apply",
		`{"proposalId":"dp-00000000000000000000000000000000","patchHash":"sha256:x"}`, nil)
	mustStatus(t, blocked.status, http.StatusForbidden)
	mustCode(t, errorCode(t, blocked), "READ_ONLY")
}

// TestD2DreamCLIApplyCoversD04 covers the CLI surface: `dream --runner` then
// `dream --apply ID --patch-hash HASH`, and a repeated apply that returns the
// same commit. The worker is stubbed by pushing the result branch through the
// injected provider before the CLI waits for it.
func TestD2DreamCLIApplyCoversD04(t *testing.T) {
	fixture := newDreamFixture(t)
	fixture.write(t, "topic.md", "# Topic\n- fact one\n")

	// The CLI blocks until the result exists, so it runs in a goroutine while the
	// stub worker answers the request branch it publishes.
	generated := make(chan cliResult, 1)
	go func() {
		generated <- runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--runner", "deepseek")
	}()
	proposalID, base := waitForRequestBranch(t, fixture)
	runWorkerInClone(t, fixture, proposalID, base, topicOrganizerStep)

	var preview cliResult
	select {
	case preview = <-generated:
	case <-time.After(60 * time.Second):
		t.Fatal("dream --runner did not finish")
	}
	if preview.code != 0 {
		t.Fatalf("dream --runner: code=%d stdout=%q stderr=%q", preview.code, preview.stdout, preview.stderr)
	}
	var proposal dreamProposalJSON
	if err := json.Unmarshal([]byte(preview.stdout), &proposal); err != nil {
		t.Fatalf("decode proposal: %v\n%s", err, preview.stdout)
	}
	if proposal.Status != "ready" || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}

	applied := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
	if applied.code != 0 {
		t.Fatalf("dream --apply: code=%d stdout=%q stderr=%q", applied.code, applied.stdout, applied.stderr)
	}
	var result struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(applied.stdout), &result); err != nil || result.Commit == "" {
		t.Fatalf("decode apply result: %v\n%s", err, applied.stdout)
	}
	if got := mustReadFile(t, filepath.Join(fixture.repo, "topic.md")); !strings.Contains(got, "organized") {
		t.Fatalf("apply did not write the proposal content: %q", got)
	}
	if localHead := gitOutput(t, fixture.env, fixture.repo, "rev-parse", "HEAD"); result.Commit != localHead {
		t.Fatalf("apply commit = %q, HEAD = %q", result.Commit, localHead)
	}

	commits := gitOutput(t, fixture.env, fixture.repo, "rev-list", "--count", "HEAD")
	repeated := runMemctl(t, fixture.bin, fixture.home, fixture.env, "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
	if repeated.code != 0 {
		t.Fatalf("repeated dream --apply: code=%d stderr=%q", repeated.code, repeated.stderr)
	}
	var again struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(repeated.stdout), &again); err != nil {
		t.Fatal(err)
	}
	if again.Commit != result.Commit {
		t.Fatalf("repeated apply commit = %q, want %q", again.Commit, result.Commit)
	}
	if got := gitOutput(t, fixture.env, fixture.repo, "rev-list", "--count", "HEAD"); got != commits {
		t.Fatalf("repeated apply created a commit: %s -> %s", commits, got)
	}
}

// TestD2DreamPreviewBoundCoversBusy verifies the documented concurrency limit on
// generation tasks: a second preview while one is still waiting for its worker
// result is rejected with 503 BUSY.
func TestD2DreamPreviewBoundCoversBusy(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	prepareHTTPDreamFixture(t, fixture)
	// A provider that never publishes a result keeps the first run waiting.
	service := installDreamService(t, fixture, stubProvider{response: `{"operations":[],"warnings":[]}`})
	service.Timeout = 5 * time.Second

	first := fixture.send(t, http.MethodPost, "/api/v1/dream:preview", fmt.Sprintf(`{"runner":%q}`, dream.ProviderDeepSeek), nil)
	mustStatus(t, first.status, http.StatusAccepted)
	var started struct {
		ProposalID string `json:"proposalId"`
	}
	if err := json.Unmarshal(first.body, &started); err != nil || started.ProposalID == "" {
		t.Fatalf("decode preview response: %v (%s)", err, first.body)
	}

	second := fixture.send(t, http.MethodPost, "/api/v1/dream:preview", fmt.Sprintf(`{"runner":%q}`, dream.ProviderDeepSeek), nil)
	mustStatus(t, second.status, http.StatusServiceUnavailable)
	mustCode(t, errorCode(t, second), "BUSY")

	// Drain the first generation so the fixture is not torn down mid-run.
	if proposal := waitForReadyProposal(t, fixture, started.ProposalID, 30*time.Second); proposal.Status != "failed" {
		t.Fatalf("first proposal = %+v, want failed after the worker wait timed out", proposal)
	}
}

// waitForRequestBranch polls the bare remote for the request branch the CLI
// pushed and returns the proposal id and its baseline commit.
func waitForRequestBranch(t *testing.T, fixture *dreamFixture) (string, string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		listing := gitOutput(t, fixture.env, fixture.repo, "ls-remote", "--heads", fixture.bare, "refs/heads/dream/req/*")
		for _, line := range strings.Split(listing, "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			trimmed := strings.TrimPrefix(fields[1], "refs/heads/dream/req/")
			parts := strings.Split(trimmed, "/")
			if len(parts) == 2 {
				return parts[0], fields[0]
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the CLI did not publish a request branch")
	return "", ""
}

// topicOrganizerStep is the deterministic worker response used by the CLI test.
const topicOrganizerStep = `{"operations":[{"action":"update","path":"topic.md","content":"# Topic\n- organized [source: test; added: 2026-10-19]\n","rationale":"organize"}],"warnings":[]}`

// runWorkerInClone runs the worker the way GitHub Actions does: in a throwaway
// clone of the remote, never in the user's working tree.
func runWorkerInClone(t *testing.T, fixture *dreamFixture, proposalID, base, response string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "worker")
	if out, err := gitCommandAt(fixture.home, "clone", "-q", fixture.bare, clone); err != nil {
		t.Fatalf("clone worker: %v\n%s", err, out)
	}
	if out, err := gitCommandAt(clone, "checkout", "-q", base); err != nil {
		t.Fatalf("checkout baseline: %v\n%s", err, out)
	}
	service := dream.NewService(dream.Source{
		RepoPath: clone,
		RepoID:   configRepoID(clone),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})
	service.NewProvider = func(string) (dream.Provider, error) {
		return stubProvider{response: response}, nil
	}
	if _, err := service.Worker(context.Background(), dream.RequestRef(proposalID, dream.ProviderDeepSeek), base); err != nil {
		t.Fatalf("worker: %v", err)
	}
}
