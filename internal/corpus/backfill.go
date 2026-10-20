package corpus

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file implements the backfill scan of docs/12-session-corpus-dreaming.md
// section 2.2.2: `dream --prepare` sweeps the local session stores of the known
// hosts and adds the recent sessions of the current project to the digest pool.
//
// The documented order is "narrow by project first, then sort by time": hosts
// whose store directory encodes the working directory are filtered by name, and
// a host whose store is not organised by project is sniffed from its own
// metadata and closed as soon as the project differs. Only the matching logs
// become candidates, they are ordered by modification time, and at most
// MaxBackfillFilesPerRun of them are distilled within BackfillBudget. Every
// failure is silent, exactly like the real-time path.

const (
	// MaxBackfillFilesPerRun bounds how many candidate logs one sweep distils.
	MaxBackfillFilesPerRun = 50
	// BackfillBudget is the wall-clock budget of one sweep. Running out of
	// budget keeps what was already distilled and is not a failure.
	BackfillBudget = 5 * time.Second
)

// errBudget stops a log read once the sweep has spent its wall-clock budget. It
// is not reported to the caller: the session is simply not distilled this run.
var errBudget = errors.New("backfill budget exhausted")

// BackfillRequest is one sweep of the local session stores.
type BackfillRequest struct {
	// Cwd is the project whose sessions are collected. Only sessions whose
	// working directory has the same cwdKey enter the pool.
	Cwd string
	// Device is the source-tag device segment.
	Device string
	// Home is the session-store root. An empty value resolves the current
	// user's home directory.
	Home string
	// WroteMemory reports whether a session already produced a commit with
	// its trailer. It is optional; a nil callback records false.
	WroteMemory func(sid string) bool
	// Now is the injectable clock.
	Now func() time.Time
	// Budget overrides BackfillBudget. Zero keeps the documented constant;
	// tests use it to exercise the deadline without waiting five seconds.
	Budget time.Duration
}

// BackfillResult reports what one sweep did, for diagnostics and tests.
type BackfillResult struct {
	// Collected is the number of newly distilled sessions.
	Collected int
	// Files is the number of candidate logs that were distilled.
	Files int
	// SkippedProject is the number of sniffed logs whose recorded working
	// directory belongs to another project.
	SkippedProject int
	// TimedOut reports that the wall-clock budget stopped the sweep early.
	TimedOut bool
}

// Backfill distils the recent sessions of req.Cwd that are not in the pool yet.
// It never returns an error: a missing store, an unreadable log and an
// exhausted budget all keep the already collected digests. A session already in
// the pool is skipped, so a consumed digest is never overwritten.
func Backfill(cache *Cache, req BackfillRequest) BackfillResult {
	result := BackfillResult{}
	if cache == nil || req.Cwd == "" || !Enabled(os.Getenv) {
		return result
	}
	now := req.Now
	if now == nil {
		now = time.Now
	}
	budget := req.Budget
	if budget <= 0 {
		budget = BackfillBudget
	}
	deadline := now().Add(budget)
	expired := func() bool { return !now().Before(deadline) }

	cwdKey := CwdKey(req.Cwd)
	sweep := &sweep{result: &result, expired: expired, now: now}
	candidates := discoverCandidates(req, cwdKey, sweep)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].mtime.Equal(candidates[j].mtime) {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].mtime.After(candidates[j].mtime)
	})

	collector := Collector{Device: req.Device, Cwd: req.Cwd, Now: now}
	for _, candidate := range candidates {
		if result.Files >= MaxBackfillFilesPerRun || expired() {
			result.TimedOut = expired()
			break
		}
		// The list is ordered by modification time, so the first entry past
		// the TTL ends the sweep instead of scanning a whole archive.
		if now().Sub(candidate.mtime) > CorpusStateTTL {
			break
		}
		// A candidate whose session id is known needs no read when its digest
		// is already cached.
		if candidate.sid != "" && cached(cache, candidate.agent, candidate.sid) {
			continue
		}
		result.Files++
		digest, ok := distillFile(collector, candidate.agent, candidate.sid, candidate.path, deadline, now)
		if !ok {
			if expired() {
				result.TimedOut = true
				break
			}
			continue
		}
		// A store name is a lossy escape, so the log's own working directory
		// is what actually decides the project.
		if digest.CwdKey != cwdKey {
			result.SkippedProject++
			continue
		}
		if req.WroteMemory != nil && req.WroteMemory(digest.SessionID) {
			digest.WroteMemoryInSess = true
		}
		if err := cache.Save(digest); err != nil {
			continue
		}
		result.Collected++
	}
	return result
}

// cached reports whether a session is already in the pool.
func cached(cache *Cache, agent, sid string) bool {
	_, ok := cache.Load(agent, sid)
	return ok
}

// distillFile reads one session log under the sweep budget. A false result
// means the log was unreadable, held nothing keepable or was cut short by the
// budget; a cut-short log is not saved, because caching an incomplete session
// would make the next sweep skip it as already present.
func distillFile(c Collector, agent, sid, path string, deadline time.Time, now func() time.Time) (Digest, bool) {
	stream, err := openSessionStream(path)
	if err != nil {
		return Digest{}, false
	}
	defer stream.Close()
	reader := &budgetReader{reader: stream.reader, deadline: deadline, now: now}
	digest, ok := c.Collect(agent, sid, reader)
	if !ok || reader.expired {
		return Digest{}, false
	}
	digest.Truncated = digest.Truncated || stream.Truncated()
	return digest, true
}

// candidate is one session log of the current project.
type candidate struct {
	agent string
	path  string
	// sid is known from the store layout when the directory or file name
	// carries it; a sniffed log leaves it empty and the log fills it in.
	sid   string
	mtime time.Time
}

// discoverCandidates lists the session logs of the current project across the
// supported hosts. It applies the project pre-filter of section 2.2.2 and
// honours the deadline, so a large store cannot spend the whole budget before
// the first log is distilled.
func discoverCandidates(req BackfillRequest, cwdKey string, sweep *sweep) []candidate {
	home := req.Home
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		home = resolved
	}
	var out []candidate

	// Hosts whose store directory is the escaped working directory: only the
	// directories of the current project are visited.
	out = append(out, discoverFlat(home, filepath.FromSlash(".claude/projects"), claudeDir(req.Cwd), "claude-code", claudeSessionID, sweep)...)
	out = append(out, discoverFlat(home, filepath.FromSlash(".pi/agent/sessions"), piDir(req.Cwd), "pi", piSessionID, sweep)...)
	out = append(out, discoverDSH(home, req.Cwd, sweep)...)
	// Hosts whose store is not organised by project: the log decides, and a
	// log of another project is closed after its metadata records.
	out = append(out, sniffCodexStore(home, cwdKey, sweep)...)
	out = append(out, sniffAntigravityStore(home, cwdKey, sweep)...)
	return out
}

// sweep carries the per-run deadline into discovery. Pre-filtering a log whose
// project is only in its own records costs one bounded metadata read, so it is
// bounded by the wall-clock budget rather than by MaxBackfillFilesPerRun: the
// documented file cap counts the logs that are parsed into digests, and a store
// partitioned by date would otherwise spend the whole cap deciding projects.
type sweep struct {
	result  *BackfillResult
	expired func() bool
	now     func() time.Time
}

// cutoff is the oldest modification time a log may have to enter the pool.
func (s *sweep) cutoff() time.Time { return s.now().Add(-CorpusStateTTL) }

// discoverFlat lists the session files of one host that keys its store by the
// escaped working directory. A nested directory is not a session record, and a
// file with another suffix is not a log.
func discoverFlat(home, store, dir, agent string, sessionID func(string) string, sweep *sweep) []candidate {
	if sweep.expired() {
		return nil
	}
	root := filepath.Join(home, store, dir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, candidate{
			agent: agent,
			path:  filepath.Join(root, entry.Name()),
			sid:   sessionID(entry.Name()),
			mtime: info.ModTime(),
		})
	}
	return out
}

// discoverDSH lists the DSH session records of the current project. Each session
// has its own directory with one or more versioned zstd records and the highest
// version is the one the host writes. The directory name is the session id.
func discoverDSH(home, cwd string, sweep *sweep) []candidate {
	if sweep.expired() {
		return nil
	}
	root := filepath.Join(home, ".dsh", "sessions", escapeDSH(cwd))
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		name, version := "", -1
		var modified time.Time
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			next := dshVersion(file.Name())
			if next < 0 || next <= version {
				continue
			}
			info, err := file.Info()
			if err != nil {
				continue
			}
			name, version, modified = file.Name(), next, info.ModTime()
		}
		if name == "" {
			continue
		}
		out = append(out, candidate{
			agent: "dsh",
			path:  filepath.Join(dir, name),
			sid:   entry.Name(),
			mtime: modified,
		})
	}
	return out
}

// dshVersion returns the N of session.vN.jsonl.zstd; an unsuffixed record is
// version 0 and any other name is not a record.
func dshVersion(name string) int {
	const prefix, suffix = "session.v", ".jsonl.zstd"
	if name == "session.jsonl.zstd" {
		return 0
	}
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return -1
	}
	value := 0
	for _, digit := range strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix) {
		if digit < '0' || digit > '9' {
			return -1
		}
		value = value*10 + int(digit-'0')
	}
	return value
}

// sniffCodexStore lists the recent Codex rollouts of the current project. The
// store is partitioned by date, so each rollout is opened only up to its
// session_meta line; a rollout of another project is counted as skipped and
// closed before its dialogue is read.
func sniffCodexStore(home, cwdKey string, sweep *sweep) []candidate {
	if sweep.expired() {
		return nil
	}
	var out []candidate
	walkSessionFiles(filepath.Join(home, filepath.FromSlash(".codex/sessions")), func(path string, info fs.FileInfo) {
		if sweep.expired() || info.ModTime().Before(sweep.cutoff()) {
			return
		}
		cwd, sid := sniffCodexFile(path)
		if cwd == "" || CwdKey(cwd) != cwdKey {
			sweep.result.SkippedProject++
			return
		}
		out = append(out, candidate{agent: "codex", path: path, sid: sid, mtime: info.ModTime()})
	})
	return out
}

// sniffAntigravityStore lists the recent Antigravity transcripts of the current
// project. The brain directory is the session id and the working directory only
// appears in the conversation's tool arguments, so the log decides.
func sniffAntigravityStore(home, cwdKey string, sweep *sweep) []candidate {
	if sweep.expired() {
		return nil
	}
	root := filepath.Join(home, filepath.FromSlash(".gemini/antigravity/brain"))
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), ".system_generated", "logs", "transcript.jsonl")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(sweep.cutoff()) {
			continue
		}
		if sweep.expired() {
			return out
		}
		cwd, _ := sniffAntigravityFile(path)
		if cwd == "" || CwdKey(cwd) != cwdKey {
			sweep.result.SkippedProject++
			continue
		}
		out = append(out, candidate{agent: "antigravity", path: path, sid: entry.Name(), mtime: info.ModTime()})
	}
	return out
}

// sniffCodexFile reads a rollout's working directory and session id from its
// session_meta line.
func sniffCodexFile(path string) (string, string) {
	stream, err := openSessionStream(path)
	if err != nil {
		return "", ""
	}
	defer stream.Close()
	return sniffCodex(stream.reader)
}

// sniffAntigravityFile reads a transcript's working directory from the first
// tool call that records one.
func sniffAntigravityFile(path string) (string, string) {
	stream, err := openSessionStream(path)
	if err != nil {
		return "", ""
	}
	defer stream.Close()
	return sniffAntigravity(stream.reader)
}

// walkSessionFiles visits every regular .jsonl file under root without
// following a symbolic link. An unreadable directory is skipped instead of
// aborting the walk.
func walkSessionFiles(root string, visit func(path string, info fs.FileInfo)) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		visit(path, info)
		return nil
	})
}

// budgetReader stops a log read when the sweep deadline passes and records that
// it did, so a half-read session is not reported as complete.
type budgetReader struct {
	reader   io.Reader
	deadline time.Time
	now      func() time.Time
	expired  bool
}

// Read implements io.Reader.
func (b *budgetReader) Read(p []byte) (int, error) {
	if !b.now().Before(b.deadline) {
		b.expired = true
		return 0, errBudget
	}
	return b.reader.Read(p)
}

// claudeDir returns the store directory name Claude Code uses for one working
// directory. Every separator, space and underscore becomes a dash; the escaping
// is one-way, so the match is made forward and the log's own `cwd` field remains
// authoritative.
func claudeDir(cwd string) string {
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

// claudeSessionID reads the session id from a Claude Code transcript name:
// the file is named `<sid>.jsonl`.
func claudeSessionID(name string) string {
	return strings.TrimSuffix(name, ".jsonl")
}

// piDir returns the pi session directory name for one working directory: pi
// only replaces separators and wraps the result in `--`.
func piDir(cwd string) string {
	return "--" + strings.Trim(strings.ReplaceAll(cwd, "/", "-"), "-") + "--"
}

// piSessionID reads the session id from a pi transcript name, which is
// `<timestamp>_<sid>.jsonl`. The file name is a lookup shortcut only; the log's
// own `session` record stays authoritative.
func piSessionID(name string) string {
	name = strings.TrimSuffix(name, ".jsonl")
	if index := strings.LastIndex(name, "_"); index >= 0 {
		return name[index+1:]
	}
	return name
}

// escapeDSH renders a working directory the way DSH names its session
// directories: separators become dashes, every character outside the literal
// set (letters, digits, `_` and `-`) becomes an uppercase `~XXXX` escape, and
// the result is wrapped in `--`. A space is therefore `~0020`, not a literal
// blank.
func escapeDSH(cwd string) string {
	var builder strings.Builder
	builder.WriteString("--")
	for _, char := range strings.TrimPrefix(strings.ReplaceAll(cwd, "\\", "/"), "/") {
		switch {
		case char == '/':
			builder.WriteByte('-')
		case char == '_' || char == '-' || char >= '0' && char <= '9' ||
			char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		default:
			builder.WriteByte('~')
			builder.WriteString(hex4(char))
		}
	}
	builder.WriteString("--")
	return builder.String()
}

// hex4 formats one rune as four uppercase hexadecimal digits, the escape width
// DSH uses for every non-ASCII character.
func hex4(char rune) string {
	const digits = "0123456789ABCDEF"
	value := int(char)
	return string([]byte{
		digits[(value>>12)&0xF],
		digits[(value>>8)&0xF],
		digits[(value>>4)&0xF],
		digits[value&0xF],
	})
}
