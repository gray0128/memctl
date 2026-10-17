package dream

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// proposalIDPattern bounds persisted identifiers so a request can never escape
// the proposals directory.
var proposalIDPattern = regexp.MustCompile(`^dp-[0-9a-f]{32}$`)

// record is the on-disk shape of one proposal. WorkDir and InstanceID are
// memctl-internal bookkeeping and are never returned to a client.
type record struct {
	Proposal
	WorkDir    string `json:"workDir,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	// Planned holds the exact apply input of every create or update change. It
	// is internal to the apply workflow and never returned to a client.
	Planned map[string]plannedFile `json:"planned,omitempty"`
}

// Store persists proposals under $STATE/repos/<repoId>/proposals. Proposals are
// review artifacts, not rebuildable cache, so they are never pruned
// automatically. See docs/01-architecture-and-modules.md section 3.1.
type Store struct {
	dir string
}

// OpenStore binds the proposals directory of one repository.
func OpenStore(stateDir, repoID string) *Store {
	key := strings.TrimPrefix(repoID, "sha256:")
	return &Store{dir: filepath.Join(stateDir, "repos", key, "proposals")}
}

// Dir returns the proposals directory, used by diagnostics and tests.
func (s *Store) Dir() string { return s.dir }

// Save writes one proposal atomically.
func (s *Store) Save(value record) error {
	if !proposalIDPattern.MatchString(value.ProposalID) {
		return memerr.New(memerr.InvalidArgument, "invalid proposal id %q", value.ProposalID)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create the proposals directory %s", s.dir)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot encode proposal %s", value.ProposalID)
	}
	return writeFileAtomic(s.dir, value.ProposalID+".json", data)
}

// Load reads one proposal by id.
func (s *Store) Load(id string) (record, error) {
	if !proposalIDPattern.MatchString(id) {
		return record{}, memerr.New(memerr.NotFound, "proposal %q does not exist", id)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return record{}, memerr.New(memerr.NotFound, "proposal %s does not exist", id)
		}
		return record{}, memerr.Wrap(memerr.IOFailed, err, "cannot read proposal %s", id)
	}
	var value record
	if err := json.Unmarshal(data, &value); err != nil {
		return record{}, memerr.Wrap(memerr.IOFailed, err, "proposal %s is unreadable", id)
	}
	if value.ProposalID == "" {
		value.ProposalID = id
	}
	return value, nil
}

// writeFileAtomic writes data to a temporary file in dir, flushes it and
// renames it into place so a reader never sees a partial proposal.
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
		return memerr.Wrap(memerr.IOFailed, err, "cannot replace %s", filepath.Join(dir, name))
	}
	// Flush the directory entry so a proposal survives a system restart, as
	// docs/02-domain-models-and-contracts.md section 2.3 requires. A failure
	// here does not corrupt the already-renamed record.
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}
