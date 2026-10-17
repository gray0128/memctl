package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// This file registers the regression checks of T039, the fixes for the state
// consistency defects found by the ego-browser end-to-end UI/UX verification
// (docs/validation/ui-ux-verification.md):
//
//   - SyncSummary.aheadCount/behindCount must be null when no upstream is
//     configured, so the status bar shows「未知」instead of a fake zero.
//   - A finished conflict resolution must refresh the cached sync state, so the
//     snapshot stops reporting the conflict that was just cleared.

type t039SyncState struct {
	HasUpstream bool   `json:"hasUpstream"`
	RemoteState string `json:"remoteState"`
	AheadCount  *int   `json:"aheadCount"`
	BehindCount *int   `json:"behindCount"`
}

func (f *b1Fixture) syncStateOf(t *testing.T) t039SyncState {
	t.Helper()
	var snapshot struct {
		SyncState t039SyncState `json:"syncState"`
	}
	f.decode(t, f.get(t, "/api/v1/snapshot"), &snapshot)
	return snapshot.SyncState
}

// TestT039SyncSummaryHasUnknownCountsWithoutUpstream covers the sync response
// contract of docs/02-domain-models-and-contracts.md section 2.2: a local-only
// repository has no meaningful ahead/behind counts, so the summary must carry
// null instead of zero.
func TestT039SyncSummaryHasUnknownCountsWithoutUpstream(t *testing.T) {
	fixture := newB1Fixture(t, false)

	res := fixture.send(t, http.MethodPost, "/api/v1/sync", "", nil)
	mustStatus(t, res.status, http.StatusAccepted)

	var summary t039SyncState
	if err := json.Unmarshal(res.body, &summary); err != nil {
		t.Fatalf("decode sync summary: %v\n%s", err, res.body)
	}
	if summary.HasUpstream {
		t.Fatalf("a local-only repository must not report an upstream: %+v", summary)
	}
	if summary.RemoteState != "local_only" {
		t.Fatalf("remoteState = %q, want local_only", summary.RemoteState)
	}
	if summary.AheadCount != nil {
		t.Fatalf("aheadCount = %d, want null without an upstream", *summary.AheadCount)
	}
	if summary.BehindCount != nil {
		t.Fatalf("behindCount = %d, want null without an upstream", *summary.BehindCount)
	}

	// The snapshot already reported the same unknown counts; the sync response
	// must not disagree with it.
	snapshotState := fixture.syncStateOf(t)
	if snapshotState.AheadCount != nil || snapshotState.BehindCount != nil {
		t.Fatalf("snapshot ahead/behind = %v/%v, want null", snapshotState.AheadCount, snapshotState.BehindCount)
	}
}

// TestT039ConflictResolveRefreshesSyncState covers the state cache after a
// conflict is resolved through the documented HTTP route: the cached conflict
// record must not outlive the conflict, or the status bar keeps reporting
// 「冲突」and a non-zero ahead/behind after a successful push.
func TestT039ConflictResolveRefreshesSyncState(t *testing.T) {
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
		Files        []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
			Onto struct {
				Exists  bool    `json:"exists"`
				Content *string `json:"content"`
			} `json:"onto"`
		} `json:"files"`
	}
	if err := json.Unmarshal(res.body, &view); err != nil {
		t.Fatalf("decode conflict: %v\n%s", err, res.body)
	}
	if view.ConflictID == "" || view.BaseRevision == "" {
		t.Fatalf("conflict snapshot incomplete: %+v", view)
	}

	resolvedFiles := make([]map[string]any, 0, len(view.Files))
	for _, file := range view.Files {
		if file.Onto.Exists && file.Onto.Content != nil {
			resolvedFiles = append(resolvedFiles, map[string]any{
				"path": file.Path, "action": "put", "rawContent": *file.Onto.Content,
			})
			continue
		}
		resolvedFiles = append(resolvedFiles, map[string]any{"path": file.Path, "action": "delete"})
	}
	body, err := json.Marshal(map[string]any{
		"conflictId":    view.ConflictID,
		"baseRevision":  view.BaseRevision,
		"resolvedFiles": resolvedFiles,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := fixture.send(t, http.MethodPost, "/api/v1/conflict:resolve", string(body), nil)
	mustStatus(t, resolved.status, http.StatusOK)

	after := fixture.syncStateOf(t)
	if after.RemoteState != "synced" {
		t.Fatalf("remoteState after resolve = %q, want synced", after.RemoteState)
	}
	if !after.HasUpstream {
		t.Fatalf("a resolved and pushed replay must keep its upstream: %+v", after)
	}
	if after.AheadCount == nil || *after.AheadCount != 0 || after.BehindCount == nil || *after.BehindCount != 0 {
		t.Fatalf("ahead/behind after resolve = %v/%v, want 0/0", after.AheadCount, after.BehindCount)
	}
}
