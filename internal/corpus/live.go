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
	// Device is the source-tag device segment.
	Device string
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

	collector := Collector{Device: req.Device, Cwd: req.Cwd}
	digest, ok := collector.Collect(req.Agent, req.SID, stream.reader)
	if !ok {
		return Digest{}, false, nil
	}
	if stream.Truncated() {
		digest.Truncated = true
	}
	if err := cache.Save(digest); err != nil {
		return digest, false, err
	}
	return digest, true, nil
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
	Cache *Cache
	Now   func() time.Time
}

// Collect runs one real-time collection under the documented budget. It never
// returns an error: the caller is a Hook that must always exit zero.
func (w Writer) Collect(req RealTimeRequest) (Digest, bool) {
	if w.Cache == nil {
		return Digest{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), LiveBudget)
	defer cancel()
	digest, ok, err := RealTime(ctx, w.Cache, req)
	if err != nil {
		return Digest{}, false
	}
	return digest, ok
}
