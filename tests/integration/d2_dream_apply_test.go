package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/dream"
)

// This file verifies stage D-2 subtask 1 (T036) from
// docs/05-validation-and-delivery.md: D04 (the same proposal applied twice and
// an Apply that was interrupted after its commit return the same commit without
// committing twice), D02 (an old-baseline proposal refuses to apply) and A03
// (POST /api/v1/dream:apply is rejected in read-only mode). It uses the HTTP
// handler and the real CLI; generation uses a synthetic runner so no real
// claude/codex host is required.

// attachDreamRunner wires a Dreaming service with one synthetic runner into a
// fixture application.
func attachDreamRunner(t *testing.T, fixture *b1Fixture, body string) {
	t.Helper()
	stubDir := filepath.Join(fixture.home, "stubs")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatalf("create stub dir: %v", err)
	}
	writeStub(t, stubDir, "claude", body)
	service := dream.NewService(dream.Source{
		RepoPath: config.Canonical(fixture.repo),
		RepoID:   config.RepoID(fixture.repo),
		StateDir: filepath.Join(fixture.home, ".agent-memory"),
	})
	service.Runners = map[string]dream.Runner{
		"claude": {Name: "claude", Binary: filepath.Join(stubDir, "claude"), BuildArgs: func(string) []string { return nil }},
	}
	service.Timeout = 20 * time.Second
	fixture.app.SetDreamService(service)
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

// startDreamProposal posts a preview and returns its proposal id.
func startDreamProposal(t *testing.T, fixture *b1Fixture) string {
	t.Helper()
	res := fixture.send(t, http.MethodPost, "/api/v1/dream:preview", `{"runner":"claude"}`, nil)
	mustStatus(t, res.status, http.StatusAccepted)
	var started struct {
		ProposalID string `json:"proposalId"`
	}
	if err := json.Unmarshal(res.body, &started); err != nil || started.ProposalID == "" {
		t.Fatalf("decode preview response: %v (%s)", err, res.body)
	}
	return started.ProposalID
}

// TestD2DreamHTTPApplyCoversD04 covers the successful apply and the repeated
// apply: the second request returns the same commit and creates no new commit.
func TestD2DreamHTTPApplyCoversD04(t *testing.T) {
	requireDarwinSandbox(t)
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	attachDreamRunner(t, fixture, "printf '# Topic\\n- organized\\n' > topic.md\n")

	proposalID := startDreamProposal(t, fixture)
	proposal := waitForReadyProposal(t, fixture, proposalID, 20*time.Second)
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
	requireDarwinSandbox(t)
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	attachDreamRunner(t, fixture, "printf '# Topic\\n- organized\\n' > topic.md\n")

	proposalID := startDreamProposal(t, fixture)
	proposal := waitForReadyProposal(t, fixture, proposalID, 20*time.Second)
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
// same commit.
func TestD2DreamCLIApplyCoversD04(t *testing.T) {
	requireDarwinSandbox(t)
	bin, home, repo, env, _ := dreamRepo(t, "printf '# Topic\\n- organized\\n' > topic.md\n")

	generated := runMemctl(t, bin, home, env, "dream", "--runner", "claude")
	if generated.code != 0 {
		t.Fatalf("dream --runner: code=%d stdout=%q stderr=%q", generated.code, generated.stdout, generated.stderr)
	}
	var proposal dreamProposalJSON
	if err := json.Unmarshal([]byte(generated.stdout), &proposal); err != nil {
		t.Fatalf("decode proposal: %v\n%s", err, generated.stdout)
	}
	if proposal.Status != "ready" || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}

	applied := runMemctl(t, bin, home, env, "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
	if applied.code != 0 {
		t.Fatalf("dream --apply: code=%d stdout=%q stderr=%q", applied.code, applied.stdout, applied.stderr)
	}
	var result struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal([]byte(applied.stdout), &result); err != nil || result.Commit == "" {
		t.Fatalf("decode apply result: %v\n%s", err, applied.stdout)
	}
	if got := mustReadFile(t, filepath.Join(repo, "topic.md")); !strings.Contains(got, "organized") {
		t.Fatalf("apply did not write the proposal content: %q", got)
	}
	localHead := gitOutput(t, env, repo, "rev-parse", "HEAD")
	if result.Commit != localHead {
		t.Fatalf("apply commit = %q, HEAD = %q", result.Commit, localHead)
	}

	commits := gitOutput(t, env, repo, "rev-list", "--count", "HEAD")
	repeated := runMemctl(t, bin, home, env, "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
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
	if got := gitOutput(t, env, repo, "rev-list", "--count", "HEAD"); got != commits {
		t.Fatalf("repeated apply created a commit: %s -> %s", commits, got)
	}
}

// TestD2DreamPreviewBoundCoversBusy verifies the documented concurrency limit
// on generation tasks (docs/04-ui-and-cli-specification.md section 5): a second
// preview while one is still running is rejected with 503 BUSY instead of
// spawning another sandboxed runner.
func TestD2DreamPreviewBoundCoversBusy(t *testing.T) {
	requireDarwinSandbox(t)
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")
	attachDreamRunner(t, fixture, "sleep 2\nprintf '# Topic\\n- organized\\n' > topic.md\n")

	first := fixture.send(t, http.MethodPost, "/api/v1/dream:preview", `{"runner":"claude"}`, nil)
	mustStatus(t, first.status, http.StatusAccepted)
	var started struct {
		ProposalID string `json:"proposalId"`
	}
	if err := json.Unmarshal(first.body, &started); err != nil || started.ProposalID == "" {
		t.Fatalf("decode preview response: %v (%s)", err, first.body)
	}

	second := fixture.send(t, http.MethodPost, "/api/v1/dream:preview", `{"runner":"claude"}`, nil)
	mustStatus(t, second.status, http.StatusServiceUnavailable)
	mustCode(t, errorCode(t, second), "BUSY")

	// Drain the first generation so the fixture is not torn down mid-run.
	if proposal := waitForReadyProposal(t, fixture, started.ProposalID, 20*time.Second); proposal.Status != "ready" {
		t.Fatalf("first proposal = %+v, want ready", proposal)
	}
}
