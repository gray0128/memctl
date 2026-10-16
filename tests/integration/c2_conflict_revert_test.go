package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/server"
)

// This file verifies C-2 subtask 1 (T030) through the HTTP handler: the
// multi-stage conflict routes shared with the CLI resolve state machine and the
// audit revert route with its single-parent validation and conflict recovery.

// newServerForRepo builds a server bound to an existing repository created by
// the CLI fixtures, so a conflict captured by a CLI sync can be resolved through
// HTTP exactly as the UI would.
func newServerForRepo(t *testing.T, home, repo string, readOnly bool) *b1Fixture {
	t.Helper()
	application := app.New(config.Config{
		RepoPath: config.Canonical(repo),
		StateDir: filepath.Join(home, ".agent-memory"),
	})
	srv, err := server.New(server.Options{
		App:      application,
		Addr:     "127.0.0.1:18420",
		ReadOnly: readOnly,
		Token:    b1Token,
	})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	return &b1Fixture{server: srv, app: application, repo: repo, home: home}
}

// TestC2ConflictHTTPResolveAbort covers the two conflict routes: the first
// route finishes a stage and pushes, the second aborts a fresh owned conflict
// and restores the original HEAD.
func TestC2ConflictHTTPResolveAbort(t *testing.T) {
	f := newConflictFixture(t)
	f.write(2, "# Memory\n\n## Index\n- two\n")
	f.write(1, "# Memory\n\n## Index\n- one\n")
	if result := f.sync(1); result.code != 0 {
		t.Fatalf("device1 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	if result := f.sync(2); result.code != 1 {
		t.Fatalf("device2 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	fixture := newServerForRepo(t, f.home2, f.repo2, false)

	res := fixture.get(t, "/api/v1/conflict")
	mustStatus(t, res.status, http.StatusOK)
	var view struct {
		ConflictID   string `json:"conflictId"`
		BaseRevision string `json:"baseRevision"`
		Phase        string `json:"phase"`
	}
	if err := json.Unmarshal(res.body, &view); err != nil {
		t.Fatalf("decode conflict: %v\n%s", err, res.body)
	}
	if view.Phase != "captured" || view.ConflictID == "" {
		t.Fatalf("captured conflict = %+v", view)
	}

	resolved := fixture.send(t, http.MethodPost, "/api/v1/conflict:resolve",
		`{"conflictId":"`+view.ConflictID+`","baseRevision":"`+view.BaseRevision+`","resolvedFiles":[{"path":"MEMORY.md","action":"put","rawContent":"# Memory\n\n## Index\n- one\n"}]}`,
		nil)
	mustStatus(t, resolved.status, http.StatusOK)
	var outcome struct {
		RemoteState string `json:"remoteState"`
		Conflict    *struct {
			Phase string `json:"phase"`
		} `json:"conflict"`
	}
	if err := json.Unmarshal(resolved.body, &outcome); err != nil {
		t.Fatalf("decode resolve: %v\n%s", err, resolved.body)
	}
	if outcome.Conflict != nil || outcome.RemoteState != "synced" {
		t.Fatalf("resolve outcome = %+v, want a finished replay and a synced remote", outcome)
	}
	if got := mustReadFile(t, filepath.Join(f.repo2, "MEMORY.md")); got != "# Memory\n\n## Index\n- one\n" {
		t.Fatalf("resolved content = %q", got)
	}
	if status := gitOutput(t, f.env2(), f.repo2, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the HTTP resolve: %q", status)
	}

	// A fresh conflict on the same devices is aborted through HTTP.
	f.write(1, "# Memory\n\n## Index\n- d1\n")
	if result := f.sync(1); result.code != 0 {
		t.Fatalf("device1 resync: code=%d stderr=%q", result.code, result.stderr)
	}
	f.write(2, "# Memory\n\n## Index\n- d2\n")
	if result := f.sync(2); result.code != 1 {
		t.Fatalf("device2 resync: code=%d stderr=%q", result.code, result.stderr)
	}
	originalHead := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD")

	active := fixture.get(t, "/api/v1/conflict")
	mustStatus(t, active.status, http.StatusOK)
	var second struct {
		ConflictID string `json:"conflictId"`
	}
	if err := json.Unmarshal(active.body, &second); err != nil {
		t.Fatalf("decode second conflict: %v\n%s", err, active.body)
	}

	aborted := fixture.send(t, http.MethodPost, "/api/v1/conflict:abort",
		`{"conflictId":"`+second.ConflictID+`"}`, nil)
	mustStatus(t, aborted.status, http.StatusOK)
	var abortOutcome struct {
		RemoteState string `json:"remoteState"`
	}
	if err := json.Unmarshal(aborted.body, &abortOutcome); err != nil {
		t.Fatalf("decode abort: %v\n%s", err, aborted.body)
	}
	if abortOutcome.RemoteState != "pending" {
		t.Fatalf("abort remoteState = %q, want pending", abortOutcome.RemoteState)
	}
	if head := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD"); head != originalHead {
		t.Fatalf("abort did not restore HEAD: %s -> %s", originalHead, head)
	}
	if status := gitOutput(t, f.env2(), f.repo2, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the HTTP abort: %q", status)
	}
	none := fixture.get(t, "/api/v1/conflict")
	mustStatus(t, none.status, http.StatusNoContent)

	// Aborting with no active conflict fails instead of reporting success.
	missing := fixture.send(t, http.MethodPost, "/api/v1/conflict:abort", `{}`, nil)
	mustStatus(t, missing.status, http.StatusNotFound)
	mustCode(t, errorCode(t, missing), "NOT_FOUND")
}

// TestC2ConflictHTTPStaleCoveredByContract covers the shared state machine
// returning CONFLICT_STALE through HTTP when the captured stage no longer
// matches the repository.
func TestC2ConflictHTTPStaleCoveredByContract(t *testing.T) {
	f := newConflictFixture(t)
	f.write(2, "# Memory\n\n## Index\n- two\n")
	f.write(1, "# Memory\n\n## Index\n- one\n")
	if result := f.sync(1); result.code != 0 {
		t.Fatalf("device1 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	if result := f.sync(2); result.code != 1 {
		t.Fatalf("device2 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	fixture := newServerForRepo(t, f.home2, f.repo2, false)

	res := fixture.get(t, "/api/v1/conflict")
	mustStatus(t, res.status, http.StatusOK)
	var view struct {
		ConflictID   string `json:"conflictId"`
		BaseRevision string `json:"baseRevision"`
	}
	if err := json.Unmarshal(res.body, &view); err != nil {
		t.Fatalf("decode conflict: %v\n%s", err, res.body)
	}
	headBefore := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD")

	stale := fixture.send(t, http.MethodPost, "/api/v1/conflict:resolve",
		`{"conflictId":"`+view.ConflictID+`","baseRevision":"sha256:stale","resolvedFiles":[{"path":"MEMORY.md","action":"put","rawContent":"# Memory\n\n## Index\n- one\n"}]}`,
		nil)
	mustStatus(t, stale.status, http.StatusConflict)
	mustCode(t, errorCode(t, stale), "CONFLICT_STALE")
	if head := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("stale resolve moved HEAD: %s -> %s", headBefore, head)
	}
}

// TestC2AuditRevertHTTP covers the success path and the single-parent
// validation of the audit revert route.
func TestC2AuditRevertHTTP(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- v1\n")
	fixture.update(t, "alpha.md", "# Alpha\n\n- v2\n")
	fixture.write(t, "beta.md", "# Beta\n\n- b\n")

	target := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD~1")
	before := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	res := fixture.send(t, http.MethodPost, "/api/v1/audit/"+target+"/revert",
		`{"baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, res.status, http.StatusOK)
	var mutation struct {
		Commit       string   `json:"commit"`
		ChangedFiles []string `json:"changedFiles"`
		Revision     string   `json:"revision"`
		SyncState    string   `json:"syncState"`
	}
	if err := json.Unmarshal(res.body, &mutation); err != nil {
		t.Fatalf("decode revert: %v\n%s", err, res.body)
	}
	if mutation.Commit == "" || mutation.Commit == before {
		t.Fatalf("revert did not create a new commit: %+v", mutation)
	}
	if len(mutation.ChangedFiles) != 1 || mutation.ChangedFiles[0] != "alpha.md" {
		t.Fatalf("changed files = %v, want alpha.md", mutation.ChangedFiles)
	}
	if mutation.SyncState != "local_only" {
		t.Fatalf("syncState = %q, want local_only", mutation.SyncState)
	}
	if got := mustReadFile(t, filepath.Join(fixture.repo, "alpha.md")); got != "# Alpha\n\n- v1\n" {
		t.Fatalf("reverted content = %q", got)
	}
	if status := gitOutput(t, os.Environ(), fixture.repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the revert: %q", status)
	}

	// Preconditions: a missing baseline is 428, a stale one is 412 and an
	// unknown or malformed object name is refused before Git runs.
	missing := fixture.send(t, http.MethodPost, "/api/v1/audit/"+target+"/revert", `{}`, nil)
	mustStatus(t, missing.status, http.StatusPreconditionRequired)
	mustCode(t, errorCode(t, missing), "PRECONDITION_REQUIRED")

	stale := fixture.send(t, http.MethodPost, "/api/v1/audit/"+target+"/revert",
		`{"baseRevision":"sha256:deadbeef"}`, nil)
	mustStatus(t, stale.status, http.StatusPreconditionFailed)
	mustCode(t, errorCode(t, stale), "REVISION_MISMATCH")

	unknown := fixture.send(t, http.MethodPost, "/api/v1/audit/"+strings.Repeat("f", 40)+"/revert",
		`{"baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, unknown.status, http.StatusNotFound)
	mustCode(t, errorCode(t, unknown), "NOT_FOUND")

	malformed := fixture.send(t, http.MethodPost, "/api/v1/audit/not-a-hash/revert",
		`{"baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, malformed.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, malformed), "INVALID_ARGUMENT")

	root := gitOutput(t, os.Environ(), fixture.repo, "rev-list", "--max-parents=0", "HEAD")
	rootRevert := fixture.send(t, http.MethodPost, "/api/v1/audit/"+root+"/revert",
		`{"baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, rootRevert.status, http.StatusUnprocessableEntity)
	mustCode(t, errorCode(t, rootRevert), "UNSUPPORTED")

	// A merge commit has two parents and is refused without guessing a mainline.
	gitInRepo(t, fixture.repo, "checkout", "-q", "-b", "side")
	fixture.write(t, "side.md", "# Side\n\n- side\n")
	gitInRepo(t, fixture.repo, "checkout", "-q", "main")
	fixture.write(t, "main.md", "# Main\n\n- main\n")
	gitInRepo(t, fixture.repo, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	merge := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	mergeRevert := fixture.send(t, http.MethodPost, "/api/v1/audit/"+merge+"/revert",
		`{"baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, mergeRevert.status, http.StatusUnprocessableEntity)
	mustCode(t, errorCode(t, mergeRevert), "UNSUPPORTED")
}

// TestC2AuditRevertConflict covers the conflicting revert over HTTP: the route
// reports CONTENT_CONFLICT and leaves the original history, worktree and index
// untouched, with no revert state behind.
func TestC2AuditRevertConflict(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- v1\n")
	fixture.update(t, "alpha.md", "# Alpha\n\n- v2\n")
	fixture.update(t, "alpha.md", "# Alpha\n\n- v3\n")

	target := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD~1")
	before := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	res := fixture.send(t, http.MethodPost, "/api/v1/audit/"+target+"/revert",
		`{"baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, res.status, http.StatusConflict)
	mustCode(t, errorCode(t, res), "CONTENT_CONFLICT")
	if head := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD"); head != before {
		t.Fatalf("conflicting revert moved HEAD: %s -> %s", before, head)
	}
	if got := mustReadFile(t, filepath.Join(fixture.repo, "alpha.md")); got != "# Alpha\n\n- v3\n" {
		t.Fatalf("conflicting revert rewrote content: %q", got)
	}
	if status := gitOutput(t, os.Environ(), fixture.repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the aborted revert: %q", status)
	}
	if _, err := os.Lstat(filepath.Join(fixture.repo, ".git", "REVERT_HEAD")); err == nil {
		t.Fatal("aborted revert left REVERT_HEAD behind")
	}
}

// TestC2ConflictAndRevertReadOnly covers A03 for the new management routes: the
// read-only server rejects every one of them before the handler runs.
func TestC2ConflictAndRevertReadOnly(t *testing.T) {
	fixture := newB1Fixture(t, true)
	fixture.write(t, "alpha.md", "# Alpha\n\n- v1\n")
	head := gitOutput(t, os.Environ(), fixture.repo, "rev-parse", "HEAD")
	revision := fixture.revisionOf(t)

	for _, call := range []struct {
		target string
		body   string
	}{
		{"/api/v1/conflict:resolve", `{"conflictId":"x","baseRevision":"y","resolvedFiles":[]}`},
		{"/api/v1/conflict:abort", `{"conflictId":"x"}`},
		{"/api/v1/audit/" + head + "/revert", `{"baseRevision":"` + revision + `"}`},
	} {
		res := fixture.send(t, http.MethodPost, call.target, call.body, nil)
		mustStatus(t, res.status, http.StatusForbidden)
		mustCode(t, errorCode(t, res), "READ_ONLY")
	}
	if _, err := fixture.app.ReadFile(context.Background(), "alpha.md"); err != nil {
		t.Fatalf("read-only server changed the repository: %v", err)
	}
}
