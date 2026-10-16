package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// Operation kinds recorded in the repository's Git directory.
const (
	KindRebase = "rebase"
	KindRevert = "revert"
)

// Operation owners. Only operations created by memctl may be aborted or
// continued automatically.
const OwnerMemctl = "memctl"

// Operation is one in-progress Git operation owned by memctl.
type Operation struct {
	OperationID  string `json:"operationId"`
	Kind         string `json:"kind"`
	Owner        string `json:"owner"`
	OriginalHead string `json:"originalHead"`
	TargetHead   string `json:"targetHead,omitempty"`
	Phase        string `json:"phase,omitempty"`
	StartedAt    string `json:"startedAt"`
	ConflictID   string `json:"conflictId,omitempty"`
}

// ConflictSide is one side of a captured three-way conflict. Content is base64
// so binary blobs survive a JSON round trip.
type ConflictSide struct {
	Exists  bool   `json:"exists"`
	BlobID  string `json:"blobId,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Content string `json:"content,omitempty"`
	Binary  bool   `json:"binary,omitempty"`
}

// ConflictFile is one captured conflicted path.
type ConflictFile struct {
	Path     string       `json:"path"`
	Kind     string       `json:"kind"`
	Base     ConflictSide `json:"base"`
	Onto     ConflictSide `json:"onto"`
	Replayed ConflictSide `json:"replayed"`
}

// Conflict is the persisted snapshot of one stopped rebase stage.
type Conflict struct {
	ConflictID    string         `json:"conflictId"`
	RepoID        string         `json:"repoId"`
	BaseRevision  string         `json:"baseRevision"`
	OriginalHead  string         `json:"originalHead"`
	ExpectedHead  string         `json:"expectedHead"`
	UpstreamHead  string         `json:"upstreamHead"`
	ReplayCommit  string         `json:"replayCommit"`
	IndexRevision string         `json:"indexRevision"`
	OperationID   string         `json:"operationId,omitempty"`
	Phase         string         `json:"phase"`
	Files         []ConflictFile `json:"files"`
	DetectedAt    string         `json:"detectedAt"`
}

// Approval records the confirmed private remote targets.
type Approval struct {
	URLs        []string `json:"urls"`
	ConfirmedAt string   `json:"confirmedAt"`
}

// NewConflictID returns a random conflict identifier.
func NewConflictID() (string, error) {
	suffix, err := randomHex(16)
	if err != nil {
		return "", err
	}
	return "cf-" + suffix, nil
}

// NewOperationID returns a random operation identifier.
func NewOperationID() (string, error) {
	suffix, err := randomHex(16)
	if err != nil {
		return "", err
	}
	return "op-" + suffix, nil
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot generate an identifier")
	}
	return hex.EncodeToString(raw), nil
}

// Now returns the RFC 3339 timestamp recorded for a new record.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *Store) recordPath(name string) string {
	return filepath.Join(s.base, name)
}

func (s *Store) saveRecord(name string, value any) error {
	if err := os.MkdirAll(s.base, 0o700); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create state store %s", s.base)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot encode %s", name)
	}
	return writeFileAtomic(s.base, name, data)
}

func (s *Store) loadRecord(name string, target any) (bool, error) {
	data, err := os.ReadFile(s.recordPath(name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, memerr.Wrap(memerr.IOFailed, err, "cannot read %s", name)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false, memerr.Wrap(memerr.IOFailed, err, "record %s is unreadable", name)
	}
	return true, nil
}

func (s *Store) deleteRecord(name string) error {
	if err := os.Remove(s.recordPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return memerr.Wrap(memerr.IOFailed, err, "cannot delete %s", name)
	}
	return nil
}

// SaveOperation persists the current Git operation owner.
func (s *Store) SaveOperation(operation Operation) error {
	return s.saveRecord("operation.json", operation)
}

// LoadOperation returns the persisted operation, if any.
func (s *Store) LoadOperation() (Operation, bool, error) {
	var operation Operation
	ok, err := s.loadRecord("operation.json", &operation)
	return operation, ok, err
}

// DeleteOperation clears the operation owner.
func (s *Store) DeleteOperation() error { return s.deleteRecord("operation.json") }

// SaveConflict persists the current conflict snapshot.
func (s *Store) SaveConflict(conflict Conflict) error {
	return s.saveRecord("conflict.json", conflict)
}

// LoadConflict returns the persisted conflict, if any.
func (s *Store) LoadConflict() (Conflict, bool, error) {
	var conflict Conflict
	ok, err := s.loadRecord("conflict.json", &conflict)
	return conflict, ok, err
}

// DeleteConflict clears the conflict snapshot.
func (s *Store) DeleteConflict() error { return s.deleteRecord("conflict.json") }

// SaveApproval persists the confirmed remote target set.
func (s *Store) SaveApproval(approval Approval) error {
	return s.saveRecord("remote-approval.json", approval)
}

// LoadApproval returns the persisted approval, if any.
func (s *Store) LoadApproval() (Approval, bool, error) {
	var approval Approval
	ok, err := s.loadRecord("remote-approval.json", &approval)
	return approval, ok, err
}
