package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/vault"
)

// deleteEntryRequest sends the documented DELETE with the file revision in
// If-Match, which the route requires.
func (f *b1Fixture) deleteEntryRequest(t *testing.T, id, filePath, fileRevision string) b1Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete,
		"http://127.0.0.1:18420/api/v1/entries/"+id+"?filePath="+filePath, nil)
	req.Host = "127.0.0.1:18420"
	req.Header.Set("X-Memctl-Token", b1Token)
	req.Header.Set("If-Match", `"`+fileRevision+`"`)
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, req)
	result := recorder.Result()
	defer result.Body.Close()
	data, _ := io.ReadAll(recorder.Body)
	return b1Response{status: result.StatusCode, header: result.Header, body: data}
}

// readRepoFile reads one file straight from the fixture repository, bypassing
// the read projection. The delete tests need the raw text because hiding is the
// behavior under test.
func readRepoFile(t *testing.T, repo, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestT094DeleteDeferredInsideHiddenPeriod covers the HTTP delete gate: an
// expired entry inside its 30-day hidden period is refused with
// DELETION_DEFERRED and the file is untouched. The entry ID comes from a raw
// parse because the entry is hidden from the entry list by design.
func TestT094DeleteDeferredInsideHiddenPeriod(t *testing.T) {
	fixture := newB1Fixture(t, false)
	// Five days before today: past `until`, still inside the 30-day hidden
	// period, so the delete must be deferred.
	recent := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	fixture.update(t, "MEMORY.md", "# Memory\n\n- expired [until: "+recent+"; added: 2000-01-01]\n- plain\n")

	file := fixture.get(t, "/api/v1/files/MEMORY.md")
	mustStatus(t, file.status, http.StatusOK)
	var read struct {
		File struct {
			FileRevision string `json:"fileRevision"`
		} `json:"file"`
	}
	fixture.decode(t, file, &read)

	// Locate the hidden entry through the raw file the fixture wrote: the API
	// read hides it, which is exactly why this test uses the on-disk text.
	content := readRepoFile(t, fixture.repo, "MEMORY.md")
	parsed := vault.ParseFile("MEMORY.md", read.File.FileRevision, []byte(content))
	id := ""
	for _, entry := range parsed.Entries {
		if entry.RawLine == "- expired [until: "+recent+"; added: 2000-01-01]" {
			id = entry.ID
		}
	}
	if id == "" {
		t.Fatalf("could not derive the entry id from:\n%s", content)
	}

	res := fixture.deleteEntryRequest(t, id, "MEMORY.md", read.File.FileRevision)
	mustStatus(t, res.status, http.StatusConflict)
	mustCode(t, errorCode(t, res), "DELETION_DEFERRED")
	if after := readRepoFile(t, fixture.repo, "MEMORY.md"); after != content {
		t.Fatalf("the refused delete changed the file:\n%s", after)
	}
}

// TestT094DeleteWithoutUntilStillWorks covers the default path: an entry with
// no `until` has no hidden period and keeps the documented delete behavior.
func TestT094DeleteWithoutUntilStillWorks(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.update(t, "MEMORY.md", "# Memory\n\n- plain [source: agent://dev/claude/s1; added: 2026-01-01]\n")

	var page struct {
		Items []struct {
			ID       string `json:"id"`
			FactText string `json:"factText"`
		} `json:"items"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries"), &page)
	if len(page.Items) != 1 {
		t.Fatalf("entries = %+v", page.Items)
	}
	content := readRepoFile(t, fixture.repo, "MEMORY.md")
	var file struct {
		File struct {
			FileRevision string `json:"fileRevision"`
		} `json:"file"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/files/MEMORY.md"), &file)
	res := fixture.deleteEntryRequest(t, page.Items[0].ID, "MEMORY.md", file.File.FileRevision)
	mustStatus(t, res.status, http.StatusOK)
	if after := readRepoFile(t, fixture.repo, "MEMORY.md"); after == content || strings.Contains(after, "- plain") {
		t.Fatalf("the entry was not deleted:\n%s", after)
	}
}
