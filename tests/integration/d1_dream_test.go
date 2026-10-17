package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/dream"
)

// This file verifies stage D-1 (T035) from docs/05-validation-and-delivery.md:
// D01 (a missing sandbox does not run, and an out-of-scope runner is blocked),
// D02 (a dirty repository refuses generation), D03 (out-of-scope proposals are
// rejected without touching the main repository) and the runner part of R02.
// Every run uses a temporary HOME and memory repository, a synthetic runner and
// the real platform sandbox; no network or real credential source is touched.

// dreamProposalJSON is the CLI/HTTP proposal shape this file inspects.
type dreamProposalJSON struct {
	ProposalID    string   `json:"proposalId"`
	Status        string   `json:"status"`
	PatchHash     string   `json:"patchHash"`
	Diff          string   `json:"diff"`
	Warnings      []string `json:"warnings"`
	Error         string   `json:"error"`
	AppliedCommit string   `json:"appliedCommit"`
	Changes       []struct {
		Path   string `json:"path"`
		Action string `json:"action"`
	} `json:"changes"`
}

// requireDarwinSandbox skips the isolation cases on a platform without a
// verified isolation backend. The missing-sandbox degradation itself is covered
// by internal/dream unit tests.
func requireDarwinSandbox(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the Seatbelt isolation backend is only verified on macOS")
	}
}

// dreamRepo initializes a repository and returns the binary, home, repo and
// environment. A stub `claude` script is placed first on PATH.
func dreamRepo(t *testing.T, body string) (bin, home, repo string, env []string, stubs string) {
	t.Helper()
	bin = buildMemctlBinary(t)
	home = t.TempDir()
	repo = filepath.Join(home, "memory")
	env = a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	stubs = filepath.Join(home, "stubs")
	if err := os.MkdirAll(stubs, 0o755); err != nil {
		t.Fatalf("create stub dir: %v", err)
	}
	writeStub(t, stubs, "claude", body)
	env = envWith(env, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin, home, repo, env, stubs
}

// TestD1DreamMissingRunnerCoversR02 verifies that an unavailable runner is a
// targeted error and never a fallback to another runner or an unsandboxed run.
func TestD1DreamMissingRunnerCoversR02(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	// A PATH with no runner at all: the command must fail before touching Git.
	missingEnv := envWith(env, "PATH=/nonexistent")
	result := runMemctl(t, bin, home, missingEnv, "dream", "--runner", "claude")
	if result.code != 1 {
		t.Fatalf("missing runner: code=%d stdout=%q stderr=%q, want 1", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "UNSUPPORTED") || !strings.Contains(result.stderr, "not installed") {
		t.Fatalf("missing runner error is not targeted: %q", result.stderr)
	}
}

// TestD1DreamDirtyRepositoryRefusesCoversD02 verifies that an uncommitted
// change stops generation before any isolated directory or proposal exists.
func TestD1DreamDirtyRepositoryRefusesCoversD02(t *testing.T) {
	requireDarwinSandbox(t)
	bin, home, repo, env, _ := dreamRepo(t, "exit 0\n")
	if err := os.WriteFile(filepath.Join(repo, "untracked.md"), []byte("# untracked\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	result := runMemctl(t, bin, home, env, "dream", "--runner", "claude")
	if result.code != 1 {
		t.Fatalf("dirty repository: code=%d stdout=%q stderr=%q, want 1", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "EXTERNAL_CHANGES") {
		t.Fatalf("dirty repository error is not targeted: %q", result.stderr)
	}
	if matches, _ := filepath.Glob(filepath.Join(home, ".agent-memory", "repos", "*", "proposals", "*.json")); len(matches) != 0 {
		t.Fatalf("a proposal was persisted for a dirty repository: %v", matches)
	}
}

// TestD1DreamIsolationCoversD01 verifies that the real sandbox lets the runner
// edit the exported tree while blocking writes to the main repository and the
// user configuration.
func TestD1DreamIsolationCoversD01(t *testing.T) {
	requireDarwinSandbox(t)
	stub := fmt.Sprintf(`cat > MEMORY.md <<'EOF'
# Memory

## Index
- [[topic]]
- [[added]]
EOF
printf '# Added\n- new fact\n' > added.md
printf 'evil' > %[1]q 2>/dev/null || true
printf 'evil' >> %[2]q 2>/dev/null || true
`, filepath.Join("REPO_PLACEHOLDER", "evil.txt"), "HOME_PLACEHOLDER/.claude/settings.json")
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	stub = strings.ReplaceAll(stub, "REPO_PLACEHOLDER", repo)
	stub = strings.ReplaceAll(stub, "HOME_PLACEHOLDER", home)
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	stubs := filepath.Join(home, "stubs")
	if err := os.MkdirAll(stubs, 0o755); err != nil {
		t.Fatalf("create stub dir: %v", err)
	}
	writeStub(t, stubs, "claude", stub)
	env = envWith(env, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Seed the content the proposal is computed against.
	a4WriteFile(t, bin, home, repo, env, "MEMORY.md", "# Memory\n\n## Index\n- [[topic]]\n")
	a4WriteFile(t, bin, home, repo, env, "topic.md", "# Topic\n- fact one\n")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("create host config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("seed host config: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}

	result := runMemctl(t, bin, home, env, "dream", "--runner", "claude")
	if result.code != 0 {
		t.Fatalf("dream: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	var proposal dreamProposalJSON
	if err := json.Unmarshal([]byte(result.stdout), &proposal); err != nil {
		t.Fatalf("decode proposal: %v\n%s", err, result.stdout)
	}
	if proposal.Status != "ready" {
		t.Fatalf("proposal status = %q (error=%q), want ready", proposal.Status, proposal.Error)
	}
	if proposal.PatchHash == "" || len(proposal.Changes) != 2 {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	actions := map[string]string{}
	for _, change := range proposal.Changes {
		actions[change.Path] = change.Action
	}
	if actions["MEMORY.md"] != "update" || actions["added.md"] != "create" {
		t.Fatalf("unexpected change set: %+v", actions)
	}

	// The main repository and the user configuration are byte-identical.
	after, err := os.ReadFile(filepath.Join(repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("re-read MEMORY.md: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the runner modified the main repository")
	}
	if _, err := os.Stat(filepath.Join(repo, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("the runner wrote outside the exported tree (stat err=%v)", err)
	}
	hostConfig, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("re-read host config: %v", err)
	}
	if string(hostConfig) != "original\n" {
		t.Fatalf("the runner modified the user configuration: %q", hostConfig)
	}

	// `dream --show` returns the persisted proposal with the same patch hash.
	shown := runMemctl(t, bin, home, env, "dream", "--show", proposal.ProposalID)
	if shown.code != 0 {
		t.Fatalf("dream --show: code=%d stderr=%q", shown.code, shown.stderr)
	}
	var loaded dreamProposalJSON
	if err := json.Unmarshal([]byte(shown.stdout), &loaded); err != nil {
		t.Fatalf("decode shown proposal: %v\n%s", err, shown.stdout)
	}
	if loaded.PatchHash != proposal.PatchHash || loaded.Status != "ready" {
		t.Fatalf("shown proposal = %+v, want the ready proposal", loaded)
	}
}

// TestD1DreamOutOfScopeRejectedCoversD03 verifies that a symbolic link created
// by the runner fails the range check and is never applied.
func TestD1DreamOutOfScopeRejectedCoversD03(t *testing.T) {
	requireDarwinSandbox(t)
	bin, home, repo, env, _ := dreamRepo(t, "ln -s /etc/passwd escape\n")
	before, err := os.ReadFile(filepath.Join(repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	result := runMemctl(t, bin, home, env, "dream", "--runner", "claude")
	if result.code != 1 {
		t.Fatalf("out-of-scope proposal: code=%d stdout=%q stderr=%q, want 1", result.code, result.stdout, result.stderr)
	}
	var proposal dreamProposalJSON
	if err := json.Unmarshal([]byte(result.stdout), &proposal); err != nil {
		t.Fatalf("decode failed proposal: %v\n%s", err, result.stdout)
	}
	if proposal.Status != "failed" || !strings.Contains(proposal.Error, "UNSUPPORTED") {
		t.Fatalf("out-of-scope proposal = %+v, want failed UNSUPPORTED", proposal)
	}
	after, err := os.ReadFile(filepath.Join(repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("re-read MEMORY.md: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the main repository was modified by a rejected proposal")
	}
	if _, err := os.Lstat(filepath.Join(repo, "escape")); !os.IsNotExist(err) {
		t.Fatalf("the rejected symlink leaked into the main repository")
	}
}

// TestD1DreamHTTPPreviewCoversD01AndEvent verifies the asynchronous HTTP route:
// a 202 carrying the proposal id, a later GET that returns the ready proposal
// and one dream.updated event on the SSE bus.
func TestD1DreamHTTPPreviewCoversD01AndEvent(t *testing.T) {
	requireDarwinSandbox(t)
	fixture := newB1Fixture(t, false)
	fixture.write(t, "topic.md", "# Topic\n- fact\n")

	stub := filepath.Join(fixture.home, "stubs")
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatalf("create stub dir: %v", err)
	}
	writeStub(t, stub, "claude", "printf '# Topic\\n- organized\\n' > topic.md\n")
	service := dream.NewService(dream.Source{
		RepoPath: config.Canonical(fixture.repo),
		RepoID:   config.RepoID(fixture.repo),
		StateDir: filepath.Join(fixture.home, ".agent-memory"),
	})
	service.Runners = map[string]dream.Runner{
		"claude": {Name: "claude", Binary: filepath.Join(stub, "claude"), BuildArgs: func(string) []string { return nil }},
	}
	service.Timeout = 20 * time.Second
	fixture.app.SetDreamService(service)

	subscription := fixture.server.Events().Subscribe()
	defer subscription.Close()

	body := `{"runner":"claude"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18420/api/v1/dream:preview", strings.NewReader(body))
	request.Host = "127.0.0.1:18420"
	request.Header.Set("X-Memctl-Token", b1Token)
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("preview status = %d, want 202: %s", recorder.Code, recorder.Body.String())
	}
	var started struct {
		ProposalID string `json:"proposalId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil || started.ProposalID == "" {
		t.Fatalf("decode 202 response: %v (%s)", err, recorder.Body.String())
	}

	// Wait for the terminal dream.updated event, then read the proposal.
	deadline := time.After(20 * time.Second)
	eventSeen := false
	for !eventSeen {
		select {
		case hint := <-subscription.Hints():
			if hint.Reason == "dream.updated" && hint.ProposalID == started.ProposalID && hint.Status == "ready" {
				eventSeen = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for the dream.updated event")
		}
	}

	shown := fixture.get(t, "/api/v1/dream/"+started.ProposalID)
	var proposal dreamProposalJSON
	fixture.decode(t, shown, &proposal)
	if proposal.Status != "ready" || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v, want ready with a patch hash", proposal)
	}
	if _, ok := proposalChange(proposal, "topic.md"); !ok {
		t.Fatalf("proposal did not include the runner's change: %+v", proposal.Changes)
	}
}

// proposalChange reports whether path is present in the decoded change set.
func proposalChange(proposal dreamProposalJSON, path string) (string, bool) {
	for _, change := range proposal.Changes {
		if change.Path == path {
			return change.Action, true
		}
	}
	return "", false
}
