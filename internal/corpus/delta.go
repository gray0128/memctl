package corpus

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// This file implements the Delta collector of docs/15-delta-corpus-adaptation.md
// section 4. Delta has no session log file: threads live in DeltaDB, which the
// host documents as CLI-only. The supported read path is therefore the
// structured JSONL stream of `delta thread watch <id> --json`, where the final
// assistant text and the model's reasoning arrive as separate item kinds. That
// separation is why no thinking-stripping rule exists here, and the stream is
// not paged, so no paginator exists either.
//
// Collection is deliberately limited to the thread the caller is running in.
// The Delta CLI exposes no thread-to-project mapping, so sweeping other threads
// could only be done by ignoring project membership, which docs/12 forbids.

const (
	// DeltaCollectBudget bounds one Delta collection. The watched stream
	// reaches its live marker in about two seconds on the measured host, so the
	// budget is roughly twice that. It is far larger than LiveBudget because
	// this path never runs inside a Hook that must return quickly.
	DeltaCollectBudget = 4 * time.Second
	// MaxDeltaStreamBytes bounds the bytes read from one watch stream. It
	// reuses the backfill file limit: the same digest budget applies, and the
	// largest measured thread already used most of it.
	MaxDeltaStreamBytes = MaxBackfillFileBytes
)

// DeltaRunner opens the watch stream of one thread. The caller owns the
// returned reader and closes it; the implementation is responsible for ending
// the child process when the reader is closed or ctx is canceled.
type DeltaRunner func(ctx context.Context, threadID string) (io.ReadCloser, error)

// DeltaRequest is one Delta collection.
type DeltaRequest struct {
	// ThreadID is the Delta thread the agent is running in, normally
	// DELTA_CURRENT_THREAD_ID.
	ThreadID string
	// Cwd is the project directory, already normalised by ResolveProjectRoot.
	Cwd string
	// Device is the source-tag device segment.
	Device string
	// Now is the injectable clock.
	Now func() time.Time
}

// CollectDelta distills the calling Delta thread and writes one digest. It
// reports ok=false without an error when there is nothing to collect: no thread
// id, an unavailable CLI, an unreadable stream, a truncated read, or a stream
// that held no keepable turn. Only a cache write failure is returned, and the
// caller treats it as advisory exactly like the file-based path.
func CollectDelta(ctx context.Context, run DeltaRunner, cache *Cache, req DeltaRequest) (Digest, bool, error) {
	if cache == nil || run == nil || strings.TrimSpace(req.ThreadID) == "" {
		return Digest{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return Digest{}, false, nil
	}
	stream, err := run(ctx, req.ThreadID)
	if err != nil {
		return Digest{}, false, nil
	}
	defer stream.Close()

	// A bounded reader records whether the digest saw the whole thread, so a
	// partial read is marked instead of being treated as a complete session.
	bounded := &cappedReader{reader: stream, limit: MaxDeltaStreamBytes}
	collector := Collector{Device: req.Device, Cwd: req.Cwd, Now: req.Now}
	parsed, ok := parseDelta(collector, bounded)
	if !ok || bounded.hit || !parsed.sawEnd {
		// A cut-short stream is not saved: caching it would make the next run
		// skip the thread as already collected, exactly as the backfill rule
		// requires (docs/12 section 2.2.2). parseDelta is called directly
		// because Collect cannot report whether the end marker was reached.
		return Digest{}, false, nil
	}
	digest, ok := collector.digestFrom("delta", req.ThreadID, parsed)
	if !ok {
		return Digest{}, false, nil
	}
	if err := cache.Save(digest); err != nil {
		return digest, false, err
	}
	return digest, true, nil
}

// parseDelta reads the JSONL stream of `delta thread watch --json`. Every line
// is one envelope; the item envelopes carry the conversation. The stream's
// order is the conversation order: items of one turn share a timestamp and the
// turn id is constant within a thread, so neither can be used for ordering or
// grouping. See docs/15-delta-corpus-adaptation.md section 2.2.
func parseDelta(c Collector, r io.Reader) (parseInput, bool) {
	out := parseInput{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	var pending *Turn
	// A turn can carry several agent_message items. They are Delta's own split
	// of one reply (progress notes, then the answer), and 57% of measured turns
	// exceed MaxTurnTextBytes when concatenated, so the longest one is kept
	// instead of a concatenation whose tail truncation would drop the answer.
	var best string
	flush := func() {
		if pending == nil {
			return
		}
		if best != "" {
			pending.Assistant = best
		}
		if turn, ok := c.finalizeTurn(suspiciousMatcher, *pending); ok {
			out.turns = append(out.turns, turn)
		}
		pending, best = nil, ""
	}
	for scanner.Scan() {
		value, ok := decodeLine(scanner.Text())
		if !ok {
			continue
		}
		switch stringField(value, "type") {
		case "ready":
			if out.sessionID == "" {
				out.sessionID = stringField(value, "thread_id")
			}
		case "live":
			// The watch stays connected after this marker, so the reader stops
			// here and lets Close end the child process. Reaching it proves the
			// whole thread was delivered; without it the read was cut short.
			out.sawEnd = true
			flush()
			return out, len(out.turns) > 0
		case "item":
			item, ok := objectField(value, "item")
			if !ok {
				continue
			}
			out.newest = newestTime(out.newest, deltaItemTime(item))
			switch stringField(item, "kind") {
			case "user_message":
				text := deltaItemText(item)
				if text == "" || looksLikeInjectedContext(text) {
					continue
				}
				flush()
				c.addUser(&pending, &out, text)
			case "agent_message":
				if pending == nil {
					// Text before the first user message belongs to the host's
					// own preface, not to a turn.
					continue
				}
				if text := deltaItemText(item); len(text) > len(best) {
					best = text
				}
			case "tool_call":
				if pending == nil {
					continue
				}
				if signature := deltaToolSignature(item); signature != "" {
					pending.Tools = append(pending.Tools, signature)
				}
			}
			// agent_thought, external_edits and compaction are never kept:
			// the first is model reasoning, the second is tool output, and the
			// third is the host's own summary of earlier context.
		}
	}
	flush()
	// A line above maxLineBytes truncates the scan; the digest then reports an
	// incomplete session instead of silently losing one item.
	if err := scanner.Err(); err != nil && strings.Contains(err.Error(), "token too long") {
		out.truncated = true
	}
	return out, len(out.turns) > 0
}

// deltaItemText returns the text payload of one item envelope.
func deltaItemText(item map[string]json.RawMessage) string {
	content, ok := objectField(item, "content")
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringField(content, "text"))
}

// deltaItemTime reads an item's millisecond timestamp.
func deltaItemTime(item map[string]json.RawMessage) time.Time {
	raw, ok := item["created_at"]
	if !ok {
		return time.Time{}
	}
	var millis int64
	if err := json.Unmarshal(raw, &millis); err != nil || millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis)
}

// deltaToolSignature renders one tool_call as a locator, never as output. The
// rawInput keys are the ones the measured Delta tools use; title is the fallback
// because every tool carries one.
func deltaToolSignature(item map[string]json.RawMessage) string {
	content, ok := objectField(item, "content")
	if !ok {
		return ""
	}
	name := stringField(content, "name")
	target := ""
	if input, ok := objectField(content, "rawInput"); ok {
		target = firstCommand(input)
	}
	if target == "" {
		target = singleLine(stringField(content, "title"))
	}
	status := stringField(content, "status")
	switch {
	case name == "" && target == "":
		return ""
	case target == "":
		return name + " (" + status + ")"
	case status == "":
		return name + ": " + target
	default:
		return name + ": " + target + " (" + status + ")"
	}
}
