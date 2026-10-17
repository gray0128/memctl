package dream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
)

// This file verifies the stage D-1 Dreaming pipeline without a kernel sandbox:
// the export, the runner invocation, the change validation and the persistence
// are exercised through an injected pass-through sandbox and a synthetic
// runner. The real Seatbelt isolation is covered by the platform test and by
// tests/integration/d1_dream_test.go.

// passthrough runs the runner directly. It exists only for pipeline tests that
// are not about isolation; production code always uses platformSandbox.
type passthrough struct{}

func (passthrough) Name() string    { return "passthrough" }
func (passthrough) Available() bool { return true }
func (passthrough) Wrap(_, execPath string, args []string) (string, []string, error) {
	return execPath, args, nil
}

// initRepo creates a one-commit memory repository and returns its root path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_NOSYSTEM=1",
		)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Memory\n\n## Index\n- [[topic]]\n"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "topic.md"), []byte("# Topic\n- fact one\n"), 0o644); err != nil {
		t.Fatalf("write topic.md: %v", err)
	}
	run("add", "MEMORY.md", "topic.md")
	run("commit", "-m", "init")
	return dir
}

// newTestService builds a service with a pass-through sandbox and one synthetic
// runner script.
func newTestService(t *testing.T, repoPath, stateDir string, scriptBody string) (*Service, string) {
	t.Helper()
	runnerPath := filepath.Join(t.TempDir(), "runner.sh")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\n"+scriptBody), 0o755); err != nil {
		t.Fatalf("write runner: %v", err)
	}
	service := NewService(Source{
		RepoPath: repoPath,
		RepoID:   config.RepoID(repoPath),
		StateDir: stateDir,
	})
	service.Sandbox = passthrough{}
	service.Timeout = 20 * time.Second
	service.Runners = map[string]Runner{
		"test": {Name: "test", Binary: runnerPath, BuildArgs: func(string) []string { return nil }},
	}
	return service, runnerPath
}

func TestProtectedPath(t *testing.T) {
	protected := []string{".git/config", ".GIT/config", ".gitconfig", "sub/.gitmodules", ".ssh/id_rsa", "id_rsa", "keys/server.pem", "sub/.claude/settings.json"}
	for _, rel := range protected {
		if !protectedPath(rel) {
			t.Errorf("protectedPath(%q) = false, want true", rel)
		}
	}
	allowed := []string{"MEMORY.md", "topic.md", "notes/plan.md", "queries/report.sql", "scripts/clean.sh"}
	for _, rel := range allowed {
		if protectedPath(rel) {
			t.Errorf("protectedPath(%q) = true, want false", rel)
		}
	}
}

func TestUnifiedDiffShape(t *testing.T) {
	createDiff := unifiedDiff("/dev/null", "b/new.md", "", "# New\n")
	if !strings.Contains(createDiff, "--- /dev/null") || !strings.Contains(createDiff, "+++ b/new.md") || !strings.Contains(createDiff, "+# New") {
		t.Fatalf("create diff is malformed:\n%s", createDiff)
	}
	updateDiff := unifiedDiff("a/x.md", "b/x.md", "one\ntwo\n", "one\nthree\n")
	if !strings.Contains(updateDiff, "-two") || !strings.Contains(updateDiff, "+three") {
		t.Fatalf("update diff is malformed:\n%s", updateDiff)
	}
	deleteDiff := unifiedDiff("a/old.md", "/dev/null", "gone\n", "")
	if !strings.Contains(deleteDiff, "--- a/old.md") || !strings.Contains(deleteDiff, "-gone") {
		t.Fatalf("delete diff is malformed:\n%s", deleteDiff)
	}
	if unifiedDiff("a/x", "b/x", "same\n", "same\n") != "" {
		t.Fatalf("identical content produced a diff")
	}
}

func TestServiceRejectsMissingSandbox(t *testing.T) {
	repo := initRepo(t)
	service, _ := newTestService(t, repo, t.TempDir(), "exit 0\n")
	service.Sandbox = Unavailable{}

	_, err := service.Preview(context.Background(), PreviewRequest{Runner: "test"})
	if !contractCode(err, "UNSUPPORTED") {
		t.Fatalf("missing sandbox: err=%v, want UNSUPPORTED", err)
	}
}

func TestServiceRejectsMissingRunner(t *testing.T) {
	repo := initRepo(t)
	service, _ := newTestService(t, repo, t.TempDir(), "exit 0\n")
	service.Runners["claude"] = Runner{Name: "claude", Binary: "memctl-no-such-runner-xyz", BuildArgs: func(string) []string { return nil }}

	_, err := service.Preview(context.Background(), PreviewRequest{Runner: "claude"})
	if !contractCode(err, "UNSUPPORTED") {
		t.Fatalf("missing runner: err=%v, want UNSUPPORTED", err)
	}
}

func TestServiceRefusesDirtyRepository(t *testing.T) {
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "untracked.md"), []byte("# Untracked\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	service, _ := newTestService(t, repo, t.TempDir(), "exit 0\n")

	_, err := service.Preview(context.Background(), PreviewRequest{Runner: "test"})
	if !contractCode(err, "EXTERNAL_CHANGES") {
		t.Fatalf("dirty repository: err=%v, want EXTERNAL_CHANGES", err)
	}
}

func TestServiceRefusesStaleBaseRevision(t *testing.T) {
	repo := initRepo(t)
	service, _ := newTestService(t, repo, t.TempDir(), "exit 0\n")

	_, err := service.Preview(context.Background(), PreviewRequest{Runner: "test", BaseRevision: "sha256:stale"})
	if !contractCode(err, "REVISION_MISMATCH") {
		t.Fatalf("stale base: err=%v, want REVISION_MISMATCH", err)
	}
}

func TestServiceGenerationPipeline(t *testing.T) {
	repo := initRepo(t)
	stateDir := t.TempDir()
	// Isolate the temporary work directory so a concurrently running test
	// binary cannot leave a matching directory in the shared system temp root.
	t.Setenv("TMPDIR", t.TempDir())
	before, err := os.ReadFile(filepath.Join(repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	service, _ := newTestService(t, repo, stateDir, `cat > MEMORY.md <<'EOF'
# Memory

## Index
- [[topic]]
- [[new]]
EOF
printf '# New\n- added\n' > new.md
`)

	proposal, err := service.Preview(context.Background(), PreviewRequest{Runner: "test"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if proposal.Status != StatusReady {
		t.Fatalf("status = %q, want ready (error=%q)", proposal.Status, proposal.Error)
	}
	if proposal.PatchHash == "" || !strings.HasPrefix(proposal.PatchHash, "sha256:") {
		t.Fatalf("patchHash = %q, want a sha256 hash", proposal.PatchHash)
	}
	if len(proposal.Changes) != 2 {
		t.Fatalf("changes = %+v, want 2 entries (one create, one update)", proposal.Changes)
	}
	actions := map[string]string{}
	for _, change := range proposal.Changes {
		actions[change.Path] = change.Action
	}
	if actions["MEMORY.md"] != ChangeUpdate || actions["new.md"] != ChangeCreate {
		t.Fatalf("unexpected actions: %+v", actions)
	}
	if proposal.Diff == "" {
		t.Fatalf("expected a non-empty diff")
	}

	// The main repository must be byte-identical: generation never writes it.
	after, err := os.ReadFile(filepath.Join(repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("re-read MEMORY.md: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the main repository was modified:\n%s", after)
	}

	// The proposal is persisted and queryable, and the temporary environment
	// has been removed.
	loaded, err := service.Load(context.Background(), proposal.ProposalID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Status != StatusReady || loaded.PatchHash != proposal.PatchHash {
		t.Fatalf("persisted proposal = %+v, want the ready proposal", loaded)
	}
	workDir := service.pending[proposal.ProposalID]
	if workDir != nil {
		t.Fatalf("pending run was not cleared")
	}
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "memctl-dream-*")); len(matches) != 0 {
		t.Fatalf("temporary Dreaming directories remain: %v", matches)
	}
}

func TestServiceRejectsOutOfScopeProposal(t *testing.T) {
	cases := map[string]string{
		"binary":       "printf '\\000\\001binary' > blob.bin\n",
		"git-config":   "printf 'x' > .gitconfig\n",
		"git-internal": "mkdir -p .git && printf 'x' > .git/config\n",
		"symlink":      "ln -s /etc/passwd escape\n",
		"host-hook":    "mkdir -p .claude && printf '{}' > .claude/settings.json\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			repo := initRepo(t)
			service, _ := newTestService(t, repo, t.TempDir(), body)
			proposal, err := service.Preview(context.Background(), PreviewRequest{Runner: "test"})
			if err == nil {
				t.Fatalf("out-of-scope proposal was accepted: %+v", proposal)
			}
			if proposal.Status != StatusFailed {
				t.Fatalf("status = %q, want failed (err=%v)", proposal.Status, err)
			}
			// The main repository is untouched.
			if _, statErr := os.Stat(filepath.Join(repo, "blob.bin")); !os.IsNotExist(statErr) {
				t.Fatalf("out-of-scope content leaked into the main repository")
			}
		})
	}
}

func TestServiceReconcilesOrphanedGeneration(t *testing.T) {
	repo := initRepo(t)
	stateDir := t.TempDir()
	service, _ := newTestService(t, repo, stateDir, "sleep 30\n")

	started, err := service.Begin(context.Background(), PreviewRequest{Runner: "test"})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if started.Status != StatusGenerating {
		t.Fatalf("status = %q, want generating", started.Status)
	}

	// A different process instance does not own the in-memory run, so it must
	// reconcile the orphaned record to failed instead of pretending it can
	// continue.
	other := NewService(Source{RepoPath: repo, RepoID: config.RepoID(repo), StateDir: stateDir})
	other.Sandbox = passthrough{}
	other.Runners = service.Runners
	proposal, err := other.Load(context.Background(), started.ProposalID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if proposal.Status != StatusFailed || proposal.Error == "" {
		t.Fatalf("orphaned generation = %+v, want failed with an error", proposal)
	}
}

// contractCode reports whether err carries the given contract code without
// importing memerr into every assertion.
func contractCode(err error, code string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), code+":") || strings.Contains(err.Error(), code)
}
