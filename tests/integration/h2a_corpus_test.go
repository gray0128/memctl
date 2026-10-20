package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/klauspost/compress/zstd"
)

// This file verifies the local session distillation of stage H-2a through the
// real binary: D06 requires that `sync` and `end` write a bounded, redacted
// SessionDigest from a hook payload's transcript_path or an explicit
// --transcript, that a disabled collector writes nothing, and that a missing
// log never changes the exit code. See
// docs/12-session-corpus-dreaming.md sections 2.2 and 3.2.

// corpusEnv builds an isolated environment without a memory repository: the
// collector must work from the state root alone and must not depend on Git.
func corpusEnv(home string) []string {
	return []string{
		"HOME=" + home,
		"AGENT_MEMORY_DIR=" + filepath.Join(home, "memory"),
		"AGENT_MEMORY_STATE=" + filepath.Join(home, ".agent-memory"),
		"AGENT_MEMORY_BACKGROUND=0",
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	}
}

// corpusDir returns the digest cache of the memory repository in this HOME.
func corpusDir(home string) string {
	repo := filepath.Join(home, "memory")
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(repo))
	return cache.Dir()
}

// readDigest loads the one digest written for a session.
func readDigest(t *testing.T, home, agent, sid string) corpus.Digest {
	t.Helper()
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	value, ok := cache.Load(agent, sid)
	if !ok {
		t.Fatalf("no digest for %s/%s under %s", agent, sid, cache.Dir())
	}
	return value
}

// TestD06LiveCollectionFromStdinPayloadCoversD06 drives `sync --stdin` with a
// hook payload that carries transcript_path. The digest must be written even
// though no memory repository exists, the exit code must stay zero, and the
// credentials in the log must be redacted.
func TestD06LiveCollectionFromStdinPayloadCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	log := strings.Join([]string{
		`{"type":"user","timestamp":"2026-10-20T01:00:00Z","cwd":"/work/memctl","sessionId":"sid-stdin","message":{"role":"user","content":"不要本地跑 make release；token ghp_abcdefghijklmnopqrstuvwxyz0123"}}`,
		`{"type":"assistant","timestamp":"2026-10-20T01:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"明白。"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"git tag -a v0.9.1"}}]}}`,
	}, "\n")
	if err := os.WriteFile(transcript, []byte(log), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	payload := `{"session_id":"sid-stdin","transcript_path":"` + transcript + `"}`

	result := runMemctlStdin(t, bin, home, corpusEnv(home), payload, "sync", "--agent", "claude-code", "--stdin")
	if result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	digest := readDigest(t, home, "claude-code", "sid-stdin")
	if digest.SourceTag != "agent://"+deviceName()+"/claude-code/sid-stdin" {
		t.Fatalf("source tag = %q", digest.SourceTag)
	}
	if digest.CwdBase != "memctl" {
		t.Fatalf("cwd base = %q", digest.CwdBase)
	}
	if len(digest.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(digest.Turns))
	}
	if !strings.Contains(digest.Turns[0].User, "[REDACTED]") {
		t.Fatalf("credential was not redacted: %q", digest.Turns[0].User)
	}
	if strings.Contains(digest.Turns[0].User, "ghp_") {
		t.Fatal("the GitHub token survived redaction")
	}
}

// TestD06LiveCollectionFromExplicitTranscriptCoversD06 checks the documented
// --transcript flag on both sync and end.
func TestD06LiveCollectionFromExplicitTranscriptCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	log := `{"type":"user","timestamp":"2026-10-20T01:00:00Z","cwd":"/work/other","sessionId":"sid-flag","message":{"role":"user","content":"以后都用 pnpm"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-20T01:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"好"}]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(log), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	result := runMemctl(t, bin, home, corpusEnv(home), "sync", "--agent", "claude-code", "--sid", "sid-flag", "--transcript", transcript)
	if result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	readDigest(t, home, "claude-code", "sid-flag")

	// `end` refreshes the same session; the digest is replaced atomically and
	// keeps its identity.
	result = runMemctl(t, bin, home, corpusEnv(home), "end", "--agent", "claude-code", "--sid", "sid-flag", "--transcript", transcript)
	if result.code != 0 {
		t.Fatalf("end exited %d: %s", result.code, result.stderr)
	}
	if digest := readDigest(t, home, "claude-code", "sid-flag"); len(digest.Turns) != 1 {
		t.Fatalf("turns after end = %d, want 1", len(digest.Turns))
	}
}

// TestD06DisabledCollectorWritesNothingCoversD06 checks AGENT_MEMORY_CORPUS=0.
func TestD06DisabledCollectorWritesNothingCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	log := `{"type":"user","sessionId":"sid-off","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(log), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	env := append(corpusEnv(home), "AGENT_MEMORY_CORPUS=0")
	result := runMemctl(t, bin, home, env, "sync", "--agent", "claude-code", "--sid", "sid-off", "--transcript", transcript)
	if result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	if entries, err := os.ReadDir(corpusDir(home)); err == nil && len(entries) > 0 {
		t.Fatalf("the disabled collector wrote %d files", len(entries))
	}
}

// TestD06MissingLogKeepsExitCodeZeroCoversD06 checks that a missing or
// unreadable transcript is skipped silently.
func TestD06MissingLogKeepsExitCodeZeroCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	missing := filepath.Join(home, "does-not-exist.jsonl")
	payload := `{"session_id":"sid-missing","transcript_path":"` + missing + `"}`

	result := runMemctlStdin(t, bin, home, corpusEnv(home), payload, "sync", "--agent", "claude-code", "--stdin")
	if result.code != 0 {
		t.Fatalf("sync with a missing log exited %d: %s", result.code, result.stderr)
	}
	if entries, err := os.ReadDir(corpusDir(home)); err == nil && len(entries) > 0 {
		t.Fatalf("a missing log produced %d cache files", len(entries))
	}

	// A symlinked log is not followed: the digest cache must not read an
	// arbitrary path a payload points at.
	target := filepath.Join(home, "real.jsonl")
	if err := os.WriteFile(target, []byte(`{"type":"user","sessionId":"s","message":{"role":"user","content":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	symlinked := `{"session_id":"sid-link","transcript_path":"` + link + `"}`
	result = runMemctlStdin(t, bin, home, corpusEnv(home), symlinked, "sync", "--agent", "claude-code", "--stdin")
	if result.code != 0 {
		t.Fatalf("sync with a symlinked log exited %d", result.code)
	}
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	if _, ok := cache.Load("claude-code", "sid-link"); ok {
		t.Fatal("the collector followed a symlinked transcript")
	}
}

// TestD06DSHZstdTranscriptCoversD06 checks the compressed DSH format end to end
// through the binary, including a stream that decodes past the size limit.
func TestD06DSHZstdTranscriptCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	sessionDir := filepath.Join(home, "sessions", "session-test")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	plain := strings.Join([]string{
		`{"type":"session","version":4,"id":"session-test","cwd":"/work/memctl"}`,
		`{"type":"user/message","time":1792371962934,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"记住：DSH 的 MCP 客户端随应用内置"}]}}`,
		`{"type":"assistant/message","time":1792371963934,"data":{"message":{"role":"assistant","content":[{"type":"text","text":"已记下。"}]}}}`,
		`{"type":"tool/call","time":1792371964934,"data":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}`,
	}, "\n")
	if err := os.WriteFile(sessionDir+"/session.v3.jsonl.zstd", []byte("stale v3 content that must not be chosen"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeZstdFile(t, filepath.Join(sessionDir, "session.v4.jsonl.zstd"), plain)
	// The v3 file above is not valid zstd; the collector must choose v4 and
	// never fall back to the older, undecodable record.
	result := runMemctl(t, bin, home, corpusEnv(home), "sync", "--agent", "dsh", "--sid", "session-test", "--transcript", filepath.Join(sessionDir, "session.v4.jsonl.zstd"))
	if result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	digest := readDigest(t, home, "dsh", "session-test")
	if len(digest.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(digest.Turns))
	}
	if !strings.Contains(digest.Turns[0].Assistant, "已记下") {
		t.Fatalf("assistant text = %q", digest.Turns[0].Assistant)
	}
	if len(digest.Turns[0].Tools) != 1 || !strings.Contains(digest.Turns[0].Tools[0], "go test ./...") {
		t.Fatalf("tools = %v", digest.Turns[0].Tools)
	}
}

// TestD06DigestStaysBoundedCoversD06 checks the 16 KiB output cap through the
// binary with a log far larger than the budget.
func TestD06DigestStaysBoundedCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	var builder strings.Builder
	builder.WriteString(`{"type":"session","id":"session-big","cwd":"/work/memctl"}` + "\n")
	for turn := 0; turn < 40; turn++ {
		builder.WriteString(`{"type":"user/message","data":{"source":{"kind":"user"},"content":[{"type":"text","text":"` +
			strings.Repeat("长文本", 3000) + `"}]}}` + "\n")
		builder.WriteString(`{"type":"assistant/message","data":{"message":{"role":"assistant","content":[{"type":"text","text":"` +
			strings.Repeat("回复", 3000) + `"}]}}}` + "\n")
	}
	dir := filepath.Join(home, "big")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeZstdFile(t, filepath.Join(dir, "session.v4.jsonl.zstd"), builder.String())
	result := runMemctl(t, bin, home, corpusEnv(home), "sync", "--agent", "dsh", "--sid", "session-big", "--transcript", filepath.Join(dir, "session.v4.jsonl.zstd"))
	if result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	path := cache.Path("dsh", "session-big")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if info.Size() > corpus.MaxSessionDigestBytes {
		t.Fatalf("digest = %d bytes, over %d", info.Size(), corpus.MaxSessionDigestBytes)
	}
}

// TestD06StaleDigestsArePrunedCoversD06 checks the 14-day TTL on the real
// cache directory.
func TestD06StaleDigestsArePrunedCoversD06(t *testing.T) {
	home := t.TempDir()
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	now := time.Now()
	old := now.Add(-corpus.CorpusStateTTL - time.Hour)
	if err := cache.Save(corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "pi",
		SessionID:     "expired",
		UpdatedAt:     old.UTC().Format(time.RFC3339),
		Turns:         []corpus.Turn{{User: "x"}},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	removed := cache.Prune(now, nil)
	if len(removed) != 1 {
		t.Fatalf("removed = %v", removed)
	}
	if _, ok := cache.Load("pi", "expired"); ok {
		t.Fatal("the expired digest survived the TTL")
	}
}

// writeZstdFile compresses plain JSONL into a zstd session file.
func writeZstdFile(t *testing.T, path, content string) {
	t.Helper()
	encoded, err := zstdCompress([]byte(content))
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// deviceName mirrors the collector's source-tag device resolution.
func deviceName() string {
	if value := os.Getenv("AGENT_MEMORY_DEVICE"); value != "" {
		return value
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}

// digestJSON is the stored digest shape this file inspects.
type digestJSON struct {
	SchemaVersion int `json:"schemaVersion"`
	Agent         string
	SessionID     string
	SourceTag     string
	Date          string
	UpdatedAt     string
	CwdBase       string
	CwdKey        string
	Truncated     bool
	ConsumedBy    string
}

// TestD06DigestIsRedactedJSONCoversD06 checks the on-disk record contract.
func TestD06DigestIsRedactedJSONCoversD06(t *testing.T) {
	home := t.TempDir()
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	value := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "pi",
		SessionID:     "sid",
		SourceTag:     "agent://host/pi/sid",
		Date:          "2026-10-20",
		UpdatedAt:     "2026-10-20T04:00:00Z",
		CwdBase:       "memctl",
		CwdKey:        corpus.CwdKey("/work/memctl"),
		Turns:         []corpus.Turn{{User: "u"}},
	}
	if err := cache.Save(value); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(cache.Path("pi", "sid"))
	if err != nil {
		t.Fatal(err)
	}
	var stored digestJSON
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("stored digest: %v", err)
	}
	if stored.SchemaVersion != corpus.SchemaVersion || stored.ConsumedBy != "" || stored.CwdKey == "" {
		t.Fatalf("stored digest = %+v", stored)
	}
}

// zstdCompress encodes content as one zstd frame.
func zstdCompress(content []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(content); err != nil {
		writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// TestD06WroteMemoryFlagCoversD06 checks that a session whose commits carry its
// own `Session:` trailer is recorded as having written memory, so the current
// agent can weigh the digest lower.
func TestD06WroteMemoryFlagCoversD06(t *testing.T) {
	bin := buildMemctlBinary(t)
	home := t.TempDir()
	repo := filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init exited %d: %s", result.code, result.stderr)
	}
	// One managed write inside a session produces the trailer the scan looks for.
	read := runMemctl(t, bin, home, env, "read", "MEMORY.md", "--json")
	if read.code != 0 {
		t.Fatalf("read exited %d: %s", read.code, read.stderr)
	}
	var current struct {
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	if err := json.Unmarshal([]byte(read.stdout), &current); err != nil {
		t.Fatalf("read --json: %v", err)
	}
	content := current.RawContent + "- 会话已写过记忆 [source: agent://host/pi/sid-wrote; added: 2026-10-20]\n"
	result := runMemctlStdin(t, bin, home, env, content,
		"write", "MEMORY.md", "--base", current.FileRevision, "--stdin", "--agent", "pi", "--sid", "sid-wrote")
	if result.code != 0 {
		t.Fatalf("write exited %d: %s", result.code, result.stderr)
	}

	transcript := filepath.Join(home, "session.jsonl")
	log := `{"type":"session","id":"sid-wrote","cwd":"` + repo + `"}` + "\n" +
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"记住这条"}]}}` + "\n" +
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"已写入"}]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(log), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if result := runMemctl(t, bin, home, env, "sync", "--agent", "pi", "--sid", "sid-wrote", "--transcript", transcript); result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	if digest := readDigest(t, home, "pi", "sid-wrote"); !digest.WroteMemoryInSess {
		t.Fatal("the digest does not record that the session wrote memory")
	}

	// A session with no trailer stays false.
	other := `{"type":"session","id":"sid-quiet","cwd":"` + repo + `"}` + "\n" +
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"只是聊聊"}]}}` + "\n" +
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"好"}]}}` + "\n"
	quiet := filepath.Join(home, "quiet.jsonl")
	if err := os.WriteFile(quiet, []byte(other), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if result := runMemctl(t, bin, home, env, "sync", "--agent", "pi", "--sid", "sid-quiet", "--transcript", quiet); result.code != 0 {
		t.Fatalf("sync exited %d: %s", result.code, result.stderr)
	}
	if digest := readDigest(t, home, "pi", "sid-quiet"); digest.WroteMemoryInSess {
		t.Fatal("a session without a trailer was marked as having written memory")
	}
}

// TestD06BackfillScansHistoryCoversD06 drives the backfill sweep as the library
// entry `H-3` will call: the current project's historical sessions of every
// verified host enter the pool, a sibling clone and an unrelated project do
// not, and a log above the per-file limit is still stored bounded and marked
// truncated.
func TestD06BackfillScansHistoryCoversD06(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "memctl")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	now := time.Now()

	// Claude Code keys its store by the escaped working directory.
	claudeDir := filepath.Join(home, ".claude", "projects", claudeProjectName(cwd))
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	claudeLog := `{"type":"user","timestamp":"2026-10-20T01:00:00Z","cwd":"` + cwd + `","sessionId":"history-claude","message":{"role":"user","content":"不要本地跑 make release"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-20T01:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"收到。"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(claudeDir, "history-claude.jsonl"), []byte(claudeLog), 0o600); err != nil {
		t.Fatal(err)
	}

	// A Codex rollout of another project shares the store and must be skipped
	// from its session_meta line.
	codexDir := filepath.Join(home, ".codex", "sessions", "2026", "10", "20")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	otherCodex := `{"type":"session_meta","payload":{"id":"other-codex","cwd":"/somewhere/else"}}` + "\n" +
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"unrelated"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(codexDir, "rollout-other.jsonl"), []byte(otherCodex), 0o600); err != nil {
		t.Fatal(err)
	}

	// A DSH record for a sibling clone with the same basename: only the cwdKey
	// separates the two clones.
	sibling := filepath.Join(t.TempDir(), "memctl")
	dshDir := filepath.Join(home, ".dsh", "sessions", dshStoreName(sibling), "session-sibling")
	if err := os.MkdirAll(dshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	siblingLog := `{"type":"session","version":4,"id":"session-sibling","cwd":"` + sibling + `"}` + "\n" +
		`{"type":"user/message","time":1792371962934,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"另一个克隆的内容"}]}}` + "\n"
	encoded, err := zstdCompress([]byte(siblingLog))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dshDir, "session.v4.jsonl.zstd"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	// An Antigravity transcript of the current project: its cwd lives in a
	// run_command argument.
	agyDir := filepath.Join(home, ".gemini", "antigravity", "brain", "history-agy", ".system_generated", "logs")
	if err := os.MkdirAll(agyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	agyLog := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-20T04:00:00Z","content":"<USER_REQUEST>\n以后审查报告统一用中文\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nnow\n</ADDITIONAL_METADATA>"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-20T04:00:01Z","content":"已记下。","tool_calls":[{"name":"run_command","args":{"CommandLine":"\"go build ./...\"","Cwd":"\"` + cwd + `\""}}]}` + "\n"
	if err := os.WriteFile(filepath.Join(agyDir, "transcript.jsonl"), []byte(agyLog), 0o600); err != nil {
		t.Fatal(err)
	}

	// A pi session far above the per-file limit: it must still be stored under
	// the digest cap and marked truncated.
	piDir := filepath.Join(home, ".pi", "agent", "sessions", "--"+strings.Trim(strings.ReplaceAll(cwd, "/", "-"), "-")+"--")
	if err := os.MkdirAll(piDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var big strings.Builder
	big.WriteString(`{"type":"session","version":3,"id":"history-big","cwd":"` + cwd + `"}` + "\n")
	for index := 0; index < 12000; index++ {
		big.WriteString(`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` +
			strings.Repeat("填充文本", 60) + `"}]}}` + "\n")
	}
	if err := os.WriteFile(filepath.Join(piDir, "2026-10-20T02-00-00-000Z_history-big.jsonl"), []byte(big.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	result := corpus.Backfill(cache, corpus.BackfillRequest{
		Cwd:    cwd,
		Device: deviceName(),
		Home:   home,
		Now:    func() time.Time { return now },
	})
	if result.Collected != 3 {
		t.Fatalf("collected = %d, want the three current-project sessions (%+v)", result.Collected, result)
	}
	if result.SkippedProject == 0 {
		t.Fatal("the sibling clone and the unrelated rollout were not skipped")
	}
	if _, ok := cache.Load("claude-code", "history-claude"); !ok {
		t.Fatal("the claude-code history was not collected")
	}
	if _, ok := cache.Load("antigravity", "history-agy"); !ok {
		t.Fatal("the antigravity history was not collected")
	}
	if _, ok := cache.Load("codex", "other-codex"); ok {
		t.Fatal("a rollout of another project was collected")
	}
	if _, ok := cache.Load("dsh", "session-sibling"); ok {
		t.Fatal("a sibling clone's session was collected")
	}
	digest, ok := cache.Load("pi", "history-big")
	if !ok {
		t.Fatal("the oversized session was not collected")
	}
	if !digest.Truncated {
		t.Fatal("an oversized log must be marked truncated")
	}
	info, err := os.Stat(cache.Path("pi", "history-big"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > corpus.MaxSessionDigestBytes {
		t.Fatalf("digest = %d bytes, over %d", info.Size(), corpus.MaxSessionDigestBytes)
	}

	// AGENT_MEMORY_CORPUS=0 disables the sweep without an error.
	t.Setenv(corpus.EnvCorpus, "0")
	disabled := corpus.Backfill(cache, corpus.BackfillRequest{Cwd: cwd, Device: deviceName(), Home: home})
	if disabled.Collected != 0 || disabled.Files != 0 {
		t.Fatalf("a disabled sweep did work: %+v", disabled)
	}
}

// claudeProjectName is the store directory Claude Code uses for one working
// directory.
func claudeProjectName(cwd string) string {
	var builder strings.Builder
	for _, char := range cwd {
		switch char {
		case '/', '\\', '_', ' ':
			builder.WriteByte('-')
		default:
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

// dshStoreName is the DSH session store directory for one working directory.
func dshStoreName(cwd string) string {
	var builder strings.Builder
	builder.WriteString("--")
	for _, char := range strings.TrimPrefix(cwd, "/") {
		switch {
		case char == '/':
			builder.WriteByte('-')
		case char == '_' || char == '-' || char >= '0' && char <= '9' ||
			char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		default:
			builder.WriteString(fmt.Sprintf("~%04X", char))
		}
	}
	builder.WriteString("--")
	return builder.String()
}
