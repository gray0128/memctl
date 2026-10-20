package dream

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gray0128/memctl/internal/memerr"
)

// This file persists the last local prepare bundle. `propose` receives integer
// session indices, and an index is only meaningful against the exact list the
// agent saw; re-deriving the list at propose time would shift every index when
// the selection order differs (another working directory, one more collected
// session). The snapshot holds session metadata and digest revisions only, no
// memory content, and lives under the rebuildable cache root next to the digests
// it points at.

// prepareSnapshot is the on-disk form of the last prepare bundle. A newer
// prepare supersedes it: the documented flow is one prepare followed by one
// propose in the same agent turn.
type prepareSnapshot struct {
	BaseHead     string          `json:"baseHead"`
	BaseRevision string          `json:"baseRevision"`
	Sessions     []BundleSession `json:"sessions"`
	Digests      []CorpusDigest  `json:"digests"`
	CreatedAt    string          `json:"createdAt"`
}

// preparePath is the snapshot file of one repository.
func (s *Service) preparePath() string {
	return filepath.Join(s.Source.StateDir, "repos", trimRepoPrefix(s.Source.RepoID), "cache", "dream", "prepare.json")
}

// savePrepare records the snapshot a following propose validates against. It is
// best effort: a write failure leaves the previous snapshot in place, so
// prepare never fails because its own cache is unavailable.
func (s *Service) savePrepare(value prepareSnapshot) {
	if value.Sessions == nil {
		value.Sessions = []BundleSession{}
	}
	if value.Digests == nil {
		value.Digests = []CorpusDigest{}
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return
	}
	path := s.preparePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".prepare.json.tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(name, path)
}

// loadPrepare reads the last snapshot.
func (s *Service) loadPrepare() (prepareSnapshot, error) {
	data, err := os.ReadFile(s.preparePath())
	if errors.Is(err, fs.ErrNotExist) {
		return prepareSnapshot{}, memerr.New(memerr.InvalidArgument,
			"no prepare bundle is on record; run `memctl dream --prepare` before proposing")
	}
	if err != nil {
		return prepareSnapshot{}, memerr.Wrap(memerr.IOFailed, err, "cannot read the prepare bundle")
	}
	var value prepareSnapshot
	if err := json.Unmarshal(data, &value); err != nil {
		return prepareSnapshot{}, memerr.Wrap(memerr.IOFailed, err,
			"the prepare bundle is unreadable; run `memctl dream --prepare` again")
	}
	return value, nil
}

// sessionByFile returns the session a recorded digest file belongs to. The
// snapshot pairs the two lists in selection order, so the lookup falls back to
// the index when the file names are not resolvable (a repository that moved its
// state root between prepare and propose).
func (s prepareSnapshot) sessionByFile(file string) (BundleSession, bool) {
	for index, digest := range s.Digests {
		if digest.File != file {
			continue
		}
		if index < len(s.Sessions) {
			return s.Sessions[index], true
		}
		return BundleSession{}, false
	}
	return BundleSession{}, false
}

// sessionByIndex returns the session of one bundle index. The bundle assigns
// SessionIndex equal to the slice position, and a snapshot written by an older
// memctl is accepted positionally.
func sessionByIndex(sessions []BundleSession, index int) (BundleSession, bool) {
	if index < 0 || index >= len(sessions) {
		return BundleSession{}, false
	}
	return sessions[index], true
}

// trimRepoPrefix strips the "sha256:" partition prefix the state layout uses as
// a directory name.
func trimRepoPrefix(repoID string) string {
	const prefix = "sha256:"
	if len(repoID) > len(prefix) && repoID[:len(prefix)] == prefix {
		return repoID[len(prefix):]
	}
	return repoID
}
