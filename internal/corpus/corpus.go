// Package corpus distills the local session logs of the supported agent hosts
// into bounded, redacted SessionDigest records. It is the deterministic
// "Light" stage of docs/12-session-corpus-dreaming.md section 2.2: no model is
// called, nothing leaves the machine, and every failure is silent so a Hook
// never blocks or fails because a log was missing or unreadable.
//
// Collected digests live in $STATE/repos/<repoId>/cache/corpus/ as one JSON
// file per session, keyed by sha256(agent + "\x00" + sid).
package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Documented limits from docs/12-session-corpus-dreaming.md section 2.2.3.
const (
	// SchemaVersion is the SessionDigest layout version.
	SchemaVersion = 1
	// MaxTurnsPerSession bounds the turns kept per session. Turns carrying a
	// user correction signal and the most recent turns are kept first.
	MaxTurnsPerSession = 20
	// MaxTurnTextBytes bounds one user or assistant text.
	MaxTurnTextBytes = 1024
	// MaxToolsPerTurn bounds the tool signatures recorded for one turn.
	MaxToolsPerTurn = 5
	// MaxToolTextBytes bounds one tool signature.
	MaxToolTextBytes = 160
	// MaxToolOutcomesPerTurn bounds the tool outcome summaries kept for one
	// turn. The most recent summaries are kept first.
	MaxToolOutcomesPerTurn = 4
	// MaxToolOutcomeBytes bounds one tool outcome summary.
	MaxToolOutcomeBytes = 256
	// MaxSessionDigestBytes bounds one serialized digest.
	MaxSessionDigestBytes = 16 << 10
	// CorpusStateTTL is how long a digest stays in the cache after its last
	// update. A consumed digest is kept until its proposal record is gone.
	CorpusStateTTL = 14 * 24 * time.Hour
)

// Turn is one distilled exchange. user and assistant hold redacted text only;
// tools hold names, first command lines or root-relative paths plus an exit
// status. File content and full tool output are never retained: toolOutcomes
// keeps at most MaxToolOutcomesPerTurn redacted summaries of a failed tool
// call or of the turn's last verification command, each at most
// MaxToolOutcomeBytes, so offline Dreaming can check an old memory against
// what actually ran. See docs/18-agent-memory-review-and-optimization.md
// section 4.3.2.
type Turn struct {
	User         string   `json:"user,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	ToolOutcomes []string `json:"toolOutcomes,omitempty"`
	Assistant    string   `json:"assistant,omitempty"`

	// verifyOutcome is the raw summary of the latest successful verification
	// command seen while the turn is being parsed. Only the last one matters,
	// so each new one replaces the previous; finalizeTurn folds it into
	// ToolOutcomes. It is never serialized.
	verifyOutcome string
}

// Digest is the SessionDigest of docs/12-session-corpus-dreaming.md section
// 2.2.3. It is written once per session and updated atomically.
type Digest struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Agent             string `json:"agent"`
	SessionID         string `json:"sessionId"`
	SourceTag         string `json:"sourceTag"`
	Date              string `json:"date"`
	UpdatedAt         string `json:"updatedAt"`
	CwdBase           string `json:"cwdBase"`
	CwdKey            string `json:"cwdKey"`
	Truncated         bool   `json:"truncated"`
	WroteMemoryInSess bool   `json:"wroteMemoryInSession"`
	ConsumedBy        string `json:"consumedBy"`
	ConsumedRevision  string `json:"consumedRevision"`
	ConsumedTurns     int    `json:"consumedTurns,omitempty"`
	Turns             []Turn `json:"turns"`
}

// CwdKey returns the documented project key: sha256 of the canonical absolute
// working directory. It never exposes the path itself.
func CwdKey(cwd string) string {
	if cwd == "" {
		return ""
	}
	normalized := evalSymlinksSafe(cwd)
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// evalSymlinksSafe resolves symbolic links on path when it or an ancestor
// directory exists on disk, and falls back to filepath.Clean for synthetic test
// paths whose components do not exist.
func evalSymlinksSafe(path string) string {
	if path == "" {
		return ""
	}
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved)
	}
	var tail []string
	curr := clean
	for {
		parent := filepath.Dir(curr)
		if parent == curr {
			return clean
		}
		tail = append(tail, filepath.Base(curr))
		curr = parent
		if resolved, err := filepath.EvalSymlinks(curr); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return filepath.Clean(resolved)
		}
	}
}

// IsHostConfigDir reports whether path is an agent host configuration or brain
// directory (such as ~/.gemini/config) that must never be recorded as a project
// working directory.
func IsHostConfigDir(path string) bool {
	if path == "" {
		return false
	}
	slash := strings.TrimRight(filepath.ToSlash(filepath.Clean(path)), "/")
	for _, suffix := range []string{
		"/.gemini/config",
		"/.gemini/antigravity",
		"/.gemini/antigravity-cli",
	} {
		if strings.HasSuffix(slash, suffix) || strings.Contains(slash, suffix+"/") {
			return true
		}
	}
	return false
}

// CwdBase returns the display-only basename of a working directory.
func CwdBase(cwd string) string {
	clean := strings.TrimRight(filepath.ToSlash(cwd), "/")
	if clean == "" {
		return ""
	}
	if index := strings.LastIndex(clean, "/"); index >= 0 {
		clean = clean[index+1:]
	}
	return clean
}

// Delta worktree roots. A Delta thread runs in a per-thread checkout under
// <project>/.delta/worktrees/<id>/<repo>, whose Git directory lives under
// <project>/.delta/clones/<id>/<repo>.git. Both carry the same project ahead of
// the marker, so cutting there recovers the directory the user actually opened.
const (
	deltaWorktreeMarker = "/.delta/worktrees/"
	deltaCloneMarker    = "/.delta/clones/"
)

// ResolveProjectRoot normalises a working directory to the project root used as
// the digest project key. It exists because Delta gives its threads per-thread
// checkouts: <project>/.delta/worktrees/<id>/<repo> would otherwise produce a new
// sha256 key per thread and split one project across many. Every other host
// already records the project root in its own log, so for them this is the
// identity transform. See docs/15-delta-corpus-adaptation.md section 3.
//
// The lookup uses only standard Git metadata through the caller's Git runner, so
// it reads nothing private to Delta. When Git is unavailable, cwd is not inside a
// repository, or neither marker matches, cwd is returned unchanged: a project key
// must never silently become empty or point at a different project.
func ResolveProjectRoot(ctx context.Context, run func(context.Context, string, ...string) (string, error), cwd string) string {
	if cwd == "" {
		return ""
	}
	normalizedCwd := evalSymlinksSafe(cwd)
	if run == nil {
		return normalizedCwd
	}
	// --path-format=absolute is required: inside a subdirectory of an ordinary
	// repository --git-common-dir prints a relative "../.git", which matches
	// neither marker and would leave the checkout un-normalised.
	out, err := run(ctx, cwd, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel")
	if err != nil {
		return normalizedCwd
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		// An unexpected shape means the Git contract changed; the caller's own
		// directory is the safe answer, not a partial marker match.
		return normalizedCwd
	}
	common, top := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	for _, candidate := range []string{common, top} {
		if root, ok := cutAtMarker(candidate, deltaCloneMarker); ok {
			return evalSymlinksSafe(root)
		}
		if root, ok := cutAtMarker(candidate, deltaWorktreeMarker); ok {
			return evalSymlinksSafe(root)
		}
	}
	if strings.Contains(common, deltaCloneMarker) || strings.Contains(common, deltaWorktreeMarker) ||
		strings.Contains(top, deltaCloneMarker) || strings.Contains(top, deltaWorktreeMarker) {
		return normalizedCwd
	}
	// When --git-common-dir points at the main repository's .git directory (both
	// in an ordinary checkout and in a linked `git worktree`), its parent is the
	// canonical project root shared by the main working tree and all worktrees.
	if filepath.IsAbs(common) && filepath.Base(common) == ".git" {
		return evalSymlinksSafe(filepath.Dir(common))
	}
	if filepath.IsAbs(top) && top != "" {
		return evalSymlinksSafe(top)
	}
	return normalizedCwd
}

// cutAtMarker returns the part of path before marker when that part is an
// existing directory. The existence check keeps a repository whose own name
// merely contains the marker literal from being truncated into a wrong root.
func cutAtMarker(path, marker string) (string, bool) {
	index := strings.Index(path, marker)
	if index <= 0 {
		return "", false
	}
	root := path[:index]
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return root, true
}

// Cache is the per-repository digest cache under
// $STATE/repos/<repoId>/cache/corpus. Every method is safe to call on a
// missing directory: a cache that was never written is simply empty.
type Cache struct {
	dir string
}

// OpenCache binds the corpus cache of one repository. repoID carries the
// "sha256:" prefix, which is stripped to keep the directory name a valid
// directory on every platform, matching the sync cache.
func OpenCache(stateDir, repoID string) *Cache {
	key := strings.TrimPrefix(repoID, "sha256:")
	return &Cache{dir: filepath.Join(stateDir, "repos", key, "cache", "corpus")}
}

// Dir returns the cache directory, used by diagnostics and tests.
func (c *Cache) Dir() string { return c.dir }

// Name returns the file name of one digest.
func Name(agent, sessionID string) string {
	sum := sha256.Sum256([]byte(agent + "\x00" + sessionID))
	return hex.EncodeToString(sum[:]) + ".json"
}

// Path returns the absolute file path of one digest. Tests and the corpus
// diagnostics use it; callers never receive it over a protocol.
func (c *Cache) Path(agent, sessionID string) string {
	return filepath.Join(c.dir, Name(agent, sessionID))
}

// Save writes one digest atomically. An empty agent or session id is refused
// because it would collapse unrelated sessions onto one file.
func (c *Cache) Save(d Digest) error {
	if d.Agent == "" || d.SessionID == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	data, err := encodeDigest(d)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, "."+Name(d.Agent, d.SessionID)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, c.Path(d.Agent, d.SessionID))
}

// Load reads one digest. A missing or unreadable file reports ok=false rather
// than an error: the collector treats both as "not in the pool yet".
func (c *Cache) Load(agent, sessionID string) (Digest, bool) {
	data, err := os.ReadFile(c.Path(agent, sessionID))
	if err != nil {
		return Digest{}, false
	}
	value, err := decodeDigest(data)
	if err != nil {
		return Digest{}, false
	}
	return value, true
}

// List returns every readable digest in the cache, ordered by UpdatedAt
// descending. Unreadable files are skipped.
func (c *Cache) List() []Digest {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil
	}
	values := make([]Digest, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, entry.Name()))
		if err != nil {
			continue
		}
		value, err := decodeDigest(data)
		if err != nil {
			continue
		}
		values = append(values, value)
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].UpdatedAt != values[j].UpdatedAt {
			return values[i].UpdatedAt > values[j].UpdatedAt
		}
		return values[i].SessionID < values[j].SessionID
	})
	return values
}

// Prune removes digests that are older than CorpusStateTTL. A consumed digest
// is kept while its proposal still exists, so "already distilled" survives the
// TTL; it is removed once the proposal record is gone. proposalExists may be
// nil, in which case a consumed digest is treated as having no proposal.
func (c *Cache) Prune(now time.Time, proposalExists func(id string) bool) []string {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(c.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := decodeDigest(data)
		if err != nil {
			// An unreadable digest is not consumed and cannot be refreshed, so
			// it is exactly what the TTL is meant to bound.
			if info, statErr := entry.Info(); statErr == nil && now.Sub(info.ModTime()) > CorpusStateTTL {
				if os.Remove(path) == nil {
					removed = append(removed, entry.Name())
				}
			}
			continue
		}
		updated, err := time.Parse(time.RFC3339, value.UpdatedAt)
		if err != nil || now.Sub(updated) <= CorpusStateTTL {
			continue
		}
		if value.ConsumedBy != "" && proposalExists != nil && proposalExists(value.ConsumedBy) {
			continue
		}
		if os.Remove(path) == nil {
			removed = append(removed, entry.Name())
		}
	}
	return removed
}
