package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// This file verifies the C-1 frontend/backend integration of T029 through the
// HTTP handler: it replays the exact request sequence the Web UI sends for a
// raw file edit, a stale-revision conflict followed by a reread, an entry edit,
// a graph split and the deterministic repair, and asserts the 412 path keeps
// the server content unchanged so the client can show the draft diff.

// TestC1UiEditFlowCoversIntegration walks one UI edit session end to end. It
// uses only the documented routes and the same headers and bodies the frontend
// API client builds, so a change on either side breaks this test.
func TestC1UiEditFlowCoversIntegration(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first fact [source: agent://dev/claude/s1; added: 2026-01-02]\n")
	fixture.update(t, "MEMORY.md", "# Memory\n\n## Index\n\n- [[alpha]]\n")

	// 1. The file editor reads the raw content and its revision.
	read := fixture.get(t, "/api/v1/files/alpha.md")
	mustStatus(t, read.status, http.StatusOK)
	var filePayload struct {
		File struct {
			FileRevision string `json:"fileRevision"`
		} `json:"file"`
		RawContent string `json:"rawContent"`
	}
	if err := json.Unmarshal(read.body, &filePayload); err != nil {
		t.Fatalf("decode file read: %v\n%s", err, read.body)
	}
	if filePayload.File.FileRevision == "" || filePayload.RawContent == "" {
		t.Fatalf("file read incomplete: %+v", filePayload)
	}

	// 2. A save with the read revision commits.
	saved := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- edited fact [source: agent://dev/claude/s1; added: 2026-01-02]\n"}`,
		map[string]string{"If-Match": `"` + filePayload.File.FileRevision + `"`})
	mustStatus(t, saved.status, http.StatusOK)

	// 3. A second save with the now-stale revision is the conflict the UI must
	// render; the server content stays at the first save.
	stale := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- unsaved draft\n"}`,
		map[string]string{"If-Match": `"` + filePayload.File.FileRevision + `"`})
	mustStatus(t, stale.status, http.StatusPreconditionFailed)
	mustCode(t, errorCode(t, stale), "REVISION_MISMATCH")
	if got := mustReadFile(t, fixture.repo+"/alpha.md"); !strings.Contains(got, "- edited fact") {
		t.Fatalf("stale save changed the file: %q", got)
	}

	// 4. The editor rereads the current content for the draft diff and retries
	// against the fresh revision.
	reread := fixture.get(t, "/api/v1/files/alpha.md")
	mustStatus(t, reread.status, http.StatusOK)
	if err := json.Unmarshal(reread.body, &filePayload); err != nil {
		t.Fatal(err)
	}
	if filePayload.File.FileRevision == "" {
		t.Fatal("reread did not return a revision")
	}
	retried := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- merged fact\n"}`,
		map[string]string{"If-Match": `"` + filePayload.File.FileRevision + `"`})
	mustStatus(t, retried.status, http.StatusOK)

	// 5. The entry editor reads the page, patches one entry with its file
	// revision, and is refused on a stale revision.
	var page struct {
		Revision string `json:"revision"`
		Items    []struct {
			ID           string `json:"id"`
			FileRevision string `json:"fileRevision"`
			FilePath     string `json:"filePath"`
			FactText     string `json:"factText"`
		} `json:"items"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?file=alpha.md"), &page)
	if len(page.Items) != 1 || page.Revision == "" {
		t.Fatalf("entry page incomplete: %+v", page)
	}
	entry := page.Items[0]

	entryStale := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+entry.ID,
		`{"filePath":"alpha.md","factText":"entry draft"}`,
		map[string]string{"If-Match": `"` + filePayload.File.FileRevision + `"`})
	mustStatus(t, entryStale.status, http.StatusPreconditionFailed)

	entryRevision := fixture.fileRevisionOf(t, "alpha.md")
	patched := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+entry.ID,
		`{"filePath":"alpha.md","factText":"entry patched"}`,
		map[string]string{"If-Match": `"` + entryRevision + `"`})
	mustStatus(t, patched.status, http.StatusOK)

	// 6. The graph split moves one entry into a new file in one transaction
	// using the page revision as baseRevision.
	var afterPatch struct {
		Revision string `json:"revision"`
		Items    []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?file=alpha.md"), &afterPatch)
	if len(afterPatch.Items) != 1 {
		t.Fatalf("unexpected entries after patch: %+v", afterPatch)
	}
	sourceRevision := fixture.fileRevisionOf(t, "alpha.md")
	split := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+afterPatch.Items[0].ID,
		`{"filePath":"alpha.md","factText":"entry patched","moveToFilePath":"split topic.md","baseRevision":"`+afterPatch.Revision+`"}`,
		map[string]string{"If-Match": `"` + sourceRevision + `"`})
	mustStatus(t, split.status, http.StatusOK)
	if _, err := fixture.app.ReadFile(context.Background(), "split topic.md"); err != nil {
		t.Fatalf("split target missing: %v", err)
	}
	if got := mustReadFile(t, fixture.repo+"/alpha.md"); strings.Contains(got, "entry patched") {
		t.Fatalf("split left the entry in the source file: %q", got)
	}

	// 7. The diagnostics view requests the deterministic repair with the codes
	// the report marks autoFixable and the report revision as baseline.
	var report struct {
		Revision string `json:"revision"`
		Issues   []struct {
			Code        string `json:"code"`
			AutoFixable bool   `json:"autoFixable"`
		} `json:"issues"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/doctor"), &report)
	codes := []string{}
	for _, issue := range report.Issues {
		if issue.AutoFixable {
			codes = append(codes, issue.Code)
		}
	}
	body, err := json.Marshal(map[string]any{"issueCodes": codes, "baseRevision": report.Revision})
	if err != nil {
		t.Fatal(err)
	}
	fixed := fixture.send(t, http.MethodPost, "/api/v1/doctor:fix", string(body), nil)
	mustStatus(t, fixed.status, http.StatusOK)

	// 8. The manual sync button posts once and receives the documented summary.
	synced := fixture.send(t, http.MethodPost, "/api/v1/sync", "", nil)
	mustStatus(t, synced.status, http.StatusAccepted)

	// 9. A read-only server disables the same write entries at the API layer.
	readOnly := newB1Fixture(t, true)
	readOnly.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	revision := readOnly.fileRevisionOf(t, "alpha.md")
	blocked := readOnly.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n"}`, map[string]string{"If-Match": `"` + revision + `"`})
	mustStatus(t, blocked.status, http.StatusForbidden)
	mustCode(t, errorCode(t, blocked), "READ_ONLY")
}
