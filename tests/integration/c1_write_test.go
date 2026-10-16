package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/cli"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/server"
)

// This file verifies the managed write routes of stage C-1 (T028) through the
// HTTP handler: PUT /api/v1/files/{path...}, POST /api/v1/files:move,
// POST/PATCH/DELETE /api/v1/entries, POST /api/v1/doctor:fix and
// POST /api/v1/sync. It covers A04 (If-Match, If-None-Match and cross-file
// baselines), W01-W02 (version protocol shared with the CLI) and the HTTP part
// of P01-P03 (path boundary, symlink refusal and request limits).

// send performs one authenticated same-Host API request with an optional body
// and extra headers.
func (f *b1Fixture) send(t *testing.T, method, target, body string, headers map[string]string) b1Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:18420"+target, reader)
	req.Host = "127.0.0.1:18420"
	req.Header.Set("X-Memctl-Token", b1Token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, req)
	result := recorder.Result()
	defer result.Body.Close()
	out := make([]byte, 0)
	if recorder.Body.Len() > 0 {
		out = append(out, recorder.Body.Bytes()...)
	}
	return b1Response{status: result.StatusCode, header: result.Header, body: out}
}

// revisionOf returns the current repository revision from the snapshot.
func (f *b1Fixture) revisionOf(t *testing.T) string {
	t.Helper()
	var snapshot struct {
		Revision string `json:"revision"`
	}
	f.decode(t, f.get(t, "/api/v1/snapshot"), &snapshot)
	if snapshot.Revision == "" {
		t.Fatal("snapshot did not include a revision")
	}
	return snapshot.Revision
}

// fileRevisionOf returns the ETag baseline of one file.
func (f *b1Fixture) fileRevisionOf(t *testing.T, path string) string {
	t.Helper()
	res := f.get(t, "/api/v1/files/"+path)
	if res.status != http.StatusOK {
		t.Fatalf("read %s: %d %s", path, res.status, res.body)
	}
	return strings.Trim(res.header.Get("ETag"), `"`)
}

// TestC1PutFileCoversA04AndW01 covers the PUT route: a current If-Match
// succeeds, a stale one is refused with 412, a missing precondition is 428, and
// If-None-Match: * creates only a missing file.
func TestC1PutFileCoversA04AndW01(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first\n")

	revision := fixture.fileRevisionOf(t, "alpha.md")
	updated := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- second\n"}`, map[string]string{"If-Match": `"` + revision + `"`})
	mustStatus(t, updated.status, http.StatusOK)
	var mutation struct {
		TransactionID string   `json:"transactionId"`
		Commit        string   `json:"commit"`
		ChangedFiles  []string `json:"changedFiles"`
		SyncState     string   `json:"syncState"`
	}
	if err := json.Unmarshal(updated.body, &mutation); err != nil {
		t.Fatalf("decode PUT result: %v\n%s", err, updated.body)
	}
	if mutation.Commit == "" || mutation.TransactionID == "" {
		t.Fatalf("PUT did not return a transaction: %+v", mutation)
	}
	if len(mutation.ChangedFiles) != 1 || mutation.ChangedFiles[0] != "alpha.md" {
		t.Fatalf("changedFiles = %v", mutation.ChangedFiles)
	}
	if got := mustReadFile(t, fixture.repo+"/alpha.md"); !strings.Contains(got, "- second") {
		t.Fatalf("file not updated: %q", got)
	}

	// The old baseline is now stale and must not change the file again.
	stale := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- stale\n"}`, map[string]string{"If-Match": `"` + revision + `"`})
	mustStatus(t, stale.status, http.StatusPreconditionFailed)
	mustCode(t, errorCode(t, stale), "REVISION_MISMATCH")
	if got := mustReadFile(t, fixture.repo+"/alpha.md"); !strings.Contains(got, "- second") {
		t.Fatalf("stale PUT changed the file: %q", got)
	}

	// No precondition at all is 428.
	missing := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md", `{"rawContent":"# Alpha\n"}`, nil)
	mustStatus(t, missing.status, http.StatusPreconditionRequired)
	mustCode(t, errorCode(t, missing), "PRECONDITION_REQUIRED")

	// If-None-Match: * creates a new file once and refuses an existing one.
	created := fixture.send(t, http.MethodPut, "/api/v1/files/new.md",
		`{"rawContent":"# New\n\n- created\n"}`, map[string]string{"If-None-Match": "*"})
	mustStatus(t, created.status, http.StatusOK)
	again := fixture.send(t, http.MethodPut, "/api/v1/files/new.md",
		`{"rawContent":"# New\n"}`, map[string]string{"If-None-Match": "*"})
	mustStatus(t, again.status, http.StatusPreconditionFailed)

	// An undeclared field is a parameter error, never a silent no-op.
	unknown := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n","extra":true}`, map[string]string{"If-Match": `"` + fixture.fileRevisionOf(t, "alpha.md") + `"`})
	mustStatus(t, unknown.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, unknown), "INVALID_ARGUMENT")
}

// TestC1MoveCoversLinkRewriteAndA04 covers the move route: a resolved link is
// rewritten, and a stale baseline or an existing destination is refused.
func TestC1MoveCoversLinkRewriteAndA04(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "beta.md", "# Beta\n\n- second fact\n")
	fixture.write(t, "alpha.md", "# Alpha\n\n- see [[beta]]\n")
	fixture.update(t, "MEMORY.md", "# Memory\n\n## Index\n\n- [[alpha]]\n")

	base := fixture.revisionOf(t)
	moved := fixture.send(t, http.MethodPost, "/api/v1/files:move",
		`{"fromPath":"beta.md","toPath":"gamma.md","baseRevision":"`+base+`"}`, nil)
	mustStatus(t, moved.status, http.StatusOK)
	var mutation struct {
		ChangedFiles []string `json:"changedFiles"`
		Commit       string   `json:"commit"`
	}
	if err := json.Unmarshal(moved.body, &mutation); err != nil {
		t.Fatalf("decode move result: %v\n%s", err, moved.body)
	}
	if mutation.Commit == "" {
		t.Fatalf("move did not commit: %+v", mutation)
	}
	alpha := mustReadFile(t, fixture.repo+"/alpha.md")
	if !strings.Contains(alpha, "[[gamma]]") {
		t.Fatalf("link was not rewritten: %q", alpha)
	}
	if _, err := fixture.app.ReadFile(context.Background(), "beta.md"); err == nil {
		t.Fatal("source file still readable after the move")
	}

	// A stale baseRevision cannot move another file.
	fixture.write(t, "other.md", "# Other\n\n- fact\n")
	stale := fixture.send(t, http.MethodPost, "/api/v1/files:move",
		`{"fromPath":"other.md","toPath":"renamed.md","baseRevision":"`+base+`"}`, nil)
	mustStatus(t, stale.status, http.StatusPreconditionFailed)
	mustCode(t, errorCode(t, stale), "REVISION_MISMATCH")

	// Missing baseRevision is 428; an existing destination is refused.
	missing := fixture.send(t, http.MethodPost, "/api/v1/files:move",
		`{"fromPath":"other.md","toPath":"renamed.md"}`, nil)
	mustStatus(t, missing.status, http.StatusPreconditionRequired)
	overwrite := fixture.send(t, http.MethodPost, "/api/v1/files:move",
		`{"fromPath":"other.md","toPath":"gamma.md","baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, overwrite.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, overwrite), "INVALID_ARGUMENT")

	// A traversal destination is a path error, not a Git error.
	traversal := fixture.send(t, http.MethodPost, "/api/v1/files:move",
		`{"fromPath":"other.md","toPath":"../escape.md","baseRevision":"`+fixture.revisionOf(t)+`"}`, nil)
	mustStatus(t, traversal.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, traversal), "INVALID_PATH")
}

// TestC1EntriesCoversHTTP covers the entry create, patch, cross-file move and
// delete routes with their If-Match and baseRevision preconditions.
func TestC1EntriesCoversHTTP(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first\n")
	fileRevision := fixture.fileRevisionOf(t, "alpha.md")

	created := fixture.send(t, http.MethodPost, "/api/v1/entries",
		`{"filePath":"alpha.md","factText":"second","sources":["agent://dev/claude/s1"],"expectedRevision":"`+fileRevision+`"}`, nil)
	mustStatus(t, created.status, http.StatusOK)
	var createdPayload struct {
		Entry struct {
			ID       string `json:"id"`
			FactText string `json:"factText"`
			FilePath string `json:"filePath"`
		} `json:"entry"`
		Result struct {
			Commit string `json:"commit"`
		} `json:"result"`
	}
	if err := json.Unmarshal(created.body, &createdPayload); err != nil {
		t.Fatalf("decode create: %v\n%s", err, created.body)
	}
	if createdPayload.Entry.ID == "" || createdPayload.Result.Commit == "" {
		t.Fatalf("create response incomplete: %+v", createdPayload)
	}
	if createdPayload.Entry.FactText != "second" || createdPayload.Entry.FilePath != "alpha.md" {
		t.Fatalf("unexpected created entry: %+v", createdPayload.Entry)
	}

	// Missing expectedRevision is 428.
	noPrecondition := fixture.send(t, http.MethodPost, "/api/v1/entries",
		`{"filePath":"alpha.md","factText":"x"}`, nil)
	mustStatus(t, noPrecondition.status, http.StatusPreconditionRequired)

	patchRevision := fixture.fileRevisionOf(t, "alpha.md")
	patched := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+createdPayload.Entry.ID,
		`{"filePath":"alpha.md","factText":"patched"}`, map[string]string{"If-Match": `"` + patchRevision + `"`})
	mustStatus(t, patched.status, http.StatusOK)
	if got := mustReadFile(t, fixture.repo+"/alpha.md"); !strings.Contains(got, "- patched") {
		t.Fatalf("patch did not change the fact: %q", got)
	}
	var patchedPayload struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(patched.body, &patchedPayload); err != nil {
		t.Fatal(err)
	}

	// A cross-file move requires baseRevision; the destination is created.
	moveRevision := fixture.fileRevisionOf(t, "alpha.md")
	noBase := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+patchedPayload.Entry.ID,
		`{"filePath":"alpha.md","factText":"patched","moveToFilePath":"topic.md"}`,
		map[string]string{"If-Match": `"` + moveRevision + `"`})
	mustStatus(t, noBase.status, http.StatusPreconditionRequired)

	moved := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+patchedPayload.Entry.ID,
		`{"filePath":"alpha.md","factText":"patched","moveToFilePath":"topic.md","baseRevision":"`+fixture.revisionOf(t)+`"}`,
		map[string]string{"If-Match": `"` + moveRevision + `"`})
	mustStatus(t, moved.status, http.StatusOK)
	if _, err := fixture.app.ReadFile(context.Background(), "topic.md"); err != nil {
		t.Fatalf("split target missing: %v", err)
	}
	var movedPayload struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(moved.body, &movedPayload); err != nil {
		t.Fatal(err)
	}

	// Deleting the moved entry needs its current file baseline.
	topicRevision := fixture.fileRevisionOf(t, "topic.md")
	deleted := fixture.send(t, http.MethodDelete,
		"/api/v1/entries/"+movedPayload.Entry.ID+"?filePath=topic.md", "",
		map[string]string{"If-Match": `"` + topicRevision + `"`})
	mustStatus(t, deleted.status, http.StatusOK)
	if got := mustReadFile(t, fixture.repo+"/topic.md"); strings.Contains(got, "patched") {
		t.Fatalf("delete left the entry: %q", got)
	}

	// Missing If-Match on patch and delete is 428.
	noMatch := fixture.send(t, http.MethodPatch, "/api/v1/entries/"+createdPayload.Entry.ID,
		`{"filePath":"alpha.md","factText":"x"}`, nil)
	mustStatus(t, noMatch.status, http.StatusPreconditionRequired)
}

// TestC1DoctorFixAndSyncHTTP covers the deterministic fix route and the sync
// route, plus their read-only interception.
func TestC1DoctorFixAndSyncHTTP(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	clean := fixture.send(t, http.MethodPost, "/api/v1/doctor:fix", `{"issueCodes":[]}`, nil)
	mustStatus(t, clean.status, http.StatusOK)
	var cleanPayload struct {
		Report struct {
			Revision string `json:"revision"`
		} `json:"report"`
		Result any `json:"result"`
	}
	if err := json.Unmarshal(clean.body, &cleanPayload); err != nil {
		t.Fatalf("decode doctor:fix: %v\n%s", err, clean.body)
	}
	if cleanPayload.Report.Revision == "" {
		t.Fatal("doctor:fix did not return a report")
	}
	if cleanPayload.Result != nil {
		t.Fatalf("a clean repository should not produce a fix transaction: %+v", cleanPayload.Result)
	}

	unsupported := fixture.send(t, http.MethodPost, "/api/v1/doctor:fix", `{"issueCodes":["ORPHAN_FILE"]}`, nil)
	mustStatus(t, unsupported.status, http.StatusUnprocessableEntity)
	mustCode(t, errorCode(t, unsupported), "UNSUPPORTED")

	synced := fixture.send(t, http.MethodPost, "/api/v1/sync", "", nil)
	mustStatus(t, synced.status, http.StatusAccepted)
	var summary struct {
		RemoteState string `json:"remoteState"`
		LocalState  string `json:"localState"`
	}
	if err := json.Unmarshal(synced.body, &summary); err != nil {
		t.Fatalf("decode sync: %v\n%s", err, synced.body)
	}
	if summary.RemoteState != "local_only" || summary.LocalState != "clean" {
		t.Fatalf("unexpected sync summary: %+v", summary)
	}

	// A repository whose entry file was committed as deleted is repaired.
	removeEntry := newB1Fixture(t, false)
	removeEntry.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	if err := os.Remove(removeEntry.repo + "/MEMORY.md"); err != nil {
		t.Fatal(err)
	}
	gitInRepo(t, removeEntry.repo, "add", "--", "MEMORY.md")
	gitInRepo(t, removeEntry.repo, "commit", "-q", "-m", "remove entry")
	fixed := removeEntry.send(t, http.MethodPost, "/api/v1/doctor:fix", `{"issueCodes":["ENTRY_MISSING"]}`, nil)
	mustStatus(t, fixed.status, http.StatusOK)
	var fixedPayload struct {
		Result *struct {
			Commit string `json:"commit"`
		} `json:"result"`
	}
	if err := json.Unmarshal(fixed.body, &fixedPayload); err != nil {
		t.Fatal(err)
	}
	if fixedPayload.Result == nil || fixedPayload.Result.Commit == "" {
		t.Fatalf("ENTRY_MISSING fix did not commit: %s", fixed.body)
	}
	if got := mustReadFile(t, removeEntry.repo+"/MEMORY.md"); got != "# Memory\n\n## Index\n" {
		t.Fatalf("recreated entry = %q", got)
	}

	readOnly := newB1Fixture(t, true)
	readOnly.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	revision := readOnly.fileRevisionOf(t, "alpha.md")
	for _, call := range []struct {
		method string
		target string
		body   string
		header map[string]string
	}{
		{http.MethodPut, "/api/v1/files/alpha.md", `{"rawContent":"# Alpha\n"}`, map[string]string{"If-Match": `"` + revision + `"`}},
		{http.MethodPost, "/api/v1/files:move", `{"fromPath":"alpha.md","toPath":"beta.md","baseRevision":"x"}`, nil},
		{http.MethodPost, "/api/v1/entries", `{"filePath":"alpha.md","factText":"x","expectedRevision":"x"}`, nil},
		{http.MethodPost, "/api/v1/doctor:fix", `{"issueCodes":[]}`, nil},
		{http.MethodPost, "/api/v1/sync", "", nil},
	} {
		res := readOnly.send(t, call.method, call.target, call.body, call.header)
		mustStatus(t, res.status, http.StatusForbidden)
		mustCode(t, errorCode(t, res), "READ_ONLY")
	}
}

// TestC1W02CliUiConcurrency covers W02: the CLI and the UI share one lock and
// one version protocol, so a write with the other side's stale baseline is
// refused instead of silently overwriting.
func TestC1W02CliUiConcurrency(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first\n")
	baseline := fixture.fileRevisionOf(t, "alpha.md")

	// A CLI write (the application service) commits with the shared baseline.
	if _, err := fixture.app.PutFile(context.Background(), "alpha.md", baseline,
		"# Alpha\n\n- from cli\n", "cli", "s1", ""); err != nil {
		t.Fatalf("cli write: %v", err)
	}

	// The UI write with the now-stale baseline is refused and changes nothing.
	stale := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- from ui\n"}`, map[string]string{"If-Match": `"` + baseline + `"`})
	mustStatus(t, stale.status, http.StatusPreconditionFailed)
	if got := mustReadFile(t, fixture.repo+"/alpha.md"); !strings.Contains(got, "- from cli") {
		t.Fatalf("stale UI write overwrote the CLI commit: %q", got)
	}

	// The UI write with the fresh baseline succeeds, and the CLI baseline is now
	// the stale one.
	fresh := fixture.fileRevisionOf(t, "alpha.md")
	ui := fixture.send(t, http.MethodPut, "/api/v1/files/alpha.md",
		`{"rawContent":"# Alpha\n\n- from ui\n"}`, map[string]string{"If-Match": `"` + fresh + `"`})
	mustStatus(t, ui.status, http.StatusOK)
	_, err := fixture.app.PutFile(context.Background(), "alpha.md", baseline, "# Alpha\n\n- cli again\n", "cli", "s1", "")
	requireContractCode(t, err, memerr.RevisionMismatch)
}

// TestC1WritePathBoundaryCoversP01P03 covers the write path boundary through
// HTTP: traversal, Git internals, symlinks and the request body limit.
func TestC1WritePathBoundaryCoversP01P03(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	for _, path := range []string{"%2e%2e%2fMEMORY.md", ".git%2fconfig", "C:secret"} {
		res := fixture.send(t, http.MethodPut, "/api/v1/files/"+path,
			`{"rawContent":"# X\n"}`, map[string]string{"If-None-Match": "*"})
		mustStatus(t, res.status, http.StatusBadRequest)
		mustCode(t, errorCode(t, res), "INVALID_PATH")
	}

	// A request body over the documented 8 MiB limit fails bounded.
	oversized := `{"rawContent":"` + strings.Repeat("a", int(server.DefaultMaxRequestBytes)+1) + `"}`
	res := fixture.send(t, http.MethodPut, "/api/v1/files/big.md", oversized, map[string]string{"If-None-Match": "*"})
	mustStatus(t, res.status, http.StatusRequestEntityTooLarge)
	mustCode(t, errorCode(t, res), "LIMIT_EXCEEDED")

	// A symlinked target is not editable memory.
	if err := os.Symlink("MEMORY.md", fixture.repo+"/link.md"); err == nil {
		gitInRepo(t, fixture.repo, "add", "--", "link.md")
		gitInRepo(t, fixture.repo, "commit", "-q", "-m", "add symlink")
		linked := fixture.send(t, http.MethodPut, "/api/v1/files/link.md",
			`{"rawContent":"# Link\n"}`, map[string]string{"If-None-Match": "*"})
		mustStatus(t, linked.status, http.StatusUnprocessableEntity)
		mustCode(t, errorCode(t, linked), "UNSUPPORTED")
	}
}

// gitInRepo runs one Git command inside a fixture repository.
func gitInRepo(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// requireContractCode asserts one contract error code on a direct application
// call.
func requireContractCode(t *testing.T, err error, want memerr.Code) {
	t.Helper()
	contractErr, ok := memerr.As(err)
	if !ok || contractErr.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

// TestC1MoveRouteIsRegisteredAsAction checks the action-segment route does not
// shadow the file path route and requires the write verb.
func TestC1MoveRouteIsRegisteredAsAction(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	get := fixture.get(t, "/api/v1/files:move")
	mustStatus(t, get.status, http.StatusMethodNotAllowed)
	if allow := get.header.Get("Allow"); !strings.Contains(allow, http.MethodPost) {
		t.Fatalf("405 response should advertise POST, got Allow=%q", allow)
	}
}

// TestC1DoctorFixCLI covers the `doctor --fix` wiring end to end: the read-only
// diagnosis exits 1 on a missing entry, the fix recreates it in one managed
// transaction and the following diagnosis exits 0. JSON output carries the
// report and the transaction.
func TestC1DoctorFixCLI(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	if err := os.Remove(filepath.Join(repo, "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, env, repo, "add", "--", "MEMORY.md")
	gitOutput(t, env, repo, "commit", "-q", "-m", "remove entry")

	report := runMemctl(t, bin, home, env, "doctor", "--json")
	if report.code != cli.ExitError {
		t.Fatalf("doctor after entry removal: code=%d stdout=%q stderr=%q", report.code, report.stdout, report.stderr)
	}
	if !strings.Contains(report.stdout, "ENTRY_MISSING") {
		t.Fatalf("doctor --json missing ENTRY_MISSING: %q", report.stdout)
	}

	fixed := runMemctl(t, bin, home, env, "doctor", "--fix")
	if fixed.code != cli.ExitOK {
		t.Fatalf("doctor --fix: code=%d stdout=%q stderr=%q", fixed.code, fixed.stdout, fixed.stderr)
	}
	if !strings.Contains(fixed.stdout, "applied: transaction ") {
		t.Fatalf("doctor --fix did not report the transaction: %q", fixed.stdout)
	}
	if got := mustReadFile(t, filepath.Join(repo, "MEMORY.md")); got != initialMemoryContent {
		t.Fatalf("recreated entry = %q", got)
	}

	// A separate repository exercises the JSON output of the fix.
	jsonHome := t.TempDir()
	jsonRepo := filepath.Join(jsonHome, "memory")
	jsonEnv := a2Env(jsonHome, jsonRepo, true)
	if result := runMemctl(t, bin, jsonHome, jsonEnv, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	if err := os.Remove(filepath.Join(jsonRepo, "MEMORY.md")); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, jsonEnv, jsonRepo, "add", "--", "MEMORY.md")
	gitOutput(t, jsonEnv, jsonRepo, "commit", "-q", "-m", "remove entry")
	jsonFix := runMemctl(t, bin, jsonHome, jsonEnv, "doctor", "--json", "--fix")
	if jsonFix.code != cli.ExitOK {
		t.Fatalf("doctor --json --fix: code=%d stderr=%q", jsonFix.code, jsonFix.stderr)
	}
	if !strings.Contains(jsonFix.stdout, "\"report\"") || !strings.Contains(jsonFix.stdout, "\"result\"") {
		t.Fatalf("doctor --json --fix output = %q", jsonFix.stdout)
	}
}
