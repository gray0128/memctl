package integration_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// This file verifies the C-2 frontend/backend integration of T031 through the
// HTTP handler: it replays the request sequence the Web UI sends for the
// conflict-resolution view (read the three-way snapshot, resolve one stage from
// one of the three sides, then abort a fresh conflict) and for the audit-revert view
// (list commits and their parents, preview the diff, then revert with the
// current repository baseline). It uses only the documented routes, so a change
// on either side breaks this test.

// conflictSidePayload mirrors one captured ConflictSide. A missing side has
// exists=false and no content; an existing empty file has exists=true with an
// empty content string, which the view must not confuse with a deletion.
type conflictSidePayload struct {
	Exists  bool    `json:"exists"`
	Binary  bool    `json:"binary"`
	Content *string `json:"content"`
}

type conflictFilePayload struct {
	Path     string              `json:"path"`
	Kind     string              `json:"kind"`
	Base     conflictSidePayload `json:"base"`
	Onto     conflictSidePayload `json:"onto"`
	Replayed conflictSidePayload `json:"replayed"`
}

type conflictViewPayload struct {
	ConflictID   string                `json:"conflictId"`
	BaseRevision string                `json:"baseRevision"`
	Phase        string                `json:"phase"`
	ReplayCommit string                `json:"replayCommit"`
	Files        []conflictFilePayload `json:"files"`
}

// TestC2UiConflictFlowCoversIntegration walks one conflict-resolution session
// end to end. The snapshot carries every captured side so the view can render
// the three-way context and build resolvedFiles from the operator's choice.
func TestC2UiConflictFlowCoversIntegration(t *testing.T) {
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

	// 1. The view reads the active conflict snapshot.
	res := fixture.get(t, "/api/v1/conflict")
	mustStatus(t, res.status, http.StatusOK)
	var view conflictViewPayload
	if err := json.Unmarshal(res.body, &view); err != nil {
		t.Fatalf("decode conflict: %v\n%s", err, res.body)
	}
	if view.ConflictID == "" || view.BaseRevision == "" || view.Phase != "captured" {
		t.Fatalf("conflict snapshot incomplete: %+v", view)
	}
	if len(view.Files) != 1 || view.Files[0].Path != "MEMORY.md" || view.Files[0].Kind != "text" {
		t.Fatalf("conflict files = %+v, want one text MEMORY.md", view.Files)
	}
	file := view.Files[0]
	if !file.Replayed.Exists || file.Replayed.Content == nil || *file.Replayed.Content != "# Memory\n\n## Index\n- two\n" {
		t.Fatalf("replayed side = %+v, want the local commit content", file.Replayed)
	}
	if !file.Onto.Exists || file.Onto.Content == nil {
		t.Fatalf("onto side = %+v, want the upstream baseline content", file.Onto)
	}

	// 2. The operator picks the upstream side. resolvedFiles covers every path
	// of the stage. Keeping the upstream side leaves device1 on the remote
	// history, so a fresh conflict can be produced for the abort step; the
	// replayed-side default is unit tested in web/src/conflicts.test.js.
	resolved := make([]map[string]any, 0, len(view.Files))
	for _, path := range view.Files {
		if path.Onto.Exists && path.Onto.Content != nil {
			resolved = append(resolved, map[string]any{
				"path": path.Path, "action": "put", "rawContent": *path.Onto.Content,
			})
			continue
		}
		resolved = append(resolved, map[string]any{"path": path.Path, "action": "delete"})
	}
	body, err := json.Marshal(map[string]any{
		"conflictId":    view.ConflictID,
		"baseRevision":  view.BaseRevision,
		"resolvedFiles": resolved,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedRes := fixture.send(t, http.MethodPost, "/api/v1/conflict:resolve", string(body), nil)
	mustStatus(t, resolvedRes.status, http.StatusOK)
	var outcome struct {
		RemoteState string               `json:"remoteState"`
		Conflict    *conflictViewPayload `json:"conflict"`
	}
	if err := json.Unmarshal(resolvedRes.body, &outcome); err != nil {
		t.Fatalf("decode resolve: %v\n%s", err, resolvedRes.body)
	}
	if outcome.Conflict != nil || outcome.RemoteState != "synced" {
		t.Fatalf("resolve outcome = %+v, want a finished replay and a synced remote", outcome)
	}
	if got := mustReadFile(t, filepath.Join(f.repo2, "MEMORY.md")); got != "# Memory\n\n## Index\n- one\n" {
		t.Fatalf("resolved content = %q", got)
	}

	// 3. A fresh conflict is aborted through the view's abort action.
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
	var second conflictViewPayload
	if err := json.Unmarshal(active.body, &second); err != nil {
		t.Fatalf("decode second conflict: %v\n%s", err, active.body)
	}
	abortBody, err := json.Marshal(map[string]any{"conflictId": second.ConflictID})
	if err != nil {
		t.Fatal(err)
	}
	aborted := fixture.send(t, http.MethodPost, "/api/v1/conflict:abort", string(abortBody), nil)
	mustStatus(t, aborted.status, http.StatusOK)
	if head := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD"); head != originalHead {
		t.Fatalf("abort did not restore HEAD: %s -> %s", originalHead, head)
	}
	none := fixture.get(t, "/api/v1/conflict")
	mustStatus(t, none.status, http.StatusNoContent)
}

// TestC2UiAuditRevertFlowCoversIntegration walks the audit view's revert
// sequence: list commits with their parents, preview the diff, read the current
// repository revision and post the revert. It also checks the read-only gate
// the view relies on instead of trusting a hidden button.
func TestC2UiAuditRevertFlowCoversIntegration(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- v1\n")
	fixture.update(t, "alpha.md", "# Alpha\n\n- v2\n")
	fixture.write(t, "beta.md", "# Beta\n\n- b\n")

	// 1. The audit view lists commits; parents decide revertability, so a root
	// commit (0 parents) and a merge commit (2+ parents) are gated in the UI.
	var page struct {
		Commits []struct {
			Hash    string   `json:"hash"`
			Parents []string `json:"parents"`
			Subject string   `json:"subject"`
			Files   []string `json:"files"`
			Alerts  []string `json:"alerts"`
		} `json:"commits"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=50"), &page)
	if len(page.Commits) == 0 {
		t.Fatal("audit returned no commits")
	}
	rootCount := 0
	var target string
	for _, commit := range page.Commits {
		if len(commit.Parents) == 0 {
			rootCount++
		}
		// The component gates the action on parents; pick the newest
		// single-parent commit that only touches alpha.md (the v2 update) so
		// the reverted path is stable.
		if target == "" && len(commit.Parents) == 1 && len(commit.Files) == 1 && commit.Files[0] == "alpha.md" {
			target = commit.Hash
		}
	}
	if rootCount != 1 {
		t.Fatalf("expected exactly one root commit, got %d", rootCount)
	}
	if target == "" {
		t.Fatal("audit did not expose a single-parent commit to revert")
	}

	// 2. The diff preview the view shows before confirming a revert.
	var diff struct {
		Hash  string `json:"hash"`
		Files []struct {
			Path  string `json:"path"`
			Patch string `json:"patch"`
		} `json:"files"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit/"+target+"/diff"), &diff)
	if diff.Hash != target || len(diff.Files) == 0 {
		t.Fatalf("diff preview = %+v, want files for %s", diff, target)
	}

	// 3. The revert uses the current repository revision as baseRevision.
	var snapshot struct {
		Revision string `json:"revision"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/snapshot"), &snapshot)
	if snapshot.Revision == "" {
		t.Fatal("snapshot carried no revision")
	}
	body, err := json.Marshal(map[string]any{"baseRevision": snapshot.Revision})
	if err != nil {
		t.Fatal(err)
	}
	res := fixture.send(t, http.MethodPost, "/api/v1/audit/"+target+"/revert", string(body), nil)
	mustStatus(t, res.status, http.StatusOK)
	var mutation struct {
		Commit       string   `json:"commit"`
		ChangedFiles []string `json:"changedFiles"`
		SyncState    string   `json:"syncState"`
	}
	if err := json.Unmarshal(res.body, &mutation); err != nil {
		t.Fatalf("decode revert: %v\n%s", err, res.body)
	}
	if mutation.Commit == "" || len(mutation.ChangedFiles) != 1 || mutation.ChangedFiles[0] != "alpha.md" {
		t.Fatalf("revert result = %+v, want a new commit touching alpha.md", mutation)
	}
	if got := mustReadFile(t, filepath.Join(fixture.repo, "alpha.md")); got != "# Alpha\n\n- v1\n" {
		t.Fatalf("reverted content = %q", got)
	}

	// 4. A read-only server rejects the same request, so the view never relies
	// on a disabled button for the A03 boundary.
	readOnly := newB1Fixture(t, true)
	readOnly.write(t, "alpha.md", "# Alpha\n\n- v1\n")
	readOnly.update(t, "alpha.md", "# Alpha\n\n- v2\n")
	readOnlyTarget := gitOutput(t, os.Environ(), readOnly.repo, "rev-parse", "HEAD")
	blocked := readOnly.send(t, http.MethodPost, "/api/v1/audit/"+readOnlyTarget+"/revert",
		`{"baseRevision":"`+readOnly.revisionOf(t)+`"}`, nil)
	mustStatus(t, blocked.status, http.StatusForbidden)
	mustCode(t, errorCode(t, blocked), "READ_ONLY")
}
