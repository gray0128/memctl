package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// SyncSummary is the persisted observability record of the last synchronization
// attempt. It lives under $STATE/repos/<repoId>/cache and is rebuildable.
type SyncSummary struct {
	RemoteState   string `json:"remoteState"`
	ReasonCode    string `json:"reasonCode,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Head          string `json:"head,omitempty"`
	HasRemote     bool   `json:"hasRemote"`
	HasUpstream   bool   `json:"hasUpstream"`
	Ahead         int    `json:"aheadCount"`
	Behind        int    `json:"behindCount"`
	LastAttemptAt string `json:"lastAttemptAt,omitempty"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	ConflictID    string `json:"conflictId,omitempty"`
	OperationID   string `json:"operationId,omitempty"`
}

// Cache stores rebuildable runtime state for one repository.
type Cache struct {
	dir    string
	repoID string
}

// OpenCache binds the cache root of one repository. The sha256: prefix is
// stripped so the directory name stays valid on Windows.
func OpenCache(stateDir, repoID string) *Cache {
	key := strings.TrimPrefix(repoID, "sha256:")
	return &Cache{dir: filepath.Join(stateDir, "repos", key, "cache"), repoID: repoID}
}

// SaveSync persists the sync summary atomically.
func (c *Cache) SaveSync(summary SyncSummary) error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create cache directory %s", c.dir)
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot encode the sync summary")
	}
	return writeFileAtomic(c.dir, "sync.json", data)
}

// LoadSync returns the persisted sync summary, if any.
func (c *Cache) LoadSync() (SyncSummary, bool, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, "sync.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SyncSummary{}, false, nil
		}
		return SyncSummary{}, false, memerr.Wrap(memerr.IOFailed, err, "cannot read the sync summary")
	}
	var summary SyncSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return SyncSummary{}, false, memerr.Wrap(memerr.IOFailed, err, "the sync summary is unreadable")
	}
	return summary, true, nil
}
