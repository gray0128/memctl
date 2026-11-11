package dream

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
)

// This file persists local prepare bundles. `propose` receives integer session
// indices, and an index is only meaningful against the exact list the agent
// saw; re-deriving the list at propose time would shift every index when the
// selection order differs (another working directory, one more collected
// session). Each prepare writes a content-addressed `prepare-<bundleId>.json`
// snapshot (retaining the 5 most recent bundles) and also updates
// `prepare.json` as the backward-compatible fallback when `bundleId` is not
// supplied.

const maxRetainedPrepareBundles = 5

// prepareSnapshot is the on-disk form of a prepare bundle.
type prepareSnapshot struct {
	BundleID     string          `json:"bundleId,omitempty"`
	BaseHead     string          `json:"baseHead"`
	BaseRevision string          `json:"baseRevision"`
	Sessions     []BundleSession `json:"sessions"`
	Digests      []CorpusDigest  `json:"digests"`
	CreatedAt    string          `json:"createdAt"`
}

func computeBundleID(head, revision string, sessions []BundleSession, digests []CorpusDigest) string {
	h := sha256.New()
	_, _ = h.Write([]byte(head + "\x00" + revision + "\x00"))
	for _, session := range sessions {
		_, _ = h.Write([]byte(session.Agent + "\x00" + session.SessionID + "\x00" + session.SourceTag + "\x00" + session.Date + "\x00" + session.CwdKey + "\x00"))
		for _, turn := range session.Turns {
			_, _ = h.Write([]byte(turn.User + "\x00" + turn.Assistant + "\x00"))
		}
	}
	for _, digest := range digests {
		_, _ = h.Write([]byte(digest.File + "\x00" + digest.Revision + "\x00"))
	}
	sum := h.Sum(nil)
	return "pb-" + hex.EncodeToString(sum[:8])
}

func (s *Service) prepareDir() string {
	return filepath.Join(s.Source.StateDir, "repos", trimRepoPrefix(s.Source.RepoID), "cache", "dream")
}

// preparePath is the default snapshot file of one repository.
func (s *Service) preparePath() string {
	return filepath.Join(s.prepareDir(), "prepare.json")
}

func (s *Service) prepareBundlePath(bundleID string) string {
	return filepath.Join(s.prepareDir(), "prepare-"+bundleID+".json")
}

func isValidBundleID(bundleID string) bool {
	if bundleID == "" || strings.ContainsAny(bundleID, "/\\") || strings.Contains(bundleID, "..") {
		return false
	}
	for _, r := range bundleID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
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
	dir := s.prepareDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	writePrepareFileAtomically(dir, s.preparePath(), data)
	if isValidBundleID(value.BundleID) {
		writePrepareFileAtomically(dir, s.prepareBundlePath(value.BundleID), data)
		s.prunePrepareBundles(value.BundleID)
	}
}

func writePrepareFileAtomically(dir, target string, data []byte) {
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
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
	_ = os.Rename(name, target)
}

func (s *Service) prunePrepareBundles(keepBundleID string) {
	dir := s.prepareDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type bundleEntry struct {
		name      string
		createdAt string
		mtime     time.Time
		keep      bool
	}
	var list []bundleEntry
	keepName := "prepare-" + keepBundleID + ".json"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "prepare-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		var createdAt string
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			var snap struct {
				CreatedAt string `json:"createdAt"`
			}
			if json.Unmarshal(raw, &snap) == nil {
				createdAt = snap.CreatedAt
			}
		}
		list = append(list, bundleEntry{
			name:      name,
			createdAt: createdAt,
			mtime:     info.ModTime(),
			keep:      name == keepName,
		})
	}
	if len(list) <= maxRetainedPrepareBundles {
		return
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].keep != list[j].keep {
			return list[i].keep
		}
		if list[i].createdAt != list[j].createdAt {
			return list[i].createdAt > list[j].createdAt
		}
		if !list[i].mtime.Equal(list[j].mtime) {
			return list[i].mtime.After(list[j].mtime)
		}
		return list[i].name > list[j].name
	})
	for _, item := range list[maxRetainedPrepareBundles:] {
		_ = os.Remove(filepath.Join(dir, item.name))
	}
}

// loadPrepare reads the requested snapshot when bundleID is non-empty, or the
// latest default snapshot otherwise.
func (s *Service) loadPrepare(bundleID string) (prepareSnapshot, error) {
	bundleID = strings.TrimSpace(bundleID)
	path := s.preparePath()
	if bundleID != "" {
		if !isValidBundleID(bundleID) {
			return prepareSnapshot{}, memerr.New(memerr.InvalidArgument,
				"prepare bundleId %q is invalid; run `memctl dream --prepare` again", bundleID)
		}
		path = s.prepareBundlePath(bundleID)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if bundleID != "" {
			return prepareSnapshot{}, memerr.New(memerr.InvalidArgument,
				"prepare bundle %q is not on record; run `memctl dream --prepare` before proposing", bundleID)
		}
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
	if bundleID != "" && value.BundleID != "" && value.BundleID != bundleID {
		return prepareSnapshot{}, memerr.New(memerr.InvalidArgument,
			"prepare bundle %q does not match stored bundle %q; run `memctl dream --prepare` again", bundleID, value.BundleID)
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
