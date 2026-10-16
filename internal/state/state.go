// Package state persists the records the local write transaction needs to
// recover after an interruption. Records live in the repository's real Git
// directory, not in the configurable state root, so switching state locations
// cannot hide an unfinished transaction. See
// docs/03-workflows-and-dataflows.md section 2.1.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// Transaction kinds.
const (
	KindMutation = "mutation"
)

// PathRecord is the pre-image of one path in a transaction. Before is the
// exact original file content; Existed is false for a path created by the
// transaction. PlannedRevision is the file revision the transaction intended
// to write.
type PathRecord struct {
	Path            string `json:"path"`
	Existed         bool   `json:"existed"`
	Mode            string `json:"mode,omitempty"`
	Before          string `json:"before,omitempty"`
	PlannedRevision string `json:"plannedRevision"`
}

// Transaction is one unfinished-or-finished local repository transaction.
type Transaction struct {
	ID           string       `json:"id"`
	Kind         string       `json:"kind"`
	OriginalHead string       `json:"originalHead"`
	StartedAt    string       `json:"startedAt"`
	Message      string       `json:"message,omitempty"`
	Paths        []PathRecord `json:"paths"`
	Completed    bool         `json:"completed"`
	Commit       string       `json:"commit,omitempty"`
}

// Paths_ returns the transaction's affected paths in order.
func (t Transaction) AffectedPaths() []string {
	paths := make([]string, 0, len(t.Paths))
	for _, record := range t.Paths {
		paths = append(paths, record.Path)
	}
	return paths
}

// Store reads and writes transaction records under <gitDir>/memctl.
type Store struct {
	dir string
}

// OpenStore binds a record store to a real Git directory.
func OpenStore(gitDir string) *Store {
	return &Store{dir: filepath.Join(gitDir, "memctl", "transactions")}
}

// NewTransactionID returns a random, non-reusable transaction identifier.
func NewTransactionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot generate a transaction id")
	}
	return "tx-" + hex.EncodeToString(raw[:]), nil
}

// StartedAt returns the RFC 3339 timestamp recorded for a new transaction.
func StartedAt() string { return time.Now().UTC().Format(time.RFC3339) }

// Save persists the record and flushes it to disk before the caller changes
// any file. A partially written record is never visible.
func (s *Store) Save(tx Transaction) error {
	if tx.ID == "" {
		return memerr.New(memerr.InvalidArgument, "transaction record requires an id")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create transaction store %s", s.dir)
	}
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot encode transaction %s", tx.ID)
	}
	return writeFileAtomic(s.dir, tx.ID+".json", data)
}

// Load returns one record by id.
func (s *Store) Load(id string) (Transaction, error) {
	data, err := os.ReadFile(s.fileName(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Transaction{}, memerr.New(memerr.NotFound, "transaction %s does not exist", id)
		}
		return Transaction{}, memerr.Wrap(memerr.IOFailed, err, "cannot read transaction %s", id)
	}
	return decodeTransaction(id, data)
}

// List returns every recorded transaction, ordered by id.
func (s *Store) List() ([]Transaction, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot list transaction store %s", s.dir)
	}
	var records []Transaction
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, memerr.Wrap(memerr.IOFailed, err, "cannot read transaction %s", id)
		}
		record, err := decodeTransaction(id, data)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return records, nil
}

// Pending returns records that were never marked complete.
func (s *Store) Pending() ([]Transaction, error) {
	records, err := s.List()
	if err != nil {
		return nil, err
	}
	pending := records[:0]
	for _, record := range records {
		if !record.Completed {
			pending = append(pending, record)
		}
	}
	return pending, nil
}

// Delete removes a record once its transaction is resolved.
func (s *Store) Delete(id string) error {
	if err := os.Remove(s.fileName(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return memerr.Wrap(memerr.IOFailed, err, "cannot delete transaction %s", id)
	}
	return nil
}

func (s *Store) fileName(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func decodeTransaction(id string, data []byte) (Transaction, error) {
	var record Transaction
	if err := json.Unmarshal(data, &record); err != nil {
		return Transaction{}, memerr.Wrap(memerr.IOFailed, err, "transaction record %s is unreadable", id)
	}
	if record.ID == "" {
		record.ID = id
	}
	return record, nil
}

// writeFileAtomic writes data to a temporary file in dir, flushes it, and
// renames it into place so readers only see a complete file.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create a temporary file in %s", dir)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot write %s", tmpName)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return memerr.Wrap(memerr.IOFailed, err, "cannot flush %s", tmpName)
	}
	if err := tmp.Close(); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot close %s", tmpName)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot publish transaction record %s", name)
	}
	// Best effort directory flush; the rename already replaced the record
	// atomically, so a failure here does not corrupt it.
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}
