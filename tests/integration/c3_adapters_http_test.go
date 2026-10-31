package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/adapters"
	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/server"
	"github.com/gray0128/memctl/internal/vault"
)

// This file verifies stage C-3 subtask 2 (T033) from
// docs/05-validation-and-delivery.md through the HTTP handler: A03 (read-only
// interception of the install route), I01 and I02 (end-to-end probe, preview
// and install over HTTP, idempotence, config baseline conflicts and manual
// patches) and the A01/A02 route, token, Host and Origin regression. Every run
// uses a synthetic host executable and an isolated HOME and state root, so no
// real agent configuration or host binary is touched.

type c3HTTPFixture struct {
	server *server.Server
	home   string
	state  string
}

// newC3HTTPFixture builds a server whose adapter environment only knows the
// named hosts and reports a fixed version. Plugin manifests are absent, so a
// host that requires a plugin is reported as needs_attention.
func newC3HTTPFixture(t *testing.T, readOnly bool, hosts ...string) *c3HTTPFixture {
	t.Helper()
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	state := filepath.Join(home, ".agent-memory")
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Memctl Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "memctl-test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Memctl Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "memctl-test@example.invalid")

	cfg := config.Config{RepoPath: config.Canonical(repo), StateDir: config.Canonical(state)}
	application := app.New(cfg)
	if _, err := application.Init(context.Background(), app.InitOptions{}); err != nil {
		t.Fatalf("init repository: %v", err)
	}
	env := syntheticAdapterEnv(home, state, hosts)
	srv, err := server.New(server.Options{
		App:      application,
		Addr:     "127.0.0.1:18420",
		ReadOnly: readOnly,
		Token:    b1Token,
		Adapters: &env,
	})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	return &c3HTTPFixture{server: srv, home: home, state: state}
}

func syntheticAdapterEnv(home, state string, hosts []string) adapters.Env {
	known := map[string]bool{}
	for _, host := range hosts {
		known[host] = true
	}
	return adapters.Env{
		Home:     home,
		StateDir: state,
		LookPath: func(name string) (string, error) {
			if known[name] {
				return filepath.Join(home, "bin", name), nil
			}
			return "", os.ErrNotExist
		},
		Run: func(_ context.Context, path string, _ ...string) ([]byte, error) {
			return []byte(filepath.Base(path) + " 9.9.9\n"), nil
		},
		ReadFile: os.ReadFile,
		Stat:     os.Stat,
	}
}

func (f *c3HTTPFixture) do(method, target, token, host, origin, body string) b1Response {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
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
	data, _ := io.ReadAll(recorder.Body)
	return b1Response{status: result.StatusCode, header: result.Header, body: data}
}

// api is the common valid API call: correct Host, token and no cross-origin
// Origin header.
func (f *c3HTTPFixture) api(method, target, body string) b1Response {
	return f.do(method, "http://127.0.0.1:18420"+target, b1Token, "127.0.0.1:18420", "", body)
}

func (f *c3HTTPFixture) settingsPath() string {
	return filepath.Join(f.home, ".claude", "settings.json")
}

func (f *c3HTTPFixture) writeSettings(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.settingsPath()), 0o755); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}
	if err := os.WriteFile(f.settingsPath(), []byte(content), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func (f *c3HTTPFixture) readSettings() string {
	data, err := os.ReadFile(f.settingsPath())
	if err != nil {
		return ""
	}
	return string(data)
}

type c3AdapterStatus struct {
	Agent        string   `json:"agent"`
	HostVersion  string   `json:"hostVersion"`
	ConfigPaths  []string `json:"configPaths"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
	Reason       string   `json:"reason"`
}

type c3AdapterArtifact struct {
	Path            string `json:"path"`
	Format          string `json:"format"`
	Action          string `json:"action"`
	CurrentRevision string `json:"currentRevision"`
	PlannedRevision string `json:"plannedRevision"`
	Note            string `json:"note"`
}

type c3AdapterPlan struct {
	Agent      string              `json:"agent"`
	Status     string              `json:"status"`
	Applicable bool                `json:"applicable"`
	Artifacts  []c3AdapterArtifact `json:"artifacts"`
}

type c3AdapterResult struct {
	DryRun  bool             `json:"dryRun"`
	Status  c3AdapterStatus  `json:"status"`
	Plan    c3AdapterPlan    `json:"plan"`
	Applied []map[string]any `json:"applied"`
	Manual  []map[string]any `json:"manual"`
	Skipped []string         `json:"skipped"`
}

func findAdapter(t *testing.T, statuses []c3AdapterStatus, agent string) c3AdapterStatus {
	t.Helper()
	for _, status := range statuses {
		if status.Agent == agent {
			return status
		}
	}
	t.Fatalf("adapter %s not found in %+v", agent, statuses)
	return c3AdapterStatus{}
}

// TestC3AdaptersHTTPInstallCoversI01I02 validates the documented probe,
// preview and install flow over HTTP, including idempotence.
func TestC3AdaptersHTTPInstallCoversI01I02(t *testing.T) {
	fixture := newC3HTTPFixture(t, false, "claude")

	res := fixture.api(http.MethodGet, "/api/v1/adapters", "")
	mustStatus(t, res.status, http.StatusOK)
	var list struct {
		Adapters []c3AdapterStatus `json:"adapters"`
	}
	if err := json.Unmarshal(res.body, &list); err != nil {
		t.Fatalf("decode adapters: %v\n%s", err, res.body)
	}
	if len(list.Adapters) != 11 {
		t.Fatalf("expected 11 registered adapters, got %d", len(list.Adapters))
	}
	claude := findAdapter(t, list.Adapters, "claude-code")
	if claude.Status != "not_installed" {
		t.Fatalf("claude-code should start not_installed, got %q (%s)", claude.Status, claude.Reason)
	}
	if claude.HostVersion != "9.9.9" {
		t.Fatalf("host version should be probed, got %q", claude.HostVersion)
	}
	if len(claude.Capabilities) != 1 || claude.Capabilities[0] != "hooks" {
		t.Fatalf("claude-code capabilities should be [hooks], got %v", claude.Capabilities)
	}
	// docs/08-pi-plugin.md section 7.2: a missing pi executable is
	// not_installed; the install hint is only for a detected host without the
	// native plugin.
	if pi := findAdapter(t, list.Adapters, "pi"); pi.Status != "not_installed" {
		t.Fatalf("pi should be not_installed without the host, got %q", pi.Status)
	}

	// A dry run returns the plan and writes nothing.
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusOK)
	var preview c3AdapterResult
	if err := json.Unmarshal(res.body, &preview); err != nil {
		t.Fatalf("decode preview: %v\n%s", err, res.body)
	}
	if !preview.DryRun || len(preview.Plan.Artifacts) != 1 || preview.Plan.Artifacts[0].Action != "create" {
		t.Fatalf("unexpected dry-run plan: %+v", preview)
	}
	if fixture.readSettings() != "" {
		t.Fatal("dry run wrote the configuration")
	}

	// A real install with no existing targets needs no baseline.
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"expectedConfigRevisions":{}}`)
	mustStatus(t, res.status, http.StatusOK)
	var installed c3AdapterResult
	if err := json.Unmarshal(res.body, &installed); err != nil {
		t.Fatalf("decode install: %v\n%s", err, res.body)
	}
	if installed.Status.Status != "configured" {
		t.Fatalf("install should report configured, got %q (%s)", installed.Status.Status, installed.Status.Reason)
	}
	if len(installed.Applied) != 1 {
		t.Fatalf("install should apply one artifact, got %+v", installed.Applied)
	}
	first := fixture.readSettings()
	if !strings.Contains(first, "memctl") {
		t.Fatalf("installed configuration is missing the managed commands:\n%s", first)
	}

	// A second preview reports an idempotent plan with the current revision.
	res = fixture.api(http.MethodGet, "/api/v1/adapters", "")
	if err := json.Unmarshal(res.body, &list); err != nil {
		t.Fatalf("decode adapters: %v", err)
	}
	if claude = findAdapter(t, list.Adapters, "claude-code"); claude.Status != "configured" {
		t.Fatalf("claude-code should be configured after install, got %q", claude.Status)
	}
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusOK)
	if err := json.Unmarshal(res.body, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Plan.Artifacts[0].Action != "skip" || preview.Plan.Artifacts[0].CurrentRevision == "" {
		t.Fatalf("second preview should skip with a baseline, got %+v", preview.Plan.Artifacts[0])
	}
	baseline, err := json.Marshal(map[string]any{
		"expectedConfigRevisions": map[string]string{
			preview.Plan.Artifacts[0].Path: preview.Plan.Artifacts[0].CurrentRevision,
		},
	})
	if err != nil {
		t.Fatalf("encode baseline: %v", err)
	}
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", string(baseline))
	mustStatus(t, res.status, http.StatusOK)
	if err := json.Unmarshal(res.body, &installed); err != nil {
		t.Fatalf("decode install: %v", err)
	}
	if len(installed.Applied) != 0 || len(installed.Skipped) != 1 {
		t.Fatalf("second install should skip, got applied=%+v skipped=%v", installed.Applied, installed.Skipped)
	}
	if second := fixture.readSettings(); second != first {
		t.Fatalf("second install changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	// An unregistered adapter is rejected.
	res = fixture.api(http.MethodPost, "/api/v1/adapters/unknown/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusUnprocessableEntity)
	mustCode(t, errorCode(t, res), "UNSUPPORTED")
}

// TestC3AdaptersHTTPBaselineCoversI02 validates the config baseline conflict,
// the missing-baseline precondition and the manual patch fallback.
func TestC3AdaptersHTTPBaselineCoversI02(t *testing.T) {
	fixture := newC3HTTPFixture(t, false, "claude")
	fixture.writeSettings(t, `{
  "model": "opus",
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "user-stop-hook" }] }
    ]
  }
}
`)

	res := fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusOK)
	var preview c3AdapterResult
	if err := json.Unmarshal(res.body, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Plan.Artifacts[0].Action != "update" || preview.Plan.Artifacts[0].CurrentRevision == "" {
		t.Fatalf("expected an update with a baseline, got %+v", preview.Plan.Artifacts[0])
	}
	staleRevision := preview.Plan.Artifacts[0].CurrentRevision
	path := preview.Plan.Artifacts[0].Path
	before := fixture.readSettings()

	// A missing baseline is a precondition problem and changes nothing.
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{}`)
	mustStatus(t, res.status, http.StatusPreconditionRequired)
	mustCode(t, errorCode(t, res), "PRECONDITION_REQUIRED")
	if fixture.readSettings() != before {
		t.Fatal("a missing baseline modified the file")
	}

	// The file changed after the preview, so the install is rejected with 412.
	fixture.writeSettings(t, before+"\n")
	changed := fixture.readSettings()
	payload, err := json.Marshal(map[string]any{
		"expectedConfigRevisions": map[string]string{path: staleRevision},
	})
	if err != nil {
		t.Fatalf("encode baseline: %v", err)
	}
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", string(payload))
	mustStatus(t, res.status, http.StatusPreconditionFailed)
	mustCode(t, errorCode(t, res), "REVISION_MISMATCH")
	if fixture.readSettings() != changed {
		t.Fatal("a stale baseline modified the file")
	}

	// A structure that cannot be merged safely returns a manual patch and never
	// writes the file.
	fixture.writeSettings(t, "[]\n")
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusOK)
	if err := json.Unmarshal(res.body, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Plan.Status != "needs_attention" || preview.Plan.Artifacts[0].Action != "manual" {
		t.Fatalf("expected a manual plan, got %+v", preview.Plan)
	}
	payload, err = json.Marshal(map[string]any{
		"expectedConfigRevisions": map[string]string{
			preview.Plan.Artifacts[0].Path: preview.Plan.Artifacts[0].CurrentRevision,
		},
	})
	if err != nil {
		t.Fatalf("encode baseline: %v", err)
	}
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", string(payload))
	mustStatus(t, res.status, http.StatusOK)
	var manual c3AdapterResult
	if err := json.Unmarshal(res.body, &manual); err != nil {
		t.Fatalf("decode install: %v", err)
	}
	if len(manual.Manual) != 1 || len(manual.Applied) != 0 {
		t.Fatalf("manual plan should not write, got applied=%+v manual=%+v", manual.Applied, manual.Manual)
	}
	if fixture.readSettings() != "[]\n" {
		t.Fatal("a manual plan modified the file")
	}
}

// TestC3AdaptersHTTPRecoversInterruptedInstallCoversI02 validates that a real
// install recovers the interrupted install first: the preview reports the
// pending recovery without touching it, and the install restores the user's
// content before merging the managed entries back in.
type c3RecoveryResult struct {
	Recovered bool     `json:"recovered"`
	Restored  []string `json:"restored"`
	Removed   []string `json:"removed"`
	Skipped   []string `json:"skipped"`
}

func TestC3AdaptersHTTPRecoversInterruptedInstallCoversI02(t *testing.T) {
	fixture := newC3HTTPFixture(t, false, "claude")

	// Simulate an interrupted install: the configuration already holds the
	// managed entries, a backup holds the user's original file and the journal
	// is still on disk.
	user := `{
  "model": "opus",
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "user-stop-hook" }] }
    ]
  }
}
`
	installed := user[:len(user)-2] + `,
  "managed": true
}
`
	if err := os.MkdirAll(filepath.Dir(fixture.settingsPath()), 0o755); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}
	if err := os.WriteFile(fixture.settingsPath(), []byte(installed), 0o644); err != nil {
		t.Fatalf("write interrupted settings: %v", err)
	}
	backupDir := filepath.Join(fixture.state, "adapters", "backups", "interrupted")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup directory: %v", err)
	}
	backupPath := filepath.Join(backupDir, "00-settings.json")
	if err := os.WriteFile(backupPath, []byte(user), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	journal := map[string]any{
		"installId": "interrupted",
		"startedAt": "2026-10-17T00:00:00Z",
		"agent":     "claude-code",
		"files": []map[string]any{{
			"path":              fixture.settingsPath(),
			"existed":           true,
			"backupPath":        backupPath,
			"mode":              "0644",
			"installedRevision": vault.FileRevision("100644", []byte(installed)),
			"plannedRevision":   vault.FileRevision("100644", []byte(installed)),
			"applied":           true,
		}},
	}
	encoded, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		t.Fatalf("encode journal: %v", err)
	}
	journalPath := filepath.Join(fixture.state, "adapters", "install.json")
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	// The preview reports the pending recovery and leaves the interrupted files
	// untouched.
	res := fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusOK)
	var preview struct {
		PendingRecovery bool `json:"pendingRecovery"`
	}
	if err := json.Unmarshal(res.body, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.PendingRecovery {
		t.Fatal("the preview should report the pending recovery")
	}
	if fixture.readSettings() != installed {
		t.Fatal("the dry run recovered the interrupted install")
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("the dry run removed the journal: %v", err)
	}

	// The real install recovers the user's file, then merges the managed entries
	// back in. The recovered preview baseline is replaced, so no baseline is
	// required for this request.
	res = fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{}`)
	mustStatus(t, res.status, http.StatusOK)
	var result struct {
		Status   c3AdapterStatus   `json:"status"`
		Recovery *c3RecoveryResult `json:"recovery"`
		Applied  []map[string]any  `json:"applied"`
	}
	if err := json.Unmarshal(res.body, &result); err != nil {
		t.Fatalf("decode install: %v", err)
	}
	if result.Recovery == nil || !result.Recovery.Recovered || len(result.Recovery.Restored) != 1 {
		t.Fatalf("install should report the recovery, got %+v", result.Recovery)
	}
	if result.Status.Status != "configured" || len(result.Applied) != 1 {
		t.Fatalf("install after recovery should configure the adapter, got %+v", result)
	}
	final := fixture.readSettings()
	if !strings.Contains(final, "user-stop-hook") {
		t.Fatalf("recovery lost the user hook:\n%s", final)
	}
	if strings.Count(final, "memctl") != 3 {
		t.Fatalf("install after recovery should have three managed commands:\n%s", final)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("the journal should be cleared after recovery: %v", err)
	}
}

// TestC3AdaptersHTTPSecurityCoversA02A03 validates that the adapter routes obey
// the shared HTTP security rules and the read-only gate.
func TestC3AdaptersHTTPSecurityCoversA02A03(t *testing.T) {
	readOnly := newC3HTTPFixture(t, true, "claude")
	mustStatus(t, readOnly.api(http.MethodGet, "/api/v1/adapters", "").status, http.StatusOK)
	res := readOnly.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRun": true}`)
	mustStatus(t, res.status, http.StatusForbidden)
	mustCode(t, errorCode(t, res), "READ_ONLY")
	if readOnly.readSettings() != "" {
		t.Fatal("the read-only server wrote a configuration")
	}

	fixture := newC3HTTPFixture(t, false, "claude")
	missingToken := fixture.do(http.MethodGet, "http://127.0.0.1:18420/api/v1/adapters", "", "127.0.0.1:18420", "", "")
	mustStatus(t, missingToken.status, http.StatusUnauthorized)
	mustCode(t, errorCode(t, missingToken), "AUTH_REQUIRED")

	crossOrigin := fixture.do(http.MethodPost, "http://127.0.0.1:18420/api/v1/adapters/claude-code/install", b1Token,
		"127.0.0.1:18420", "https://evil.example", `{"dryRun": true}`)
	mustStatus(t, crossOrigin.status, http.StatusForbidden)
	mustCode(t, errorCode(t, crossOrigin), "ORIGIN_DENIED")

	fakeHost := fixture.do(http.MethodGet, "http://127.0.0.1:18420/api/v1/adapters", b1Token, "evil.example", "", "")
	mustStatus(t, fakeHost.status, http.StatusBadRequest)

	// A GET route rejects an undeclared method instead of running the handler.
	wrongMethod := fixture.api(http.MethodDelete, "/api/v1/adapters", "")
	mustStatus(t, wrongMethod.status, http.StatusMethodNotAllowed)

	// An unknown field in an otherwise valid request is a parameter error.
	unknownField := fixture.api(http.MethodPost, "/api/v1/adapters/claude-code/install", `{"dryRunTo": true}`)
	mustStatus(t, unknownField.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, unknownField), "INVALID_ARGUMENT")
}
