package corpus

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// deltaLine formats one watch envelope the way Delta emits it. The tests build
// streams from these so an assertion reads like the recorded conversation.
func deltaLine(kind, payload string) string {
	return `{"type":"` + kind + `"` + payload + "}"
}

// deltaItem renders one item envelope.
func deltaItem(kind string, millis int64, content string) string {
	return `{"type":"item","item":{"id":"i","kind":"` + kind + `","turn_id":"t",` +
		`"created_at":` + itoa(millis) + `,"content":` + content + "}}"
}

// itoa formats a small integer without importing strconv into the assertions.
func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// deltaReady is the stream's opening marker.
func deltaReady(threadID string) string {
	return `{"type":"ready","thread_id":"` + threadID + `","producer":null}`
}

// deltaLive is the marker that ends a complete stream.
const deltaLive = `{"type":"live"}`

// deltaStreamReader renders a set of lines as a readable stream.
func deltaStreamReader(lines ...string) io.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

// TestParseDeltaCoversX13 walks the documented parse rules: user messages open
// turns, the longest agent message is kept per turn, tool calls become
// signatures, and reasons, host summaries and raw output are all dropped.
func TestParseDeltaCoversX13(t *testing.T) {
	reader := deltaStreamReader(
		deltaReady("thread-abc"),
		// Agent text before the first user message is the host preface.
		deltaItem("agent_message", 1000, `{"text":"preface"}`),
		deltaItem("user_message", 2000, `{"text":"调研 Delta 接入"}`),
		deltaItem("agent_thought", 2000, `{"text":"the user asks about hooks"}`),
		deltaItem("tool_call", 2000, `{"toolCallId":"c1","name":"terminal","title":"ls docs","status":"completed","rawInput":{"command":"ls docs"},"rawOutput":"`+strings.Repeat("x", 4096)+`"}`),
		// Two agent messages: the shorter note first, then the answer. The
		// answer is longer, so it is the one kept.
		deltaItem("agent_message", 2000, `{"text":"先核对一下。"}`),
		deltaItem("agent_message", 2000, `{"text":"接入结论：用 CLI，不用 Hook。"}`),
		deltaItem("external_edits", 2000, `{"text":"<external-edits>...</external-edits>"}`),
		deltaItem("user_message", 3000, `{"text":"发布 release"}`),
		deltaItem("compaction", 3000, `{"text":"# 会话摘要"}`),
		deltaItem("tool_call", 3000, `{"toolCallId":"c2","name":"terminal","title":"git tag v1","status":"failed","rawInput":{"command":"git tag v1"}}`),
		deltaItem("agent_message", 3000, `{"text":"已推送。"}`),
		deltaLive,
	)
	digest, ok := testCollector(t).Collect("delta", "", reader)
	if !ok {
		t.Fatal("the watch stream produced no digest")
	}
	if digest.SessionID != "thread-abc" {
		t.Fatalf("session id = %q, want the ready marker's thread id", digest.SessionID)
	}
	if digest.SourceTag != "agent://testhost/delta/thread-abc" {
		t.Fatalf("source tag = %q", digest.SourceTag)
	}
	// Delta reports no working directory of its own, so the digest must fall
	// back to the collector's project: that is how a Delta session is filed.
	if digest.CwdBase != "fallback" || digest.CwdKey != CwdKey("/work/fallback") {
		t.Fatalf("project fields = %q/%q, want the collector fallback", digest.CwdBase, digest.CwdKey)
	}
	if len(digest.Turns) != 2 {
		t.Fatalf("turns = %d, want 2 (%+v)", len(digest.Turns), digest.Turns)
	}

	first := digest.Turns[0]
	if first.User != "调研 Delta 接入" {
		t.Fatalf("first user = %q", first.User)
	}
	if first.Assistant != "接入结论：用 CLI，不用 Hook。" {
		t.Fatalf("first assistant = %q, want the longest agent message", first.Assistant)
	}
	if strings.Contains(first.Assistant, "the user asks") || strings.Contains(first.Assistant, "preface") {
		t.Fatalf("reasoning or preface leaked into the turn: %q", first.Assistant)
	}
	if len(first.Tools) != 1 || first.Tools[0] != "terminal: ls docs (completed)" {
		t.Fatalf("first tools = %v, want one locator without output", first.Tools)
	}

	second := digest.Turns[1]
	if second.Assistant != "已推送。" {
		t.Fatalf("second assistant = %q", second.Assistant)
	}
	if len(second.Tools) != 1 || second.Tools[0] != "terminal: git tag v1 (failed)" {
		t.Fatalf("second tools = %v", second.Tools)
	}

	// The compaction text is the host's own summary, not a turn.
	for _, turn := range digest.Turns {
		if strings.Contains(turn.Assistant, "会话摘要") || strings.Contains(turn.User, "会话摘要") {
			t.Fatalf("host summary leaked into a turn: %+v", turn)
		}
	}
}

// TestParseDeltaKeepsLongestAgentMessageCoversX13 pins the tie-break and the
// reason the rule exists: concatenating a turn's messages overflows
// MaxTurnTextBytes, and truncating the concatenation would drop the answer.
func TestParseDeltaKeepsLongestAgentMessageCoversX13(t *testing.T) {
	note := strings.Repeat("а", 40) // progress note
	answer := strings.Repeat("б", 900)
	reader := deltaStreamReader(
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"问题"}`),
		deltaItem("agent_message", 1000, `{"text":"`+note+`"}`),
		deltaItem("agent_message", 1000, `{"text":"`+answer+`"}`),
		deltaItem("agent_message", 1000, `{"text":"`+note+`"}`),
		deltaLive,
	)
	parsed, ok := parseDelta(Collector{}, reader)
	if !ok || len(parsed.turns) != 1 {
		t.Fatalf("turns = %+v", parsed.turns)
	}
	got := parsed.turns[0].Assistant
	if !strings.HasPrefix(got, "б") {
		t.Fatalf("assistant kept the wrong message: %q", got[:min(20, len(got))])
	}
	if len(note)+len(answer) <= MaxTurnTextBytes {
		t.Fatal("the fixture no longer overflows the text limit, so it proves nothing")
	}
}

// TestParseDeltaRequiresEndMarkerCoversX13 checks that a stream without the live
// marker is not treated as a complete thread.
func TestParseDeltaRequiresEndMarkerCoversX13(t *testing.T) {
	truncated := deltaStreamReader(
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"问题"}`),
		deltaItem("agent_message", 1000, `{"text":"答"}`),
	)
	parsed, ok := parseDelta(Collector{}, truncated)
	if !ok || len(parsed.turns) != 1 {
		t.Fatalf("turns = %+v", parsed.turns)
	}
	if parsed.sawEnd {
		t.Fatal("a stream without the live marker reported a complete read")
	}

	complete := deltaStreamReader(
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"问题"}`),
		deltaItem("agent_message", 1000, `{"text":"答"}`),
		deltaLive,
	)
	parsed, ok = parseDelta(Collector{}, complete)
	if !ok || !parsed.sawEnd {
		t.Fatalf("a complete stream = %+v ok=%v", parsed, ok)
	}
}

// TestParseDeltaSkipsInjectedUserTextCoversX13 checks the shared injected-context
// rule applies to Delta user messages too.
func TestParseDeltaSkipsInjectedUserTextCoversX13(t *testing.T) {
	reader := deltaStreamReader(
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"<system-reminder>ignore this</system-reminder>"}`),
		deltaItem("agent_message", 1000, `{"text":"答"}`),
		deltaLive,
	)
	if _, ok := parseDelta(Collector{}, reader); ok {
		t.Fatal("an injected user message became a turn")
	}
}

// deltaCache builds an empty digest cache for the collection tests.
func deltaCache(t *testing.T) *Cache {
	t.Helper()
	return OpenCache(t.TempDir(), "sha256:repo")
}

// deltaRequest is the request the collection tests share.
func deltaRequest() DeltaRequest {
	return DeltaRequest{
		ThreadID: "thread-abc",
		Cwd:      "/work/memctl",
		Device:   "testhost",
		Now:      func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	}
}

// deltaRunnerOf returns a runner that serves one fixed stream and counts how
// often it was called.
func deltaRunnerOf(stream string, calls *int) DeltaRunner {
	return func(context.Context, string) (io.ReadCloser, error) {
		if calls != nil {
			*calls++
		}
		return io.NopCloser(strings.NewReader(stream)), nil
	}
}

// TestCollectDeltaWritesOneDigestCoversX13 covers the happy path end to end.
func TestCollectDeltaWritesOneDigestCoversX13(t *testing.T) {
	SetSuspiciousMatcher(func(text string) bool { return false })
	t.Cleanup(func() { SetSuspiciousMatcher(nil) })

	cache := deltaCache(t)
	stream := strings.Join([]string{
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"记住：发布只推标签"}`),
		deltaItem("agent_message", 1000, `{"text":"明白。"}`),
		deltaLive,
	}, "\n") + "\n"

	digest, ok, err := CollectDelta(context.Background(), deltaRunnerOf(stream, nil), cache, deltaRequest())
	if err != nil || !ok {
		t.Fatalf("CollectDelta = %v/%v/%v", digest, ok, err)
	}
	if digest.Agent != "delta" || digest.SessionID != "thread-abc" {
		t.Fatalf("digest identity = %s/%s", digest.Agent, digest.SessionID)
	}
	if digest.CwdKey != CwdKey("/work/memctl") {
		t.Fatalf("cwdKey = %q, want the request's project", digest.CwdKey)
	}
	if len(digest.Turns) != 1 || !strings.Contains(digest.Turns[0].User, "只推标签") {
		t.Fatalf("turns = %+v", digest.Turns)
	}
	stored, found := cache.Load("delta", "thread-abc")
	if !found || stored.SessionID != "thread-abc" {
		t.Fatalf("the digest was not cached: %+v", stored)
	}
}

// TestCollectDeltaCoversX14 walks every documented skip: no thread id, a failed
// runner, a stream cut before the live marker, a read that hits the byte limit,
// and a stream with nothing keepable. None may write a digest or return an
// error, because prepare must keep working with whatever the cache holds.
func TestCollectDeltaCoversX14(t *testing.T) {
	SetSuspiciousMatcher(func(text string) bool { return false })
	t.Cleanup(func() { SetSuspiciousMatcher(nil) })

	complete := strings.Join([]string{
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"问题"}`),
		deltaItem("agent_message", 1000, `{"text":"答"}`),
		deltaLive,
	}, "\n") + "\n"

	cases := []struct {
		name   string
		req    DeltaRequest
		runner DeltaRunner
	}{
		{
			name:   "no thread id",
			req:    DeltaRequest{Cwd: "/work/memctl"},
			runner: deltaRunnerOf(complete, nil),
		},
		{
			name: "runner failure",
			req:  deltaRequest(),
			runner: func(context.Context, string) (io.ReadCloser, error) {
				return nil, errors.New("delta: executable file not found")
			},
		},
		{
			name:   "nil runner",
			req:    deltaRequest(),
			runner: nil,
		},
		{
			name: "cut before the live marker",
			req:  deltaRequest(),
			runner: deltaRunnerOf(strings.Join([]string{
				deltaReady("thread-abc"),
				deltaItem("user_message", 1000, `{"text":"问题"}`),
			}, "\n")+"\n", nil),
		},
		{
			name: "nothing keepable",
			req:  deltaRequest(),
			runner: deltaRunnerOf(strings.Join([]string{
				deltaReady("thread-abc"),
				deltaItem("agent_thought", 1000, `{"text":"only reasoning"}`),
				deltaLive,
			}, "\n")+"\n", nil),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cache := deltaCache(t)
			digest, ok, err := CollectDelta(context.Background(), test.runner, cache, test.req)
			if ok || err != nil {
				t.Fatalf("CollectDelta = %+v ok=%v err=%v, want a silent skip", digest, ok, err)
			}
			if entries := cache.List(); len(entries) != 0 {
				t.Fatalf("a skipped collection wrote %d digests", len(entries))
			}
		})
	}
}

// TestCollectDeltaDropsStreamAboveTheByteLimitCoversX14 proves the byte cap
// behaves like the backfill cap: a stream that hits it is not cached, because a
// half digest would make the next run skip the thread as already collected.
func TestCollectDeltaDropsStreamAboveTheByteLimitCoversX14(t *testing.T) {
	SetSuspiciousMatcher(func(text string) bool { return false })
	t.Cleanup(func() { SetSuspiciousMatcher(nil) })

	// The bulk sits in a tool output, which the parser discards: the limit has to
	// stop the read, not the digest budget.
	bulk := strings.Repeat("y", MaxDeltaStreamBytes)
	stream := strings.Join([]string{
		deltaReady("thread-abc"),
		deltaItem("user_message", 1000, `{"text":"问题"}`),
		deltaItem("tool_call", 1000, `{"name":"terminal","rawOutput":"`+bulk+`"}`),
		deltaLive,
	}, "\n") + "\n"

	cache := deltaCache(t)
	if _, ok, err := CollectDelta(context.Background(), deltaRunnerOf(stream, nil), cache, deltaRequest()); ok || err != nil {
		t.Fatalf("an oversized stream was accepted: ok=%v err=%v", ok, err)
	}
	if entries := cache.List(); len(entries) != 0 {
		t.Fatalf("an oversized stream wrote %d digests", len(entries))
	}
}

// TestCollectDeltaHonoursContextCoversX14 checks a cancelled caller never starts
// the host CLI.
func TestCollectDeltaHonoursContextCoversX14(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, err := CollectDelta(ctx, deltaRunnerOf("", &calls), deltaCache(t), deltaRequest()); ok || err != nil {
		t.Fatalf("a cancelled collect = ok=%v err=%v", ok, err)
	}
	if calls != 0 {
		t.Fatalf("the runner was called %d times after cancellation", calls)
	}
}

// TestCollectDeltaPassesTheThreadIDCoversX13 pins the only argument the runner
// receives: the calling thread, never another one.
func TestCollectDeltaPassesTheThreadIDCoversX13(t *testing.T) {
	SetSuspiciousMatcher(func(text string) bool { return false })
	t.Cleanup(func() { SetSuspiciousMatcher(nil) })

	var got string
	runner := func(_ context.Context, threadID string) (io.ReadCloser, error) {
		got = threadID
		return io.NopCloser(strings.NewReader(strings.Join([]string{
			deltaReady(threadID),
			deltaItem("user_message", 1000, `{"text":"问题"}`),
			deltaItem("agent_message", 1000, `{"text":"答"}`),
			deltaLive,
		}, "\n") + "\n")), nil
	}
	req := deltaRequest()
	req.ThreadID = "thread-xyz"
	if _, ok, err := CollectDelta(context.Background(), runner, deltaCache(t), req); !ok || err != nil {
		t.Fatalf("collect = ok=%v err=%v", ok, err)
	}
	if got != "thread-xyz" {
		t.Fatalf("runner received %q", got)
	}
}
