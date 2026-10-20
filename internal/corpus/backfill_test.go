package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backfillHome builds an isolated HOME with one store per verified host and
// returns it. Every session is placed in the current project unless the test
// says otherwise, so a check about project filtering always has a control.
func backfillHome(t *testing.T, cwd string) string {
	t.Helper()
	home := t.TempDir()
	writeClaudeSession(t, home, cwd, "claude-session", "记住：发布只推标签")
	writePiSession(t, home, cwd, "pi-session", "以后都用 pnpm 而不是 npm")
	writeDSHSession(t, home, cwd, "dsh-session", "DSH 的 MCP 客户端随应用内置")
	writeCodexSession(t, home, cwd, "codex-session", "不要用 make release 发布")
	writeAntigravitySession(t, home, cwd, "agy-session", "以后审查报告统一用中文")
	return home
}

// writeNested creates path and writes content, returning the path.
func writeNested(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// stamp sets a file's modification time so the TTL and the mtime order are
// controllable.
func stamp(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// claudeProjectDir is the store directory Claude Code uses for one working
// directory.
func claudeProjectDir(cwd string) string {
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

// writeClaudeSession writes one Claude Code transcript.
func writeClaudeSession(t *testing.T, home, cwd, sid, user string) string {
	t.Helper()
	lines := []string{
		`{"type":"user","timestamp":"2026-10-20T01:00:00Z","cwd":"` + cwd + `","sessionId":"` + sid + `","message":{"role":"user","content":"` + user + `"}}`,
		`{"type":"assistant","timestamp":"2026-10-20T01:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"收到。"}]}}`,
	}
	return writeNested(t, filepath.Join(home, ".claude", "projects", claudeProjectDir(cwd), sid+".jsonl"), strings.Join(lines, "\n"))
}

// writePiSession writes one pi transcript.
func writePiSession(t *testing.T, home, cwd, sid, user string) string {
	t.Helper()
	lines := []string{
		`{"type":"session","version":3,"id":"` + sid + `","timestamp":"2026-10-20T02:00:00Z","cwd":"` + cwd + `"}`,
		`{"type":"message","timestamp":"2026-10-20T02:00:01Z","message":{"role":"user","content":[{"type":"text","text":"` + user + `"}]}}`,
		`{"type":"message","timestamp":"2026-10-20T02:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"好的。"}]}}`,
	}
	dir := "--" + strings.Trim(strings.ReplaceAll(cwd, "/", "-"), "-") + "--"
	return writeNested(t, filepath.Join(home, ".pi", "agent", "sessions", dir, "2026-10-20T02-00-00-000Z_"+sid+".jsonl"), strings.Join(lines, "\n"))
}

// writeDSHSession writes one compressed DSH session record.
func writeDSHSession(t *testing.T, home, cwd, sid, user string) string {
	t.Helper()
	lines := []string{
		`{"type":"session","version":4,"id":"` + sid + `","cwd":"` + cwd + `"}`,
		`{"type":"user/message","time":1792371962934,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"` + user + `"}]}}`,
		`{"type":"assistant/message","time":1792371963934,"data":{"message":{"role":"assistant","content":[{"type":"text","text":"已记下。"}]}}}`,
	}
	dir := filepath.Join(home, ".dsh", "sessions", escapeDSH(cwd), sid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "session.v4.jsonl.zstd")
	writeZstdTo(t, path, strings.Join(lines, "\n"))
	return path
}

// writeCodexSession writes one Codex rollout under the date-partitioned store.
func writeCodexSession(t *testing.T, home, cwd, sid, user string) string {
	t.Helper()
	lines := []string{
		`{"type":"session_meta","timestamp":"2026-10-20T03:00:00Z","payload":{"id":"` + sid + `","cwd":"` + cwd + `"}}`,
		`{"type":"response_item","timestamp":"2026-10-20T03:00:02Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + user + `"}]}}`,
		`{"type":"event_msg","timestamp":"2026-10-20T03:00:05Z","payload":{"type":"task_complete","last_agent_message":"明白。"}}`,
	}
	return writeNested(t, filepath.Join(home, ".codex", "sessions", "2026", "10", "20", "rollout-"+sid+".jsonl"), strings.Join(lines, "\n"))
}

// writeAntigravitySession writes one Antigravity transcript. The working
// directory only appears in the run_command arguments, which is where the
// backfill reads it from.
func writeAntigravitySession(t *testing.T, home, cwd, sid, user string) string {
	t.Helper()
	lines := []string{
		`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-20T04:00:00Z","content":"<USER_REQUEST>\n` + user + `\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: 2026-10-20T12:00:00+08:00.\n</ADDITIONAL_METADATA>\n<USER_SETTINGS_CHANGE>\nThe user changed setting ` + "`Model Selection`" + `.\n</USER_SETTINGS_CHANGE>"}`,
		`{"step_index":1,"source":"SYSTEM","type":"CONVERSATION_HISTORY","status":"DONE","created_at":"2026-10-20T04:00:00Z","content":"# Conversation History\nrecent conversations"}`,
		`{"step_index":2,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-20T04:00:01Z","thinking":"private reasoning","content":"已记下。","tool_calls":[{"name":"run_command","args":{"CommandLine":"\"go test ./...\"","Cwd":"\"` + cwd + `\"","toolAction":"\"Running tests\"","toolSummary":"\"Tests\""}}]}`,
		`{"step_index":3,"source":"SYSTEM","type":"CHECKPOINT","status":"DONE","created_at":"2026-10-20T04:00:02Z","content":"# Resuming from a compaction"}`,
	}
	return writeNested(t, filepath.Join(home, ".gemini", "antigravity", "brain", sid, ".system_generated", "logs", "transcript.jsonl"), strings.Join(lines, "\n"))
}

// writeZstdTo compresses content into a zstd session file.
func writeZstdTo(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	encoded, err := compressZstd([]byte(content))
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestBackfillCollectsCurrentProjectCoversD06 sweeps one store per verified
// host and checks that every session of the current project enters the pool
// with its own agent name and real session id.
func TestBackfillCollectsCurrentProjectCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := backfillHome(t, cwd)
	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 5 {
		t.Fatalf("collected = %d, want 5 (%+v)", result.Collected, result)
	}
	for _, want := range []struct{ agent, sid, text string }{
		{"claude-code", "claude-session", "发布只推标签"},
		{"pi", "pi-session", "pnpm"},
		{"dsh", "dsh-session", "MCP 客户端"},
		{"codex", "codex-session", "make release"},
		{"antigravity", "agy-session", "审查报告"},
	} {
		digest, ok := cache.Load(want.agent, want.sid)
		if !ok {
			t.Fatalf("%s/%s was not collected", want.agent, want.sid)
		}
		if digest.CwdKey != CwdKey(cwd) || digest.CwdBase != "memctl" {
			t.Fatalf("%s project = %q/%q", want.agent, digest.CwdBase, digest.CwdKey)
		}
		if len(digest.Turns) == 0 || !strings.Contains(digest.Turns[0].User, want.text) {
			t.Fatalf("%s turns = %+v", want.agent, digest.Turns)
		}
		if digest.SourceTag != "agent://testhost/"+want.agent+"/"+want.sid {
			t.Fatalf("%s source tag = %q", want.agent, digest.SourceTag)
		}
	}
}

// TestBackfillSkipsOtherProjectsCoversD06 checks the project filter: a store
// that is not keyed by project must be closed as soon as its metadata names
// another directory, and its sessions must not take a slot.
func TestBackfillSkipsOtherProjectsCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	writeClaudeSession(t, home, cwd, "mine", "记住：默认分支是 main")
	writePiSession(t, home, "/work/other", "theirs-pi", "不相关")
	writeCodexSession(t, home, "/work/other", "theirs-codex", "不相关")
	writeAntigravitySession(t, home, "/work/other", "theirs-agy", "不相关")
	// A sibling clone with the same basename is a different project: only the
	// cwdKey tells them apart.
	writeDSHSession(t, home, "/other/checkout/memctl", "theirs-dsh", "不相关")

	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 1 {
		t.Fatalf("collected = %d, want only the current project (%+v)", result.Collected, result)
	}
	if digest, ok := cache.Load("claude-code", "mine"); !ok || digest.CwdKey != CwdKey(cwd) {
		t.Fatal("the current project's session was not collected")
	}
	for _, other := range []struct{ agent, sid string }{
		{"pi", "theirs-pi"}, {"codex", "theirs-codex"},
		{"antigravity", "theirs-agy"}, {"dsh", "theirs-dsh"},
	} {
		if _, ok := cache.Load(other.agent, other.sid); ok {
			t.Fatalf("%s/%s from another project was collected", other.agent, other.sid)
		}
	}
	if result.SkippedProject == 0 {
		t.Fatal("no log was reported as another project")
	}
}

// TestBackfillKeepsConsumedDigestsCoversD06 checks that a sweep never
// overwrites a digest that a proposal already consumed.
func TestBackfillKeepsConsumedDigestsCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	writePiSession(t, home, cwd, "consumed", "旧的会话内容")
	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	original := Digest{
		SchemaVersion:    SchemaVersion,
		Agent:            "pi",
		SessionID:        "consumed",
		SourceTag:        "agent://testhost/pi/consumed",
		Date:             "2026-10-19",
		UpdatedAt:        now.Add(-time.Hour).Format(time.RFC3339),
		CwdBase:          "memctl",
		CwdKey:           CwdKey(cwd),
		ConsumedBy:       "dp-1",
		ConsumedRevision: "sha256:old",
		Turns:            []Turn{{User: "已提炼过"}},
	}
	if err := cache.Save(original); err != nil {
		t.Fatalf("save: %v", err)
	}
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 0 {
		t.Fatalf("collected = %d, want 0", result.Collected)
	}
	loaded, ok := cache.Load("pi", "consumed")
	if !ok {
		t.Fatal("the digest disappeared")
	}
	if loaded.ConsumedBy != "dp-1" || loaded.ConsumedRevision != "sha256:old" || len(loaded.Turns) != 1 {
		t.Fatalf("the digest was overwritten: %+v", loaded)
	}
}

// TestBackfillMarksOversizedLogTruncatedCoversD06 checks that a log above the
// per-file limit still produces a bounded digest marked truncated.
func TestBackfillMarksOversizedLogTruncatedCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	var builder strings.Builder
	builder.WriteString(`{"type":"session","version":3,"id":"big","cwd":"` + cwd + `"}` + "\n")
	for index := 0; index < 12000; index++ {
		builder.WriteString(`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` +
			strings.Repeat("填充文本", 60) + `"}]}}` + "\n")
		builder.WriteString(`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}` + "\n")
	}
	path := writePiSession(t, home, cwd, "big", "")
	writeNested(t, path, builder.String())

	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 1 {
		t.Fatalf("collected = %d (%+v)", result.Collected, result)
	}
	digest, ok := cache.Load("pi", "big")
	if !ok {
		t.Fatal("no digest")
	}
	if !digest.Truncated {
		t.Fatal("an oversized log must be marked truncated")
	}
	info, err := os.Stat(cache.Path("pi", "big"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > MaxSessionDigestBytes {
		t.Fatalf("digest = %d bytes, over %d", info.Size(), MaxSessionDigestBytes)
	}
}

// TestBackfillStopsAtFileLimitCoversD06 checks the per-run file cap: the newest
// sessions are collected and the older ones are left for the next run. Logs of
// another project do not take a slot, which is the documented "unrelated
// projects do not crowd out the quota" property.
func TestBackfillStopsAtFileLimitCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	// More recent logs of other projects than the whole per-run cap: a sweep
	// that counted them would collect nothing.
	for index := 0; index < MaxBackfillFilesPerRun+10; index++ {
		path := writeCodexSession(t, home, "/work/other", fmt.Sprintf("other-%02d", index), "不相关")
		stamp(t, path, now.Add(-time.Duration(index+1)*time.Second))
	}
	for index := 0; index < MaxBackfillFilesPerRun+5; index++ {
		sid := fmt.Sprintf("session-%02d", index)
		path := writeClaudeSession(t, home, cwd, sid, "普通问题 "+sid)
		stamp(t, path, now.Add(-time.Duration(index+1)*time.Minute))
	}
	cache := OpenCache(t.TempDir(), "sha256:repo")
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Files != MaxBackfillFilesPerRun {
		t.Fatalf("files = %d, want %d", result.Files, MaxBackfillFilesPerRun)
	}
	if result.Collected != MaxBackfillFilesPerRun {
		t.Fatalf("collected = %d, want %d", result.Collected, MaxBackfillFilesPerRun)
	}
	if result.TimedOut {
		t.Fatal("hitting the file cap is not a timeout")
	}
	// The newest session is in the pool and the oldest is not.
	if _, ok := cache.Load("claude-code", "session-00"); !ok {
		t.Fatal("the newest session was not collected")
	}
	if _, ok := cache.Load("claude-code", fmt.Sprintf("session-%02d", MaxBackfillFilesPerRun+4)); ok {
		t.Fatal("a session past the file cap was collected")
	}
}

// TestBackfillStopsAtBudgetCoversD06 checks the wall-clock budget: the sweep
// stops without an error and keeps what it already distilled.
func TestBackfillStopsAtBudgetCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 6; index++ {
		sid := fmt.Sprintf("session-%02d", index)
		path := writeClaudeSession(t, home, cwd, sid, "普通问题 "+sid)
		stamp(t, path, now.Add(-time.Duration(index+1)*time.Minute))
	}
	cache := OpenCache(t.TempDir(), "sha256:repo")
	// Every clock read advances a second, so the 3s budget is gone before the
	// first session is opened.
	clock := now.Add(-time.Second)
	result := Backfill(cache, BackfillRequest{
		Cwd:    cwd,
		Device: "testhost",
		Home:   home,
		Now:    func() time.Time { clock = clock.Add(time.Second); return clock },
		Budget: 3 * time.Second,
	})
	if !result.TimedOut {
		t.Fatalf("the budget did not stop the sweep: %+v", result)
	}
	if result.Collected > 6 {
		t.Fatalf("collected = %d", result.Collected)
	}
}

// TestBackfillDisabledCoversD06 checks AGENT_MEMORY_CORPUS=0: the sweep reads
// and writes nothing.
func TestBackfillDisabledCoversD06(t *testing.T) {
	t.Setenv(EnvCorpus, "0")
	cwd := "/work/memctl"
	home := backfillHome(t, cwd)
	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 0 || result.Files != 0 {
		t.Fatalf("a disabled sweep did work: %+v", result)
	}
	if entries, err := os.ReadDir(cache.Dir()); err == nil && len(entries) > 0 {
		t.Fatalf("a disabled sweep wrote %d files", len(entries))
	}
}

// TestBackfillSkipsStaleSessions checks the 14-day TTL: a store entry older
// than the window is not collected.
func TestBackfillSkipsStaleSessions(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	stale := writeClaudeSession(t, home, cwd, "stale", "旧会话")
	stamp(t, stale, now.Add(-CorpusStateTTL-time.Hour))
	fresh := writePiSession(t, home, cwd, "fresh", "新会话")
	stamp(t, fresh, now.Add(-time.Hour))

	cache := OpenCache(t.TempDir(), "sha256:repo")
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 1 {
		t.Fatalf("collected = %d, want only the fresh session", result.Collected)
	}
	if _, ok := cache.Load("claude-code", "stale"); ok {
		t.Fatal("a session outside the TTL was collected")
	}
}

// TestBackfillRecordsWroteMemory checks that a session which already produced a
// commit with its trailer is recorded as such.
func TestBackfillRecordsWroteMemory(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	writePiSession(t, home, cwd, "writer", "记住：这个会话会写入记忆")
	writePiSession(t, home, cwd, "reader", "只读会话")
	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	result := Backfill(cache, BackfillRequest{
		Cwd: cwd, Device: "testhost", Home: home,
		Now:         func() time.Time { return now },
		WroteMemory: func(sid string) bool { return sid == "writer" },
	})
	if result.Collected != 2 {
		t.Fatalf("collected = %d", result.Collected)
	}
	writer, _ := cache.Load("pi", "writer")
	if !writer.WroteMemoryInSess {
		t.Fatal("the writer session was not marked")
	}
	reader, _ := cache.Load("pi", "reader")
	if reader.WroteMemoryInSess {
		t.Fatal("the reader session was marked")
	}
}

// TestBackfillIsIdempotent checks that a second sweep finds everything in the
// pool and distils nothing again. Files counts the logs opened to decide, so a
// codex rollout still counts: its project is only known after opening it.
func TestBackfillIsIdempotent(t *testing.T) {
	cwd := "/work/memctl"
	home := backfillHome(t, cwd)
	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	req := BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }}
	first := Backfill(cache, req)
	second := Backfill(cache, req)
	if first.Collected == 0 || second.Collected != 0 {
		t.Fatalf("first = %+v, second = %+v", first, second)
	}
	// The sniffed hosts must open a log to learn its project, so a second
	// sweep still opens the codex rollout and the antigravity transcript; the
	// hosts whose store name carries the project open nothing.
	if second.Files > 2 {
		t.Fatalf("the second sweep opened %d logs", second.Files)
	}
}

// TestAntigravityDigestCoversD06 distills an Antigravity transcript and checks
// that only the explicit user request, the planner text and the tool signatures
// are kept.
func TestAntigravityDigestCoversD06(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	path := writeAntigravitySession(t, home, cwd, "agy-session", "记住：审查报告统一用中文")
	stream, err := openSessionStream(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer stream.Close()
	digest, ok := testCollector(t).Collect("antigravity", "agy-session", stream.reader)
	if !ok {
		t.Fatal("the antigravity transcript produced no digest")
	}
	if digest.SessionID != "agy-session" {
		t.Fatalf("session id = %q", digest.SessionID)
	}
	if digest.CwdKey != CwdKey(cwd) || digest.CwdBase != "memctl" {
		t.Fatalf("project = %q/%q, want the run_command cwd", digest.CwdBase, digest.CwdKey)
	}
	if len(digest.Turns) != 1 {
		t.Fatalf("turns = %+v", digest.Turns)
	}
	turn := digest.Turns[0]
	if !strings.Contains(turn.User, "审查报告统一用中文") {
		t.Fatalf("user text = %q", turn.User)
	}
	for _, hostText := range []string{"ADDITIONAL_METADATA", "USER_SETTINGS_CHANGE", "Conversation History", "compaction"} {
		if strings.Contains(turn.User, hostText) {
			t.Fatalf("host metadata %q leaked into the user text: %q", hostText, turn.User)
		}
	}
	if strings.Contains(turn.Assistant, "private reasoning") {
		t.Fatalf("a thinking field leaked: %q", turn.Assistant)
	}
	if !strings.Contains(turn.Assistant, "已记下") {
		t.Fatalf("assistant text = %q", turn.Assistant)
	}
	if len(turn.Tools) != 1 || !strings.Contains(turn.Tools[0], "go test ./...") {
		t.Fatalf("tools = %v", turn.Tools)
	}
}

// TestEscapeStoreNames checks the three store-name escapes the sweep derives
// against names observed on this machine. Every one is forward-only: the log's
// own working directory remains the authority on the project.
func TestEscapeStoreNames(t *testing.T) {
	dsh := map[string]string{
		"/Users/me/memctl":    "--Users-me-memctl--",
		"/Users/me/工作空间":      "--Users-me-~5DE5~4F5C~7A7A~95F4--",
		"/Users/me/04_产品":     "--Users-me-04_~4EA7~54C1--",
		"/Users/me/2027 维工项目": "--Users-me-2027~0020~7EF4~5DE5~9879~76EE--",
	}
	for cwd, want := range dsh {
		if got := escapeDSH(cwd); got != want {
			t.Fatalf("escapeDSH(%q) = %q, want %q", cwd, got, want)
		}
	}
	pi := map[string]string{
		"/Users/me/memctl":      "--Users-me-memctl--",
		"/Users/me/agent通用工作空间": "--Users-me-agent通用工作空间--",
	}
	for cwd, want := range pi {
		if got := piDir(cwd); got != want {
			t.Fatalf("piDir(%q) = %q, want %q", cwd, got, want)
		}
	}
	claude := map[string]string{
		"/Users/me/memctl":          "-Users-me-memctl",
		"/Users/me/agent通用工作空间":     "-Users-me-agent通用工作空间",
		"/Users/me/04_产品/One Drive": "-Users-me-04-产品-One-Drive",
	}
	for cwd, want := range claude {
		if got := claudeDir(cwd); got != want {
			t.Fatalf("claudeDir(%q) = %q, want %q", cwd, got, want)
		}
	}
}

// TestBackfillDSHStoreIsReadable checks that the derived DSH directory name
// points at the record the fixture wrote, so a real store is found as well.
func TestBackfillDSHStoreIsReadable(t *testing.T) {
	cwd := "/work/工作空间"
	home := t.TempDir()
	writeDSHSession(t, home, cwd, "session-a", "记住：这个项目用 pnpm")
	cache := OpenCache(t.TempDir(), "sha256:repo")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
	if result.Collected != 1 {
		t.Fatalf("collected = %d", result.Collected)
	}
	if _, ok := cache.Load("dsh", "session-a"); !ok {
		t.Fatal("the DSH session was not collected")
	}
}

// TestLocateSessionFindsKnownSessionCoversD07 covers the lookup `dream
// --prepare` uses to refresh the calling session before the sweep: every host
// store is found by agent and session id, and an unknown or missing session is
// reported as empty instead of guessing a path.
func TestLocateSessionFindsKnownSessionCoversD07(t *testing.T) {
	cwd := "/work/memctl"
	home := t.TempDir()
	claudePath := writeClaudeSession(t, home, cwd, "claude-session", "记住：发布只推标签")
	piPath := writePiSession(t, home, cwd, "pi-session", "以后都用 pnpm")
	dshPath := writeDSHSession(t, home, cwd, "dsh-session", "DSH 的 MCP 客户端随应用内置")
	codexPath := writeCodexSession(t, home, cwd, "codex-session", "不要用 make release 发布")
	agyPath := writeAntigravitySession(t, home, cwd, "agy-session", "以后审查报告统一用中文")

	cases := []struct {
		agent string
		sid   string
		want  string
	}{
		{"claude-code", "claude-session", claudePath},
		{"pi", "pi-session", piPath},
		{"dsh", "dsh-session", dshPath},
		{"codex", "codex-session", codexPath},
		{"antigravity", "agy-session", agyPath},
		{"claude-code", "unknown", ""},
		{"commandcode", "any", ""},
		{"claude-code", "", ""},
	}
	for _, testCase := range cases {
		got := LocateSession(testCase.agent, testCase.sid, cwd, home)
		if got != testCase.want {
			t.Errorf("LocateSession(%q, %q) = %q, want %q", testCase.agent, testCase.sid, got, testCase.want)
		}
	}
}
