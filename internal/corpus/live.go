package corpus

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// This file implements the real-time collection path used by `memctl sync` and
// `memctl end`: the host passes the current session's transcript path, the
// collector distills it and writes one digest. The path is best effort and
// bounded: every failure is silent and the caller never reports a non-zero
// exit code because of it. See docs/12-session-corpus-dreaming.md section 2.2.

// EnvCorpus disables local collection when set to a false-like value, per the
// documented AGENT_MEMORY_CORPUS variable.
const EnvCorpus = "AGENT_MEMORY_CORPUS"

// Enabled reports whether local corpus collection is enabled. Every value
// except an explicit false-like value keeps the documented default on.
func Enabled(getenv func(string) string) bool {
	switch normalizeEnv(getenv(EnvCorpus)) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// normalizeEnv lowercases and trims a boolean environment value so the
// documented false-like spellings are matched case-insensitively.
func normalizeEnv(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// LiveBudget is the wall-clock budget of one real-time collection, per
// docs/12-session-corpus-dreaming.md section 3.2 rule 1. It is deliberately
// much smaller than the backfill budget: this path runs inside a host Hook.
const LiveBudget = 200 * time.Millisecond

// RealTimeRequest is one collection triggered by a Hook or a host plugin.
type RealTimeRequest struct {
	// Agent is the host name recorded in the digest.
	Agent string
	// SID is the fallback session id, used when the log has no metadata line.
	SID string
	// TranscriptPath is the log file to distill.
	TranscriptPath string
	// Cwd is the fallback project directory, used when the log carries none.
	Cwd string
	// OverrideCwd forces Cwd to take precedence over the log's own directory
	// when the Hook payload supplied an explicit working directory.
	OverrideCwd bool
	// Device is the source-tag device segment.
	Device string
	// SuspiciousMatcher reports whether a text fragment carries a prompt
	// injection phrase.
	SuspiciousMatcher func(string) bool
	// Now is the injectable clock.
	Now func() time.Time
}

// RealTime distills one session log and writes its digest. It reports ok=false
// and no error when there is nothing to do: collection disabled, unknown host
// format, missing log, or an unreadable stream. Only a cache write failure is
// returned, and even that is advisory for the caller.
func RealTime(ctx context.Context, cache *Cache, req RealTimeRequest) (Digest, bool, error) {
	if cache == nil || req.TranscriptPath == "" || !Supports(req.Agent) {
		return Digest{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return Digest{}, false, nil
	}
	stream, err := openSessionStream(req.TranscriptPath)
	if err != nil {
		return Digest{}, false, nil
	}
	defer stream.Close()

	collector := Collector{
		Device:            req.Device,
		Cwd:               req.Cwd,
		OverrideCwd:       req.OverrideCwd,
		SuspiciousMatcher: req.SuspiciousMatcher,
		Now:               req.Now,
	}
	digest, ok := collector.Collect(req.Agent, req.SID, stream.reader)
	if !ok {
		return Digest{}, false, nil
	}
	if stream.Truncated() {
		digest.Truncated = true
	}
	if existing, found := cache.Load(digest.Agent, digest.SessionID); found {
		digest = mergeExistingDigest(existing, digest)
	}
	if err := cache.Save(digest); err != nil {
		return digest, false, err
	}
	return digest, true, nil
}

// mergeExistingDigest preserves consumption metadata across real-time refreshes.
// When an already-consumed session is re-collected without new turns, its
// ConsumedBy, ConsumedRevision and UpdatedAt remain untouched; when new turns
// are appended after consumption, ConsumedBy and ConsumedRevision are retained
// alongside the consumed-turn watermark so only the unconsumed suffix is exposed
// to subsequent dream --prepare runs.
func mergeExistingDigest(existing, next Digest) Digest {
	next.WroteMemoryInSess = next.WroteMemoryInSess || existing.WroteMemoryInSess
	if existing.ConsumedBy == "" {
		return next
	}
	consumedTurns := existing.ConsumedTurns
	if consumedTurns <= 0 || consumedTurns > len(existing.Turns) {
		consumedTurns = len(existing.Turns)
	}
	next.ConsumedBy = existing.ConsumedBy
	next.ConsumedRevision = existing.ConsumedRevision
	next.ConsumedTurns = consumedPrefixLen(existing.Turns[:consumedTurns], next.Turns)
	if turnsEqual(existing.Turns, next.Turns) {
		next.UpdatedAt = existing.UpdatedAt
		next.Date = existing.Date
	}
	return next
}

func consumedPrefixLen(consumed, current []Turn) int {
	if len(consumed) == 0 || len(current) == 0 {
		return 0
	}
	// Find the longest suffix of consumed that matches a prefix of current so
	// sliding window truncation at MaxTurnsPerSession still aligns the watermark.
	for start := 0; start < len(consumed); start++ {
		suffix := consumed[start:]
		if len(suffix) <= len(current) && turnsEqual(suffix, current[:len(suffix)]) {
			return len(suffix)
		}
	}
	if len(consumed) <= len(current) {
		return len(consumed)
	}
	return len(current)
}

func turnsEqual(left, right []Turn) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].User != right[i].User || left[i].Assistant != right[i].Assistant {
			return false
		}
		if len(left[i].Tools) != len(right[i].Tools) {
			return false
		}
		for j := range left[i].Tools {
			if left[i].Tools[j] != right[i].Tools[j] {
				return false
			}
		}
	}
	return true
}

// ResolveTranscript turns a hook-provided transcript path into an absolute
// path. Relative paths are rejected: a hook payload must not select a file
// relative to whatever directory the host happened to use.
func ResolveTranscript(path, cwd string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	return filepath.Clean(path)
}

// Cache writes the digest cache for one repository and never fails the caller.
// It is the single entry point the CLI and the plugins' spawn path share.
type Writer struct {
	Cache             *Cache
	SuspiciousMatcher func(string) bool
	Now               func() time.Time
}

// Collect runs one real-time collection under the documented budget. It never
// returns an error: the caller is a Hook that must always exit zero.
func (w Writer) Collect(req RealTimeRequest) (Digest, bool) {
	if w.Cache == nil {
		return Digest{}, false
	}
	if req.SuspiciousMatcher == nil {
		req.SuspiciousMatcher = w.SuspiciousMatcher
	}
	if req.Now == nil {
		req.Now = w.Now
	}
	ctx, cancel := context.WithTimeout(context.Background(), LiveBudget)
	defer cancel()
	digest, ok, err := RealTime(ctx, w.Cache, req)
	if err != nil {
		return Digest{}, false
	}
	return digest, ok
}
