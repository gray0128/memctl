package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/server"
)

// This file verifies stage B-1 (T023) from docs/05-validation-and-delivery.md
// through the HTTP handler: A01 (route registration and method matching), A02
// (token, Origin and Host enforcement), A03 (server-side read-only gate) and
// the HTTP part of P01-P03 (path boundary, symlink refusal and request limits).
// Every run uses a temporary HOME and memory repository, so no network or real
// credential source is touched.

const b1Token = "b1-test-session-token-0123456789abcdef"

type b1Fixture struct {
	server *server.Server
	app    *app.App
	repo   string
	home   string
}

func newB1Fixture(t *testing.T, readOnly bool) *b1Fixture {
	t.Helper()
	home := t.TempDir()
	repo := filepath.Join(home, "memory")

	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Memctl Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "memctl-test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Memctl Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "memctl-test@example.invalid")

	cfg := config.Config{RepoPath: config.Canonical(repo), StateDir: filepath.Join(home, ".agent-memory")}
	application := app.New(cfg)
	if _, err := application.Init(context.Background(), app.InitOptions{}); err != nil {
		t.Fatalf("init repository: %v", err)
	}

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

func (f *b1Fixture) write(t *testing.T, path, content string) {
	t.Helper()
	if _, err := f.app.Apply(context.Background(), app.MutationRequest{
		Changes: []app.FileChange{{
			Path:             path,
			ExpectedRevision: "absent",
			Action:           app.ActionPut,
			RawContent:       content,
		}},
	}); err != nil {
		t.Fatalf("managed write %s: %v", path, err)
	}
}

func (f *b1Fixture) update(t *testing.T, path, content string) {
	t.Helper()
	file, err := f.app.ReadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if _, err := f.app.Apply(context.Background(), app.MutationRequest{
		Changes: []app.FileChange{{
			Path:             path,
			ExpectedRevision: file.FileRevision,
			Action:           app.ActionPut,
			RawContent:       content,
		}},
	}); err != nil {
		t.Fatalf("managed update %s: %v", path, err)
	}
}

type b1Response struct {
	status int
	header http.Header
	body   []byte
}

func (f *b1Fixture) do(t *testing.T, method, target, token, host, origin string) b1Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if host != "" {
		req.Host = host
	}
	if token != "" {
		req.Header.Set("X-Memctl-Token", token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, req)
	result := recorder.Result()
	defer result.Body.Close()
	body := make([]byte, 0)
	if recorder.Body.Len() > 0 {
		body = append(body, recorder.Body.Bytes()...)
	}
	return b1Response{status: result.StatusCode, header: result.Header, body: body}
}

// get is the common valid API call: correct Host, token and same-origin.
func (f *b1Fixture) get(t *testing.T, target string) b1Response {
	t.Helper()
	return f.do(t, http.MethodGet, "http://127.0.0.1:18420"+target, b1Token, "127.0.0.1:18420", "")
}

func (f *b1Fixture) decode(t *testing.T, res b1Response, target any) {
	t.Helper()
	if res.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.status, res.body)
	}
	if err := json.Unmarshal(res.body, target); err != nil {
		t.Fatalf("decode response: %v\n%s", err, res.body)
	}
}

func errorCode(t *testing.T, res b1Response) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v\n%s", err, res.body)
	}
	return envelope.Error.Code
}

// TestB1RoutesCoversA01 registers every route and checks method matching and
// multi-segment path extraction.
func TestB1RoutesCoversA01(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first fact [source: agent://dev/claude/s1; added: 2026-01-02]\n- see [[beta]]\n")
	fixture.write(t, "beta.md", "# Beta\n\n- second fact\n")

	routes := []struct {
		target string
		check  func(status int)
	}{
		{"/api/v1/snapshot", func(status int) { mustStatus(t, status, http.StatusOK) }},
		{"/api/v1/files/alpha.md", func(status int) { mustStatus(t, status, http.StatusOK) }},
		{"/api/v1/entries", func(status int) { mustStatus(t, status, http.StatusOK) }},
		{"/api/v1/audit", func(status int) { mustStatus(t, status, http.StatusOK) }},
		{"/api/v1/doctor", func(status int) { mustStatus(t, status, http.StatusOK) }},
		// No conflict is captured in this fixture, so the route answers 204.
		{"/api/v1/conflict", func(status int) { mustStatus(t, status, http.StatusNoContent) }},
	}
	for _, route := range routes {
		res := fixture.get(t, route.target)
		route.check(res.status)
	}

	// A wrong method on a registered path is rejected by ServeMux with 405 and
	// an Allow header instead of running the GET handler.
	res := fixture.do(t, http.MethodPost, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "")
	mustStatus(t, res.status, http.StatusMethodNotAllowed)
	if allow := res.header.Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Fatalf("405 response should advertise GET, got Allow=%q", allow)
	}

	// The {path...} wildcard extracts a multi-segment path and returns the ETag.
	res = fixture.get(t, "/api/v1/files/alpha.md")
	if res.header.Get("ETag") == "" {
		t.Fatal("file read should return an ETag")
	}
	var filePayload struct {
		File struct {
			RelPath      string `json:"relPath"`
			FileRevision string `json:"fileRevision"`
			Editable     bool   `json:"editable"`
		} `json:"file"`
		RawContent string `json:"rawContent"`
	}
	fixture.decode(t, res, &filePayload)
	if filePayload.File.RelPath != "alpha.md" || !strings.Contains(filePayload.RawContent, "first fact") {
		t.Fatalf("unexpected file payload: %+v", filePayload)
	}
	if !strings.HasPrefix(filePayload.File.FileRevision, "sha256:") {
		t.Fatalf("fileRevision should be a sha256 digest, got %q", filePayload.File.FileRevision)
	}

	// The {path...} wildcard extracts a nested root-relative path.
	if err := os.MkdirAll(filepath.Join(fixture.repo, "notes"), 0o755); err != nil {
		t.Fatalf("create notes directory: %v", err)
	}
	fixture.write(t, "notes/deep.md", "# Deep\n\n- nested fact\n")
	nested := fixture.get(t, "/api/v1/files/notes/deep.md")
	fixture.decode(t, nested, &filePayload)
	if filePayload.File.RelPath != "notes/deep.md" || !strings.Contains(filePayload.RawContent, "nested fact") {
		t.Fatalf("nested file payload mismatch: %+v", filePayload)
	}

	// Unknown API path is 404, not a panic.
	mustStatus(t, fixture.get(t, "/api/v1/unknown").status, http.StatusNotFound)

	// The server exposes a 32-byte token as 64 hex characters.
	generated, err := server.New(server.Options{App: fixture.app, Addr: "127.0.0.1:18420"})
	if err != nil {
		t.Fatalf("build server with generated token: %v", err)
	}
	if len(generated.Token()) != 64 {
		t.Fatalf("session token should encode 32 bytes, got %d characters", len(generated.Token()))
	}
}

// TestB1AuthCoversA02 checks token, Origin and Host enforcement plus method
// bypass rejection.
func TestB1AuthCoversA02(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	missing := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", "", "127.0.0.1:18420", "")
	mustStatus(t, missing.status, http.StatusUnauthorized)
	mustCode(t, errorCode(t, missing), "AUTH_REQUIRED")

	wrong := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", "not-the-token", "127.0.0.1:18420", "")
	mustStatus(t, wrong.status, http.StatusUnauthorized)

	nullOrigin := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "null")
	mustStatus(t, nullOrigin.status, http.StatusForbidden)
	mustCode(t, errorCode(t, nullOrigin), "ORIGIN_DENIED")

	evilOrigin := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "https://evil.example")
	mustStatus(t, evilOrigin.status, http.StatusForbidden)
	mustCode(t, errorCode(t, evilOrigin), "ORIGIN_DENIED")

	// A fake Host is rejected before routing.
	fakeHost := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "evil.example", "")
	mustStatus(t, fakeHost.status, http.StatusBadRequest)

	// Same-origin requests with a valid token are accepted.
	sameOrigin := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "http://127.0.0.1:18420")
	mustStatus(t, sameOrigin.status, http.StatusOK)

	// PATCH cannot masquerade as a read: method matching still rejects it.
	patch := fixture.do(t, http.MethodPatch, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "")
	mustStatus(t, patch.status, http.StatusMethodNotAllowed)

	// Security headers are present on every response, including errors.
	for _, header := range []string{"Cache-Control", "X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy"} {
		if missing.header.Get(header) == "" {
			t.Fatalf("genuine response is missing header %s", header)
		}
	}
}

// TestB1ReadOnlyCoversA03 checks that the server refuses every write verb.
func TestB1ReadOnlyCoversA03(t *testing.T) {
	fixture := newB1Fixture(t, true)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	mustStatus(t, fixture.get(t, "/api/v1/snapshot").status, http.StatusOK)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		res := fixture.do(t, method, "http://127.0.0.1:18420/api/v1/snapshot", b1Token, "127.0.0.1:18420", "")
		mustStatus(t, res.status, http.StatusForbidden)
		mustCode(t, errorCode(t, res), "READ_ONLY")
	}
	// Unimplemented management routes are still rejected in read-only mode
	// instead of falling through to a method-not-allowed placeholder.
	for _, target := range []string{"/api/v1/sync", "/api/v1/doctor:fix", "/api/v1/dream:preview"} {
		res := fixture.do(t, http.MethodPost, "http://127.0.0.1:18420"+target, b1Token, "127.0.0.1:18420", "")
		mustStatus(t, res.status, http.StatusForbidden)
		mustCode(t, errorCode(t, res), "READ_ONLY")
	}
}

// TestB1PathBoundaryCoversP01P02 checks traversal, Git internals and symlink
// refusal.
func TestB1PathBoundaryCoversP01P02(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	traversal := fixture.get(t, "/api/v1/files/%2e%2e%2fMEMORY.md")
	mustStatus(t, traversal.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, traversal), "INVALID_PATH")

	gitInternal := fixture.get(t, "/api/v1/files/.git/config")
	mustStatus(t, gitInternal.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, gitInternal), "INVALID_PATH")

	absolute := fixture.get(t, "/api/v1/files//etc/passwd")
	mustStatus(t, absolute.status, http.StatusBadRequest)

	drive := fixture.get(t, "/api/v1/files/C:secret")
	mustStatus(t, drive.status, http.StatusBadRequest)

	if runtime.GOOS != "windows" {
		link := filepath.Join(fixture.repo, "link.md")
		if err := os.Symlink("MEMORY.md", link); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		symlinked := fixture.get(t, "/api/v1/files/link.md")
		mustStatus(t, symlinked.status, http.StatusUnprocessableEntity)
		mustCode(t, errorCode(t, symlinked), "UNSUPPORTED")
	}
}

// TestB1LimitsCoversP03 checks that page limits and oversized reads fail with a
// bounded error instead of truncation.
func TestB1LimitsCoversP03(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	over := fixture.get(t, "/api/v1/entries?limit=201")
	mustStatus(t, over.status, http.StatusRequestEntityTooLarge)
	mustCode(t, errorCode(t, over), "LIMIT_EXCEEDED")

	nonNumeric := fixture.get(t, "/api/v1/entries?limit=abc")
	mustStatus(t, nonNumeric.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, nonNumeric), "INVALID_ARGUMENT")

	negative := fixture.get(t, "/api/v1/entries?offset=-1")
	mustStatus(t, negative.status, http.StatusBadRequest)

	// A file above the documented 2 MiB text limit is refused, not truncated.
	big := filepath.Join(fixture.repo, "big.md")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", 2<<20+1)), 0o644); err != nil {
		t.Fatalf("write oversized file: %v", err)
	}
	large := fixture.get(t, "/api/v1/files/big.md")
	mustStatus(t, large.status, http.StatusRequestEntityTooLarge)
	mustCode(t, errorCode(t, large), "LIMIT_EXCEEDED")
}

// TestB1SnapshotContent checks the snapshot, entries, audit and doctor payloads
// against the documented shape.
func TestB1SnapshotContent(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first fact [source: agent://dev/claude/s1; added: 2026-01-02]\n- see [[beta]]\n")
	fixture.write(t, "beta.md", "# Beta\n\n- second fact\n")
	fixture.write(t, "orphan.md", "# Orphan\n\n- unreachable\n")
	// The entry point must link alpha for it to be reachable; beta is reachable
	// through alpha.
	fixture.update(t, "MEMORY.md", "# Memory\n\n## Index\n\n- [[alpha]]\n")

	var snapshot struct {
		RepoID   string `json:"repoId"`
		Revision string `json:"revision"`
		Stale    bool   `json:"stale"`
		Files    []struct {
			RelPath       string `json:"relPath"`
			IndexedInRoot bool   `json:"indexedInRoot"`
			Entries       []struct {
				FactText string `json:"factText"`
				Sources  []struct {
					Agent     string `json:"agent"`
					SessionID string `json:"sessionId"`
				} `json:"sources"`
			} `json:"entries"`
		} `json:"files"`
		Graph struct {
			Nodes []struct {
				Path   string `json:"path"`
				Orphan bool   `json:"orphan"`
			} `json:"nodes"`
			Edges []struct {
				From   string `json:"from"`
				To     string `json:"to"`
				Exists bool   `json:"exists"`
			} `json:"edges"`
		} `json:"graph"`
		SyncState struct {
			RemoteState string `json:"remoteState"`
			LocalState  string `json:"localState"`
		} `json:"syncState"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/snapshot"), &snapshot)

	if snapshot.RepoID == "" || !strings.HasPrefix(snapshot.Revision, "sha256:") {
		t.Fatalf("snapshot should include repoId and revision: %+v", snapshot)
	}
	if snapshot.Stale {
		t.Fatal("a clean repository should not report a stale snapshot")
	}
	if snapshot.SyncState.RemoteState != "local_only" || snapshot.SyncState.LocalState != "clean" {
		t.Fatalf("unexpected sync state: %+v", snapshot.SyncState)
	}
	if snapshot.Health.Status != "warning" {
		// orphan.md is unreachable, which is a warning.
		t.Fatalf("expected warning health because of the orphan, got %q", snapshot.Health.Status)
	}

	indexed := map[string]bool{}
	orphan := map[string]bool{}
	for _, file := range snapshot.Files {
		indexed[file.RelPath] = file.IndexedInRoot
	}
	for _, node := range snapshot.Graph.Nodes {
		orphan[node.Path] = node.Orphan
	}
	if !indexed["MEMORY.md"] || !indexed["alpha.md"] || !indexed["beta.md"] {
		t.Fatalf("entry point and linked files should be indexed: %v", indexed)
	}
	if indexed["orphan.md"] || !orphan["orphan.md"] {
		t.Fatalf("unreachable file should be orphan/unindexed: indexed=%v orphan=%v", indexed, orphan)
	}

	var entries struct {
		Revision string `json:"revision"`
		Items    []struct {
			FactText string `json:"factText"`
			FilePath string `json:"filePath"`
		} `json:"items"`
		Total int `json:"total"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?q=first"), &entries)
	if entries.Total != 1 || len(entries.Items) != 1 || entries.Items[0].FilePath != "alpha.md" {
		t.Fatalf("q filter should match one entry, got %+v", entries)
	}

	var byAgent struct {
		Total int `json:"total"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?agent=claude"), &byAgent)
	if byAgent.Total != 1 {
		t.Fatalf("agent filter should match the sourced entry, got %d", byAgent.Total)
	}

	var audit struct {
		Commits []struct {
			Hash   string   `json:"hash"`
			Files  []string `json:"files"`
			Alerts []string `json:"alerts"`
		} `json:"commits"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1"), &audit)
	if len(audit.Commits) != 1 || len(audit.Commits[0].Files) == 0 {
		t.Fatalf("audit should return one commit with files: %+v", audit)
	}

	var report struct {
		Revision string `json:"revision"`
		Issues   []any  `json:"issues"`
		Counts   struct {
			Total int `json:"total"`
		} `json:"counts"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/doctor"), &report)
	if report.Revision == "" {
		t.Fatal("doctor report should include the repository revision")
	}
}

// TestB1LoopbackListenerServesRealRequests binds an ephemeral loopback port and
// drives the server over a real TCP socket, verifying that Host validation
// matches the actual listener port.
func TestB1LoopbackListenerServesRealRequests(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	listener, err := server.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := listener.Addr().String()
	srv, err := server.New(server.Options{App: fixture.app, Addr: addr, Token: b1Token})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	httpServer := srv.NewHTTPServer()
	go func() { _ = httpServer.Serve(listener) }()
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/snapshot", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Memctl-Token", b1Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 over TCP, got %d", resp.StatusCode)
	}
}

func mustStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("expected HTTP %d, got %d", want, got)
	}
}

func mustCode(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("expected error code %s, got %s", want, got)
	}
}
