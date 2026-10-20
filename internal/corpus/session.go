package corpus

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This file locates one already-known session log by its agent and session id.
// `dream --prepare` refreshes the calling session before sweeping the stores:
// the digest of a session that is still being written is what the current agent
// most needs, and the sweep deliberately skips sessions already in the pool.
// See docs/12-session-corpus-dreaming.md section 2.3 step 1.

// LocateSession returns the log file of one session, or an empty string when
// the host store does not hold it. The lookup is bounded: an unknown host, a
// missing directory and an unreadable file all report "not found" instead of
// guessing. cwd is the project the session belongs to; home overrides the
// session-store root and an empty value resolves the current user's home.
func LocateSession(agent, sid, cwd, home string) string {
	if sid == "" {
		return ""
	}
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = resolved
	}
	switch agent {
	case "claude-code":
		return regularFile(filepath.Join(home, filepath.FromSlash(".claude/projects"), claudeDir(cwd), sid+".jsonl"))
	case "pi":
		return newestUnder(filepath.Join(home, filepath.FromSlash(".pi/agent/sessions"), piDir(cwd)), func(name string) bool {
			return strings.HasSuffix(name, ".jsonl") && piSessionID(name) == sid
		})
	case "dsh":
		// The host names a session directory `session-<sid>`; older stores and
		// the test fixtures use the bare id. Both are the same session record,
		// so the lookup accepts either name.
		root := filepath.Join(home, ".dsh", "sessions", escapeDSH(cwd))
		if path := highestDSHVersion(filepath.Join(root, "session-"+sid)); path != "" {
			return path
		}
		return highestDSHVersion(filepath.Join(root, sid))
	case "antigravity":
		return regularFile(filepath.Join(home, filepath.FromSlash(".gemini/antigravity/brain"), sid,
			".system_generated", "logs", "transcript.jsonl"))
	case "codex":
		return locateCodex(home, sid)
	default:
		return ""
	}
}

// regularFile reports a regular, non-symlink file at path.
func regularFile(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return path
}

// newestUnder returns the most recently modified regular file of dir that
// matches accept, or an empty string when none does.
func newestUnder(dir string, accept func(name string) bool) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !accept(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = filepath.Join(dir, entry.Name()), info.ModTime()
		}
	}
	return best
}

// highestDSHVersion returns the highest-versioned session record of one DSH
// session directory.
func highestDSHVersion(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, version := "", -1
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		next := dshVersion(entry.Name())
		if next < 0 || next <= version {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			best, version = filepath.Join(dir, entry.Name()), next
		}
	}
	return best
}

// locateCodex walks the date-partitioned Codex store and compares the session
// id recorded in each rollout's metadata line. It is bounded by the corpus TTL,
// so a large archive is not scanned on every prepare.
func locateCodex(home, sid string) string {
	cutoff := time.Now().Add(-CorpusStateTTL)
	var found string
	walkSessionFiles(filepath.Join(home, filepath.FromSlash(".codex/sessions")), func(path string, info fs.FileInfo) {
		if found != "" || info.ModTime().Before(cutoff) {
			return
		}
		if _, got := sniffCodexFile(path); got == sid {
			found = path
		}
	})
	return found
}
