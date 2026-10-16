package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// journalRecord is the pre-image and planned result of one configuration file.
type journalRecord struct {
	Path              string `json:"path"`
	Existed           bool   `json:"existed"`
	BackupPath        string `json:"backupPath,omitempty"`
	Mode              string `json:"mode,omitempty"`
	InstalledRevision string `json:"installedRevision"`
	PlannedRevision   string `json:"plannedRevision"`
	Applied           bool   `json:"applied"`
}

// installJournal records an install in progress so an interrupted run can be
// recovered. It lives under the state root, not inside any host directory.
type installJournal struct {
	InstallID string          `json:"installId"`
	StartedAt string          `json:"startedAt"`
	Agent     string          `json:"agent"`
	Files     []journalRecord `json:"files"`
}

func (env Env) journalPath() string {
	return filepath.Join(env.StateDir, "adapters", "install.json")
}

func (env Env) backupDir(installID string) string {
	return filepath.Join(env.StateDir, "adapters", "backups", installID)
}

func newInstallID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "install-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "install-" + hex.EncodeToString(raw[:])
}

// newJournal starts a fresh install journal. A leftover journal means an
// earlier install was interrupted and must be recovered first.
func (env Env) newJournal(agent string) (*installJournal, error) {
	_, err := os.Stat(env.journalPath())
	switch {
	case err == nil:
		return nil, memerr.New(memerr.RecoveryRequired,
			"an earlier adapter install was interrupted; run install again to recover it before installing")
	case !errors.Is(err, fs.ErrNotExist):
		return nil, memerr.Wrap(memerr.IOFailed, err, "cannot read the adapter install journal")
	}
	return &installJournal{
		InstallID: newInstallID(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Agent:     agent,
	}, nil
}

func (env Env) saveJournal(journal *installJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot encode the adapter install journal")
	}
	return writeFileAtomic(env.journalPath(), data, 0o600)
}

// finishJournal removes the journal once every planned file was written. The
// backups stay in the state root for manual inspection.
func (env Env) finishJournal(journal *installJournal) error {
	_ = journal
	if err := os.Remove(env.journalPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return memerr.Wrap(memerr.IOFailed, err, "cannot clear the adapter install journal")
	}
	return nil
}

// PendingRecovery reports whether an interrupted install still needs recovery.
func (env Env) PendingRecovery() (bool, error) {
	if env.StateDir == "" {
		return false, nil
	}
	_, err := os.ReadFile(env.journalPath())
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, memerr.Wrap(memerr.IOFailed, err, "cannot read the adapter install journal")
	}
}

// RecoveryResult lists what an interrupted install recovery changed.
type RecoveryResult struct {
	Recovered bool
	Restored  []string
	Removed   []string
	Skipped   []string
}

// Recover restores the files an interrupted install changed. Only files whose
// content still matches the recorded installed revision are touched; a file the
// user edited afterwards is reported and left alone. See
// docs/03-workflows-and-dataflows.md section 7.
func (env Env) Recover() (RecoveryResult, error) {
	data, err := os.ReadFile(env.journalPath())
	if errors.Is(err, fs.ErrNotExist) {
		return RecoveryResult{}, nil
	}
	if err != nil {
		return RecoveryResult{}, memerr.Wrap(memerr.IOFailed, err, "cannot read the adapter install journal")
	}
	var journal installJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return RecoveryResult{}, memerr.Wrap(memerr.IOFailed, err, "the adapter install journal is unreadable")
	}

	result := RecoveryResult{Recovered: true}
	for index := len(journal.Files) - 1; index >= 0; index-- {
		record := journal.Files[index]
		if err := env.recoverRecord(record, &result); err != nil {
			return result, err
		}
	}
	if err := os.Remove(env.journalPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return result, memerr.Wrap(memerr.IOFailed, err, "cannot clear the adapter install journal")
	}
	return result, nil
}

func (env Env) recoverRecord(record journalRecord, result *RecoveryResult) error {
	current, exists, mode, err := env.readCurrent(record.Path)
	if err != nil {
		return err
	}
	if !exists || vault.FileRevision("100644", current) != record.InstalledRevision {
		result.Skipped = append(result.Skipped, record.Path)
		return nil
	}

	dir := filepath.Dir(record.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot create %s", dir)
	}
	held, err := lock.AcquirePath(context.Background(), filepath.Join(dir, "."+filepath.Base(record.Path)+".memctl.lock"))
	if err != nil {
		return err
	}
	defer held.Release()

	if !record.Existed {
		if err := os.Remove(record.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return memerr.Wrap(memerr.IOFailed, err, "cannot remove %s", record.Path)
		}
		result.Removed = append(result.Removed, record.Path)
		return nil
	}
	backup, err := env.readFile(record.BackupPath)
	if err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot read the backup %s", record.BackupPath)
	}
	restoreMode := modeOrDefault(mode)
	if record.Mode != "" {
		if parsed, parseErr := strconv.ParseUint(record.Mode, 8, 32); parseErr == nil {
			restoreMode = os.FileMode(parsed)
		}
	}
	if err := writeFileAtomic(record.Path, backup, restoreMode); err != nil {
		return err
	}
	result.Restored = append(result.Restored, record.Path)
	return nil
}
