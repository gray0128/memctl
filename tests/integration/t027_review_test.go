package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
)

// This file verifies the T027 review closure over HTTP: the audit before
// cursor is validated and cannot be used as a Git option, agent-filtered audit
// pages scan past non-matching commits without returning empty pages, the
// commit source trailer records the device, and a protected state serves the
// last complete snapshot as stale.

func applyAs(t *testing.T, fixture *b1Fixture, path, content, agent, device string) string {
	t.Helper()
	result, err := fixture.app.Apply(context.Background(), app.MutationRequest{
		Changes: []app.FileChange{{
			Path:             path,
			ExpectedRevision: "absent",
			Action:           app.ActionPut,
			RawContent:       content,
		}},
		Agent:  agent,
		Device: device,
	})
	if err != nil {
		t.Fatalf("apply %s: %v", path, err)
	}
	return result.Commit
}

func TestT027AuditBeforeIsValidated(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	out := filepath.Join(t.TempDir(), "leak.txt")
	res := fixture.get(t, "/api/v1/audit?before=--output="+out)
	mustStatus(t, res.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, res), "INVALID_ARGUMENT")
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("git wrote %s from an option-like before value", out)
	}

	// A non-hex cursor is a parameter error, not a Git failure.
	res = fixture.get(t, "/api/v1/audit?before=not-a-hash")
	mustStatus(t, res.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, res), "INVALID_ARGUMENT")

	// A real commit id still works as a pagination cursor, and the last page
	// does not advertise a cursor.
	var page struct {
		Commits    []struct {
			Hash string `json:"hash"`
		} `json:"commits"`
		NextCursor string `json:"nextCursor"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1"), &page)
	if len(page.Commits) != 1 {
		t.Fatalf("expected one commit, got %+v", page)
	}
	var next struct {
		Commits    []struct {
			Hash string `json:"hash"`
		} `json:"commits"`
		NextCursor string `json:"nextCursor"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1&before="+page.Commits[0].Hash), &next)
	if len(next.Commits) != 1 || next.NextCursor != "" {
		t.Fatalf("the last page must return the remaining commit without a cursor: %+v", next)
	}

	// A cursor at the oldest commit returns an empty page and, crucially, does
	// not echo the cursor back, which would make the client loop forever.
	var exhausted struct {
		Commits    []struct {
			Hash string `json:"hash"`
		} `json:"commits"`
		NextCursor string `json:"nextCursor"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1&before="+next.Commits[0].Hash), &exhausted)
	if len(exhausted.Commits) != 0 || exhausted.NextCursor != "" {
		t.Fatalf("a cursor past the oldest commit must not return a cursor: %+v", exhausted)
	}
}

func TestT027AuditAgentPaginationAndDevice(t *testing.T) {
	fixture := newB1Fixture(t, false)
	alphaHash := applyAs(t, fixture, "alpha.md", "# Alpha\n\n- alpha fact\n", "alpha", "laptop")
	applyAs(t, fixture, "beta-1.md", "# Beta 1\n\n- beta fact\n", "beta", "server")
	applyAs(t, fixture, "beta-2.md", "# Beta 2\n\n- beta fact\n", "beta", "server")
	applyAs(t, fixture, "beta-3.md", "# Beta 3\n\n- beta fact\n", "beta", "server")

	var page struct {
		Commits []struct {
			Hash   string `json:"hash"`
			Agent  string `json:"agent"`
			Device string `json:"device"`
		} `json:"commits"`
		NextCursor string `json:"nextCursor"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?agent=alpha&limit=1"), &page)
	if len(page.Commits) != 1 || page.Commits[0].Hash != alphaHash {
		t.Fatalf("agent filter must scan past non-matching pages, got %+v", page)
	}
	if page.Commits[0].Agent != "alpha" {
		t.Fatalf("agent filter returned the wrong commit: %+v", page.Commits[0])
	}
	if page.Commits[0].Device != "laptop" {
		t.Fatalf("audit must report the recorded device, got %q", page.Commits[0].Device)
	}
	if page.NextCursor == "" {
		t.Fatalf("a full filtered page must return a cursor: %+v", page)
	}

	var last struct {
		Commits []struct {
			Hash string `json:"hash"`
		} `json:"commits"`
		NextCursor string `json:"nextCursor"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?agent=alpha&limit=1&before="+page.NextCursor), &last)
	if len(last.Commits) != 0 || last.NextCursor != "" {
		t.Fatalf("no older alpha commit should remain: %+v", last)
	}
}

func TestT027SnapshotIsStaleInProtectedState(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	var snapshot struct {
		Revision string `json:"revision"`
		Stale    bool   `json:"stale"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/snapshot"), &snapshot)
	if snapshot.Stale {
		t.Fatal("a clean repository must not be stale")
	}

	gitDir, err := git.GitDir(context.Background(), fixture.repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("hold the repository lock: ok=%v err=%v", ok, err)
	}
	defer held.Release()

	stale := fixture.get(t, "/api/v1/snapshot")
	mustStatus(t, stale.status, http.StatusOK)
	fixture.decode(t, stale, &snapshot)
	if !snapshot.Stale {
		t.Fatalf("a protected state must return a stale snapshot: %+v", snapshot)
	}
}
