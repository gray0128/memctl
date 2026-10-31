package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/git"
)

// This file covers X10–X12 of docs/15-delta-corpus-adaptation.md section 8:
// Delta runs every thread in its own checkout under <project>/.delta/worktrees/,
// so the project key of a digest has to be the directory the user opened rather
// than the checkout. The rule is proved against real Git layouts instead of a
// fake runner, because the whole point is that Git reports the Delta clone
// layout the way the rule expects.

// x10Git runs Git in dir and fails the test on a non-zero exit.
func x10Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v (%s)", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// x10Project builds a project repository plus two Delta thread checkouts laid
// out the way Delta creates them.
func x10Project(t *testing.T) (project string, threads []string) {
	t.Helper()
	project = filepath.Join(t.TempDir(), "memctl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}
	x10Git(t, project, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(project, "README.md"), []byte("project\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	x10Git(t, project, "add", "-A")
	x10Git(t, project, "commit", "-q", "-m", "init")

	for _, id := range []string{"vt74q90bpvrv", "eh1c2q2xgcqy"} {
		checkout := filepath.Join(project, ".delta", "worktrees", id, "memctl")
		if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
			t.Fatalf("create checkout parent: %v", err)
		}
		x10Git(t, project, "worktree", "add", "-q", "--detach", checkout)
		threads = append(threads, checkout)
	}
	return project, threads
}

// TestProjectRootCollapsesDeltaCheckoutsCoversX10 proves that the original
// project directory and every Delta thread checkout produce one project key, and
// that the key is the one existing digests already carry.
func TestProjectRootCollapsesDeltaCheckoutsCoversX10(t *testing.T) {
	project, threads := x10Project(t)
	projectRoot, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	want := corpus.CwdKey(projectRoot)
	if got := corpus.CwdKey(projectRoot); got != want {
		t.Fatalf("project key is not stable: %q vs %q", got, want)
	}
	for _, thread := range threads {
		resolved, err := filepath.EvalSymlinks(thread)
		if err != nil {
			t.Fatalf("resolve thread checkout: %v", err)
		}
		// The raw checkout must not be the key: that is exactly the
		// fragmentation this rule exists to prevent.
		if corpus.CwdKey(resolved) == want {
			t.Fatalf("the raw thread checkout %s already shares the project key", resolved)
		}
		if got := corpus.CwdKey(corpus.ResolveProjectRoot(t.Context(), git.Run, resolved)); got != want {
			t.Fatalf("thread checkout %s normalised to %q, want the project key %q", resolved, got, want)
		}
		// A subdirectory of a checkout is the same project too.
		sub := filepath.Join(resolved, "sub")
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatalf("create subdirectory: %v", err)
		}
		if got := corpus.CwdKey(corpus.ResolveProjectRoot(t.Context(), git.Run, sub)); got != want {
			t.Fatalf("subdirectory %s normalised to %q, want %q", sub, got, want)
		}
	}
}

// TestPrepareKeepsForeignDirectoryOutOfThePoolCoversX12 proves the closing half
// of the rule: when the agent runs `dream --prepare` outside a project, the
// session must not be filed under the current project, and the command must
// still succeed.
func TestPrepareKeepsForeignDirectoryOutOfThePoolCoversX12(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project, threads := x10Project(t)
	projectRoot, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	// One digest for the project, one for a directory that belongs to nothing.
	// A prepare from that directory must return the project session only when
	// it is asked for, and must never rewrite the foreign key.
	writeH3Digest(t, home, "claude-code", "project-sess", projectRoot, "2026-10-20", "project fact")
	// t.TempDir() is a symlink target on macOS, so both the digest and the
	// command use the resolved path: the key is a hash of the exact string.
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve outside directory: %v", err)
	}
	foreign := writeH3Digest(t, home, "claude-code", "outside-sess", outside, "2026-10-20", "unrelated fact")

	bundle := runPrepare(t, bin, home, outside)
	if bundle.ProjectFilter.CwdKey != corpus.CwdKey(config.Canonical(outside)) {
		t.Fatalf("project filter = %+v, want the directory the command ran in", bundle.ProjectFilter)
	}
	for _, session := range bundle.Sessions {
		if session.SessionID == "outside-sess" && session.CwdBase != filepath.Base(outside) {
			t.Fatalf("the outside session was refiled: %+v", session)
		}
	}

	// The foreign digest is unchanged: a prepare must not rewrite keys.
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(repo))
	stored, ok := cache.Load("claude-code", "outside-sess")
	if !ok {
		t.Fatal("the outside digest disappeared")
	}
	if stored.CwdKey != foreign.CwdKey {
		t.Fatalf("the outside digest key changed: %q -> %q", foreign.CwdKey, stored.CwdKey)
	}

	// From a thread checkout the same command lands on the project key, and the
	// project's own session is the one ranked first.
	threadRoot, err := filepath.EvalSymlinks(threads[0])
	if err != nil {
		t.Fatalf("resolve thread checkout: %v", err)
	}
	inThread := runPrepare(t, bin, home, threadRoot)
	if inThread.ProjectFilter.CwdKey != corpus.CwdKey(projectRoot) {
		t.Fatalf("thread project filter = %+v, want the project key %q", inThread.ProjectFilter, corpus.CwdKey(projectRoot))
	}
	if len(inThread.Sessions) == 0 || inThread.Sessions[0].SessionID != "project-sess" {
		t.Fatalf("thread sessions = %+v, want the project session first", inThread.Sessions)
	}
}

// x10FakeDelta installs a `delta` executable that answers `thread watch` with a
// fixed stream, so the end-to-end prepare path runs without a real Delta
// installation. It returns the directory to put first on PATH.
func x10FakeDelta(t *testing.T, stream string) string {
	t.Helper()
	dir := t.TempDir()
	payload := filepath.Join(dir, "watch.jsonl")
	if err := os.WriteFile(payload, []byte(stream), 0o600); err != nil {
		t.Fatalf("write stream: %v", err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"thread\" ] && [ \"$2\" = \"watch\" ]; then\n" +
		"  cat '" + payload + "'\n" +
		// The real watch keeps streaming after its live marker; sleeping proves
		// the collector stops reading on its own instead of waiting for exit.
		"  sleep 30\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"unexpected invocation: $*\" >&2\n" +
		"exit 2\n"
	path := filepath.Join(dir, "delta")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake delta: %v", err)
	}
	return dir
}

// x13Stream builds a watch stream with the shapes X13 names: a long thread, two
// agent messages in one turn, a reasoning item, an oversized tool call and the
// live marker.
func x13Stream(threadID string) string {
	long := strings.Repeat("答", corpus.MaxTurnTextBytes/3)
	return strings.Join([]string{
		`{"type":"ready","thread_id":"` + threadID + `","producer":null}`,
		`{"type":"item","item":{"kind":"user_message","created_at":1000,"content":{"text":"第一问"}}}`,
		`{"type":"item","item":{"kind":"agent_thought","created_at":1000,"content":{"text":"thinking only"}}}`,
		`{"type":"item","item":{"kind":"tool_call","created_at":1000,"content":{"name":"terminal","title":"ls","status":"completed","rawInput":{"command":"ls"},"rawOutput":"` + strings.Repeat("z", 2048) + `"}}}`,
		`{"type":"item","item":{"kind":"agent_message","created_at":1000,"content":{"text":"先核对一下。"}}}`,
		`{"type":"item","item":{"kind":"agent_message","created_at":1000,"content":{"text":"` + long + `"}}}`,
		`{"type":"item","item":{"kind":"user_message","created_at":2000,"content":{"text":"第二问"}}}`,
		`{"type":"item","item":{"kind":"agent_message","created_at":2000,"content":{"text":"第二答"}}}`,
		`{"type":"live"}`,
	}, "\n") + "\n"
}

// TestPrepareCollectsTheDeltaThreadCoversX13 proves the whole chain through the
// binary: a Delta thread named by DELTA_CURRENT_THREAD_ID becomes an
// `agent: delta` digest whose project key is the thread checkout's project, the
// assistant text carries no reasoning, and the turn keeps the longest message.
func TestPrepareCollectsTheDeltaThreadCoversX13(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project, threads := x10Project(t)
	projectRoot, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}
	threadRoot, err := filepath.EvalSymlinks(threads[0])
	if err != nil {
		t.Fatalf("resolve thread checkout: %v", err)
	}

	const threadID = "thread-xyz"
	fakeDir := x10FakeDelta(t, x13Stream(threadID))

	// The environment is the one Delta creates: the thread id is exported, the
	// agent name is passed explicitly, and the fake CLI is first on PATH.
	env := append(h3Env(home),
		"PATH="+fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DELTA_CURRENT_THREAD_ID="+threadID,
	)
	args := []string{"dream", "--prepare", "--agent", "delta", "--sid", threadID}
	result := runMemctl(t, bin, threadRoot, env, args...)
	if result.code != 0 {
		t.Fatalf("dream --prepare exited %d: %s", result.code, result.stderr)
	}

	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(repo))
	digest, ok := cache.Load("delta", threadID)
	if !ok {
		t.Fatalf("no delta digest was written; stderr: %s", result.stderr)
	}
	if digest.CwdKey != corpus.CwdKey(projectRoot) {
		t.Fatalf("delta cwdKey = %q, want the project key %q", digest.CwdKey, corpus.CwdKey(projectRoot))
	}
	if digest.SourceTag != "agent://"+deviceName()+"/delta/"+threadID {
		t.Fatalf("source tag = %q", digest.SourceTag)
	}
	if len(digest.Turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(digest.Turns))
	}
	if strings.Contains(digest.Turns[0].Assistant, "thinking only") {
		t.Fatalf("reasoning leaked into the digest: %q", digest.Turns[0].Assistant)
	}
	if !strings.HasPrefix(digest.Turns[0].Assistant, "答") {
		t.Fatalf("the turn kept the wrong agent message: %q", digest.Turns[0].Assistant[:20])
	}
	if len(digest.Turns[0].Assistant) > corpus.MaxTurnTextBytes {
		t.Fatalf("assistant text is %d bytes, above the limit", len(digest.Turns[0].Assistant))
	}
	if len(digest.Turns[0].Tools) != 1 || strings.Contains(digest.Turns[0].Tools[0], "zzz") {
		t.Fatalf("tool signature kept output: %v", digest.Turns[0].Tools)
	}
}

// TestPrepareSurvivesAFailingDeltaCoversX14 proves the collector never breaks
// prepare: a CLI that exits non-zero, one that is missing, and a stream cut
// before the live marker all leave prepare succeeding with no digest.
func TestPrepareSurvivesAFailingDeltaCoversX14(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project, threads := x10Project(t)
	if _, err := filepath.EvalSymlinks(project); err != nil {
		t.Fatalf("resolve project: %v", err)
	}
	threadRoot, err := filepath.EvalSymlinks(threads[0])
	if err != nil {
		t.Fatalf("resolve thread checkout: %v", err)
	}

	const threadID = "thread-fail"
	cases := []struct {
		name string
		// pathEntry is prepended to PATH; empty means "do not use the fake".
		pathEntry string
	}{
		{
			name:      "the CLI exits non-zero",
			pathEntry: x10FakeDeltaExiting(t),
		},
		{
			name: "the stream stops before the live marker",
			pathEntry: x10FakeDelta(t, `{"type":"ready","thread_id":"`+threadID+`"}`+"\n"+
				`{"type":"item","item":{"kind":"user_message","created_at":1000,"content":{"text":"问题"}}}`+"\n"),
		},
		{
			name:      "the CLI is missing",
			pathEntry: filepath.Join(t.TempDir(), "empty-path"),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := os.MkdirAll(test.pathEntry, 0o700); err != nil {
				t.Fatalf("create path entry: %v", err)
			}
			env := append(h3Env(home),
				"PATH="+test.pathEntry+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DELTA_CURRENT_THREAD_ID="+threadID,
			)
			start := time.Now()
			result := runMemctl(t, bin, threadRoot, env, "dream", "--prepare", "--agent", "delta", "--sid", threadID)
			elapsed := time.Since(start)
			if result.code != 0 {
				t.Fatalf("prepare exited %d: %s", result.code, result.stderr)
			}
			// The budget must actually end the read: the fixture's watch sleeps
			// for 30 seconds after printing, so anything near that means the
			// deadline never reached the process group.
			if elapsed > 15*time.Second {
				t.Fatalf("prepare took %s, so the collection budget did not fire", elapsed.Round(time.Second))
			}
			cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(repo))
			if _, ok := cache.Load("delta", threadID); ok {
				t.Fatal("a failed collection still wrote a digest")
			}
		})
	}
}

// x10FakeDeltaExiting installs a `delta` that always exits non-zero.
func x10FakeDeltaExiting(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'delta: unexpected error' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "delta"), []byte(script), 0o700); err != nil {
		t.Fatalf("write failing delta: %v", err)
	}
	return dir
}
