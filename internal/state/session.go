package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// SessionStateTTL is the documented session cache expiry. Only rebuildable
// session records are pruned; proposals, recovery records and unpushed
// commits are never touched. See docs/03-workflows-and-dataflows.md section 3.
const SessionStateTTL = 14 * 24 * time.Hour

// SessionRecord is the rebuildable cache entry of one (repoId, agent, sid)
// tuple. Injected and SyncSucceeded are separate fields so an offline round is
// never mistaken for a successful sync.
type SessionRecord struct {
	RepoID         string `json:"repoId"`
	Agent          string `json:"agent"`
	SessionID      string `json:"sessionId"`
	Injected       bool   `json:"injected"`
	SyncSucceeded  bool   `json:"syncSucceeded"`
	RemoteState    string `json:"remoteState,omitempty"`
	ReasonCode     string `json:"reasonCode,omitempty"`
	LastAttemptAt  string `json:"lastAttemptAt,omitempty"`
	LastSyncAt     string `json:"lastSyncAt,omitempty"`
	LastInjectedAt string `json:"lastInjectedAt,omitempty"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}

// SessionKey is the deterministic digest of the full tuple. Hashing the whole
// tuple avoids the collision a lossy character replacement would introduce
// between different session ids.
func SessionKey(repoID, agent, sid string) string {
	sum := sha256.Sum256([]byte(repoID + "\x00" + agent + "\x00" + sid))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) sessionsDir() string { return filepath.Join(c.dir, "sessions") }

func (c *Cache) sessionFile(repoID, agent, sid string) string {
	return filepath.Join(c.sessionsDir(), SessionKey(repoID, agent, sid)+".json")
}

// LoadSession returns the cached session record. A missing record is not an
// error; the caller starts from a zero record.
func (c *Cache) LoadSession(agent, sid string) (SessionRecord, bool, error) {
	data, err := os.ReadFile(c.sessionFile(c.repoID, agent, sid))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SessionRecord{}, false, nil
		}
		return SessionRecord{}, false, memerr.Wrap(memerr.IOFailed, err, "cannot read the session record")
	}
	var record SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		// The session cache is rebuildable: an unreadable record is treated as
		// absent so it can never block a hook invocation.
		return SessionRecord{}, false, nil
	}
	return record, true, nil
}

// SaveSession persists one session record atomically. The repoId, agent and
// sessionId are always taken from the arguments so a stale record body cannot
// be written under another tuple.
func (c *Cache) SaveSession(record SessionRecord) error {
	if c.repoID == "" || record.Agent == "" || record.SessionID == "" {
		return memerr.New(memerr.InvalidArgument, "a session record requires repoId, agent and sessionId")
	}
	record.RepoID = c.repoID
	if record.UpdatedAt == "" {
		record.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(c.sessionsDir(), 0o700); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create the session cache %s", c.sessionsDir())
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot encode the session record")
	}
	return writeFileAtomic(c.sessionsDir(), SessionKey(c.repoID, record.Agent, record.SessionID)+".json", data)
}

// PruneSessions removes session records older than SessionStateTTL. The age is
// read from UpdatedAt, falling back to the file modification time. Records
// that cannot be parsed are removed as well, because the cache is rebuildable.
func (c *Cache) PruneSessions(now time.Time) error {
	entries, err := os.ReadDir(c.sessionsDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return memerr.Wrap(memerr.IOFailed, err, "cannot list the session cache %s", c.sessionsDir())
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(c.sessionsDir(), entry.Name())
		if !c.sessionExpired(path, entry, now) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return memerr.Wrap(memerr.IOFailed, err, "cannot prune the session record %s", entry.Name())
		}
	}
	return nil
}

func (c *Cache) sessionExpired(path string, entry fs.DirEntry, now time.Time) bool {
	updated := time.Time{}
	if data, err := os.ReadFile(path); err == nil {
		var record SessionRecord
		if json.Unmarshal(data, &record) == nil && record.UpdatedAt != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, record.UpdatedAt); parseErr == nil {
				updated = parsed
			}
		}
	}
	if updated.IsZero() {
		info, err := entry.Info()
		if err != nil {
			return false
		}
		updated = info.ModTime()
	}
	return now.Sub(updated) > SessionStateTTL
}
