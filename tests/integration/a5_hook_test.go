package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/state"
)

// This file verifies A-5 sub-task 2 (T019): the Hook protocol, session identity
// and the start/context/pull/tag/wrap commands. It covers H01-H06. Every run
// uses a temporary HOME and memory repository with background derivation
// disabled, so no network or real credential source is touched.

type hookEnvelope struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func decodeEnvelope(t *testing.T, stdout string) hookEnvelope {
	t.Helper()
	var envelope hookEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\n%s", err, stdout)
	}
	return envelope
}

// a5LocalRepo initializes a local-only memory repository with a fixed device
// and agent identity.
func a5LocalRepo(t *testing.T, bin, root, name string) (home, repo string, env []string) {
	t.Helper()
	home = a4Home(t, root, name)
	repo = filepath.Join(home, "memory")
	env = a4Env(home, repo, "AGENT_MEMORY_DEVICE=test-device", "AGENT_MEMORY_AGENT=test-agent")
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	return home, repo, env
}

// TestA5SessionIDPriorityCoversH01 verifies the documented SID priority, the
// stdin spellings, empty-value skipping and the non-string protocol error.
func TestA5SessionIDPriorityCoversH01(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, baseEnv := a5LocalRepo(t, bin, root, "home")

	tests := []struct {
		name    string
		env     []string
		payload string
		args    []string
		want    string
	}{
		{
			name:    "explicit wins over env and payload",
			env:     envWith(baseEnv, "AGENT_MEMORY_SID=env-session"),
			payload: `{"session_id":"payload-session"}`,
			args:    []string{"--sid", "flag-session"},
			want:    "flag-session",
		},
		{
			name:    "dsh environment is used after AGENT_MEMORY_SID",
			env:     envWith(baseEnv, "AGENT_MEMORY_SID=", "DSH_HOOK_SESSION_ID=dsh-session"),
			payload: `{"session_id":"payload-session"}`,
			want:    "dsh-session",
		},
		{
			name:    "payload session_id",
			env:     baseEnv,
			payload: `{"session_id":"snake"}`,
			want:    "snake",
		},
		{
			name:    "payload sessionId",
			env:     baseEnv,
			payload: `{"sessionId":"camel"}`,
			want:    "camel",
		},
		{
			name:    "payload sessionID",
			env:     baseEnv,
			payload: `{"sessionID":"upper"}`,
			want:    "upper",
		},
		{
			name:    "empty payload field falls through",
			env:     baseEnv,
			payload: `{"session_id":"","sessionId":"fallback"}`,
			want:    "fallback",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runMemctlStdin(t, bin, home, tc.env, tc.payload, append([]string{"context", "--json", "--stdin"}, tc.args...)...)
			if result.code != 0 {
				t.Fatalf("context: code=%d stderr=%q", result.code, result.stderr)
			}
			context := decodeEnvelope(t, result.stdout).HookSpecificOutput.AdditionalContext
			want := "agent://test-device/test-agent/" + tc.want
			if !strings.Contains(context, "source tag: "+want+"\n") {
				t.Fatalf("context did not report %q:\n%s", want, context)
			}
		})
	}

	t.Run("no stable sid", func(t *testing.T) {
		result := runMemctlStdin(t, bin, home, envWith(baseEnv, "AGENT_MEMORY_SID="), `{}`, "context", "--json", "--stdin")
		if result.code != 0 {
			t.Fatalf("context: code=%d stderr=%q", result.code, result.stderr)
		}
		context := decodeEnvelope(t, result.stdout).HookSpecificOutput.AdditionalContext
		if !strings.Contains(context, "source tag: unavailable") {
			t.Fatalf("expected an unavailable source tag:\n%s", context)
		}
		if strings.Contains(context, "test-agent//") {
			t.Fatalf("a source tag was fabricated without a stable sid:\n%s", context)
		}
	})

	t.Run("non-string session id", func(t *testing.T) {
		result := runMemctlStdin(t, bin, home, baseEnv, `{"session_id":123}`, "context", "--stdin")
		if result.code != 2 || !strings.Contains(result.stderr, "INVALID_ARGUMENT") {
			t.Fatalf("non-string session id: code=%d stderr=%q, want 2 INVALID_ARGUMENT", result.code, result.stderr)
		}
		if result.stdout != "" {
			t.Fatalf("a protocol error must not print a context body: %q", result.stdout)
		}
	})
}

// TestA5ParallelSessionsDoNotCrossCoversH02 verifies that two sessions of the
// same agent keep distinct sources and that no shared last-sid state is used.
func TestA5ParallelSessionsDoNotCrossCoversH02(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	tagA := runMemctl(t, bin, home, env, "tag", "--agent", "alpha", "--sid", "sessA")
	tagB := runMemctl(t, bin, home, env, "tag", "--agent", "beta", "--sid", "sessB")
	if tagA.code != 0 || tagB.code != 0 {
		t.Fatalf("tag: A code=%d stderr=%q, B code=%d stderr=%q", tagA.code, tagA.stderr, tagB.code, tagB.stderr)
	}
	if strings.TrimSpace(tagA.stdout) != "agent://test-device/alpha/sessA" {
		t.Fatalf("tag A = %q", tagA.stdout)
	}
	if strings.TrimSpace(tagB.stdout) != "agent://test-device/beta/sessB" {
		t.Fatalf("tag B = %q", tagB.stdout)
	}

	writeSession := func(agent, sid, content string) {
		baseline, err := readBaseline(t, bin, home, repo, env, "MEMORY.md")
		if err != nil {
			baseline = "absent"
		}
		result := runMemctlStdin(t, bin, home, env, content, "write", "MEMORY.md", "--base", baseline, "--stdin", "--agent", agent, "--sid", sid)
		if result.code != 0 {
			t.Fatalf("write %s/%s: code=%d stderr=%q", agent, sid, result.code, result.stderr)
		}
	}
	writeSession("alpha", "sessA", "# Memory\n\n## Index\n- from alpha\n")
	writeSession("beta", "sessB", "# Memory\n\n## Index\n- from alpha\n- from beta\n")

	log := gitOutput(t, env, repo, "log", "-2", "--format=%B")
	if !strings.Contains(log, "Agent: alpha") || !strings.Contains(log, "Session: sessA") {
		t.Fatalf("alpha commit lost its source:\n%s", log)
	}
	if !strings.Contains(log, "Agent: beta") || !strings.Contains(log, "Session: sessB") {
		t.Fatalf("beta commit lost its source:\n%s", log)
	}

	// A session id is required; memctl never guesses one from shared state.
	noSID := runMemctl(t, bin, home, env, "tag", "--agent", "alpha")
	if noSID.code != 1 || !strings.Contains(noSID.stderr, "NO_SESSION_ID") {
		t.Fatalf("tag without sid: code=%d stderr=%q, want 1 NO_SESSION_ID", noSID.code, noSID.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-memory", "seen")); !os.IsNotExist(err) {
		t.Fatalf("a shared last-sid directory exists: %v", err)
	}
}

// TestA5OnceSyncRetryCoversH03 verifies that a failed first sync is retried and
// that a successfully injected body is not printed twice.
func TestA5OnceSyncRetryCoversH03(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")
	agent, sid := "test-agent", "retry-session"

	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	held, err := lock.Acquire(context.Background(), gitDir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	first := runMemctlStdin(t, bin, home, env, `{}`, "context", "--once", "--stdin", "--sid", sid, "--agent", agent)
	if first.code != 0 {
		t.Fatalf("first context: code=%d stderr=%q", first.code, first.stderr)
	}
	if !strings.Contains(first.stdout, "sync=busy") {
		t.Fatalf("expected a busy first sync:\n%s", first.stdout)
	}
	if !strings.Contains(first.stdout, "----- "+filepath.Join(config.Canonical(repo), "MEMORY.md")+" -----") {
		t.Fatalf("the local entry file was not injected while the lock was busy:\n%s", first.stdout)
	}
	_ = held.Release()

	second := runMemctlStdin(t, bin, home, env, `{}`, "context", "--once", "--stdin", "--sid", sid, "--agent", agent)
	if second.code != 0 {
		t.Fatalf("second context: code=%d stderr=%q", second.code, second.stderr)
	}
	if second.stdout != "" {
		t.Fatalf("an already injected body was printed again:\n%s", second.stdout)
	}

	record := readSessionRecord(t, home, repo, agent, sid)
	if !record.SyncSucceeded {
		t.Fatalf("the retried sync was not recorded as successful: %+v", record)
	}
	if !record.Injected {
		t.Fatalf("the successful injection was not recorded: %+v", record)
	}
}

// TestA5HookOutputContractCoversH04 verifies the JSON envelope, the plain text
// start output and the empty stdout of pull and sync.
func TestA5HookOutputContractCoversH04(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	start := runMemctlStdin(t, bin, home, env, `{"session_id":"s1"}`, "start", "--json", "--stdin")
	if start.code != 0 {
		t.Fatalf("start --json: code=%d stderr=%q", start.code, start.stderr)
	}
	startEnvelope := decodeEnvelope(t, start.stdout)
	if startEnvelope.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Fatalf("start event = %q", startEnvelope.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(startEnvelope.HookSpecificOutput.AdditionalContext, "----- INSTRUCTIONS -----") {
		t.Fatalf("start JSON lacks the instructions:\n%s", startEnvelope.HookSpecificOutput.AdditionalContext)
	}

	contextResult := runMemctlStdin(t, bin, home, env, `{"session_id":"s1"}`, "context", "--json", "--stdin")
	if contextResult.code != 0 {
		t.Fatalf("context --json: code=%d stderr=%q", contextResult.code, contextResult.stderr)
	}
	if event := decodeEnvelope(t, contextResult.stdout).HookSpecificOutput.HookEventName; event != "UserPromptSubmit" {
		t.Fatalf("context event = %q", event)
	}

	plain := runMemctlStdin(t, bin, home, env, `{}`, "start", "--stdin")
	if plain.code != 0 {
		t.Fatalf("start plain: code=%d stderr=%q", plain.code, plain.stderr)
	}
	if !strings.HasPrefix(plain.stdout, "[agent-memory] repo=") {
		t.Fatalf("plain start output = %q", plain.stdout)
	}
	if strings.Contains(plain.stdout, "fatal:") || strings.Contains(plain.stdout, "From ") {
		t.Fatalf("Git output leaked into the context:\n%s", plain.stdout)
	}

	pull := runMemctlStdin(t, bin, home, env, `{}`, "pull", "--once", "--stdin")
	if pull.code != 0 {
		t.Fatalf("pull: code=%d stderr=%q", pull.code, pull.stderr)
	}
	if pull.stdout != "" {
		t.Fatalf("pull stdout must be empty: %q", pull.stdout)
	}
	syncResult := runMemctlStdin(t, bin, home, env, `{}`, "sync", "--stdin")
	if syncResult.code != 0 {
		t.Fatalf("sync: code=%d stderr=%q", syncResult.code, syncResult.stderr)
	}
	if syncResult.stdout != "" {
		t.Fatalf("sync stdout must be empty: %q", syncResult.stdout)
	}
}

// TestA5WrapPassesThroughCoversH05 verifies the child's exit code and signal
// semantics, the exported session environment and that a failed pre-run sync
// never masks the child result.
func TestA5WrapPassesThroughCoversH05(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	envResult := runMemctlStdin(t, bin, home, env, "", "wrap", "--agent", "wrapped", "--sid", "wrap-sid", "--", "sh", "-c", `printf '%s|%s|%s' "$AGENT_MEMORY_SID" "$AGENT_MEMORY_AGENT" "$AGENT_MEMORY_DIR"`)
	if envResult.code != 0 {
		t.Fatalf("wrap env: code=%d stderr=%q", envResult.code, envResult.stderr)
	}
	if envResult.stdout != "wrap-sid|wrapped|"+config.Canonical(repo) {
		t.Fatalf("wrapped environment = %q", envResult.stdout)
	}

	exit := runMemctlStdin(t, bin, home, env, "", "wrap", "--agent", "wrapped", "--", "sh", "-c", "exit 3")
	if exit.code != 3 {
		t.Fatalf("wrap exit code = %d, want 3 (stderr=%q)", exit.code, exit.stderr)
	}

	signalResult := runMemctlStdin(t, bin, home, env, "", "wrap", "--agent", "wrapped", "--", "sh", "-c", "kill -TERM $$")
	if signalResult.code != 143 {
		t.Fatalf("wrap signal exit code = %d, want 143 (stderr=%q)", signalResult.code, signalResult.stderr)
	}

	// Hold the repository lock so the pre-run sync fails as busy.
	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	held, err := lock.Acquire(context.Background(), gitDir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer held.Release()

	withFailedSync := runMemctlStdin(t, bin, home, env, "", "wrap", "--agent", "wrapped", "--", "sh", "-c", "exit 7")
	if withFailedSync.code != 7 {
		t.Fatalf("wrap exit code with a failed sync = %d, want 7 (stderr=%q)", withFailedSync.code, withFailedSync.stderr)
	}
	if !strings.Contains(withFailedSync.stderr, "pre-run sync incomplete: BUSY") {
		t.Fatalf("a busy pre-run sync was not reported: %q", withFailedSync.stderr)
	}
}

// TestA5NoRemoteMissingSIDAndOversizedEntryCoversH06 verifies local-only
// context, the absence of cross-session dedup without a stable sid and the
// explicit oversized-entry notice.
func TestA5NoRemoteMissingSIDAndOversizedEntryCoversH06(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	start := runMemctlStdin(t, bin, home, env, `{}`, "start", "--stdin")
	if start.code != 0 {
		t.Fatalf("start: code=%d stderr=%q", start.code, start.stderr)
	}
	if !strings.Contains(start.stdout, "sync=local") {
		t.Fatalf("local-only start did not report sync=local:\n%s", start.stdout)
	}
	if !strings.Contains(start.stdout, "# Memory") {
		t.Fatalf("local-only start did not include the entry file:\n%s", start.stdout)
	}

	// Without a stable sid, --once must not deduplicate the body.
	first := runMemctlStdin(t, bin, home, envWith(env, "AGENT_MEMORY_SID="), `{}`, "context", "--once", "--stdin")
	second := runMemctlStdin(t, bin, home, envWith(env, "AGENT_MEMORY_SID="), `{}`, "context", "--once", "--stdin")
	if first.stdout == "" || second.stdout == "" {
		t.Fatalf("without a stable sid the body must be injected every round: first=%q second=%q", first.stdout, second.stdout)
	}

	// An oversized entry file is reported instead of being silently truncated.
	huge := "# Memory\n\n## Index\n- OVERSIZED-MARKER\n" + strings.Repeat("x", 3<<20)
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte(huge), 0o644); err != nil {
		t.Fatalf("write oversized entry: %v", err)
	}
	gitOutput(t, env, repo, "add", "MEMORY.md")
	gitOutput(t, env, repo, "commit", "-m", "oversized entry")

	oversized := runMemctlStdin(t, bin, home, envWith(env, "AGENT_MEMORY_SID=oversized-session"), `{}`, "context", "--stdin")
	if oversized.code != 0 {
		t.Fatalf("context: code=%d stderr=%q", oversized.code, oversized.stderr)
	}
	if !strings.Contains(oversized.stdout, "exceeds the 2097152 byte injection limit") {
		t.Fatalf("oversized entry was not reported:\n%s", oversized.stdout)
	}
	if len(oversized.stdout) > 300<<10 {
		t.Fatalf("oversized entry was inlined (%d bytes)", len(oversized.stdout))
	}
}

// readSessionRecord loads the persisted cache record for one session tuple.
func readSessionRecord(t *testing.T, home, repo, agent, sid string) state.SessionRecord {
	t.Helper()
	repoID := config.RepoID(repo)
	key := strings.TrimPrefix(repoID, "sha256:")
	path := filepath.Join(home, ".agent-memory", "repos", key, "cache", "sessions", state.SessionKey(repoID, agent, sid)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session record: %v", err)
	}
	var record state.SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode session record: %v", err)
	}
	return record
}

// TestDeltaThreadIDProvidesTheSessionIdentity covers the Delta host: it has no
// lifecycle hook, so the only session identity is DELTA_CURRENT_THREAD_ID,
// which Delta injects into every command the agent runs. The variable must feed
// the same provenance path as the DSH variables: a bare `tag` resolves, and a
// managed write records the thread as the Session trailer. See
// docs/13-delta-integration.md section 4.
func TestDeltaThreadIDProvidesTheSessionIdentity(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, baseEnv := a5LocalRepo(t, bin, root, "home")
	thread := "delta-thread-abc"
	env := envWith(baseEnv, "AGENT_MEMORY_SID=", "DELTA_CURRENT_THREAD_ID="+thread)

	tag := runMemctlStdin(t, bin, home, env, "", "tag", "--agent", "delta")
	if tag.code != 0 {
		t.Fatalf("tag: code=%d stderr=%q", tag.code, tag.stderr)
	}
	if want := "agent://test-device/delta/" + thread; !strings.Contains(tag.stdout, want) {
		t.Fatalf("tag = %q, want %q", tag.stdout, want)
	}

	// The context body must report the injected thread instead of an
	// unavailable source tag. The agent name comes from AGENT_MEMORY_AGENT of
	// the harness, so only the session segment proves the Delta fallback.
	context := runMemctlStdin(t, bin, home, env, `{}`, "context", "--json", "--stdin")
	if context.code != 0 {
		t.Fatalf("context: code=%d stderr=%q", context.code, context.stderr)
	}
	body := decodeEnvelope(t, context.stdout).HookSpecificOutput.AdditionalContext
	if want := "source tag: agent://test-device/test-agent/" + thread + "\n"; !strings.Contains(body, want) {
		t.Fatalf("context did not report %q:\n%s", want, body)
	}

	// A managed write without --sid must carry the thread into the commit
	// trailer, which is what the audit view reads.
	read := runMemctlStdin(t, bin, home, env, "", "read", "MEMORY.md", "--json")
	if read.code != 0 {
		t.Fatalf("read: code=%d stderr=%q", read.code, read.stderr)
	}
	var payload writeReadPayload
	if err := json.Unmarshal([]byte(read.stdout), &payload); err != nil {
		t.Fatalf("decode read: %v\n%s", err, read.stdout)
	}
	write := runMemctlStdin(t, bin, home, env, "# Memory\n\n## Index\n- probe\n",
		"write", "MEMORY.md", "--base", payload.FileRevision, "--stdin", "--agent", "delta")
	if write.code != 0 {
		t.Fatalf("write: code=%d stderr=%q", write.code, write.stderr)
	}
	log := exec.Command("git", "-C", repo, "log", "-1", "--format=%B")
	log.Env = os.Environ()
	out, err := log.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "Session: "+thread) {
		t.Fatalf("commit trailer is missing the Delta thread:\n%s", out)
	}
}
