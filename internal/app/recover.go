package app

import (
	"context"
	"strings"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/vault"
)

// Per-file recovery states reported by `recover --status`.
const (
	RecoverFileUnmodified = "unmodified"
	RecoverFilePlanned    = "planned"
	RecoverFileModified   = "modified"
)

// RecoverFileStatus is the current state of one affected path.
type RecoverFileStatus struct {
	Path  string
	State string
}

// RecoverTransactionStatus is the current state of one unfinished transaction.
type RecoverTransactionStatus struct {
	ID           string
	StartedAt    string
	OriginalHead string
	Commit       string
	Resolved     bool
	Reason       string
	Files        []RecoverFileStatus
}

// RecoverStatus lists every unfinished transaction and its files.
type RecoverStatus struct {
	Transactions []RecoverTransactionStatus
}

// RecoverStatus reports unfinished transactions without changing the
// repository.
func (a *App) RecoverStatus(ctx context.Context) (RecoverStatus, error) {
	session, err := a.beginLocked(ctx, false, false)
	if err != nil {
		return RecoverStatus{}, err
	}
	defer session.Close()

	pending, err := session.store.Pending()
	if err != nil {
		return RecoverStatus{}, err
	}
	status := RecoverStatus{}
	for _, record := range pending {
		described, err := session.describe(ctx, record)
		if err != nil {
			return RecoverStatus{}, err
		}
		status.Transactions = append(status.Transactions, described)
	}
	return status, nil
}

// RecoverRollback resolves unfinished transactions that are provably still
// controlled by memctl. It restores pre-images, unstages the affected paths and
// drops the record. A commit whose message carries the transaction marker is
// recognized instead of being rolled back; a path edited by hand or an
// unexpected HEAD refuses automatic recovery and is reported for manual work.
func (a *App) RecoverRollback(ctx context.Context) (RecoverStatus, error) {
	session, err := a.beginLocked(ctx, false, false)
	if err != nil {
		return RecoverStatus{}, err
	}
	defer session.Close()

	pending, err := session.store.Pending()
	if err != nil {
		return RecoverStatus{}, err
	}
	status := RecoverStatus{}
	var manual []string
	for _, record := range pending {
		described, err := session.describe(ctx, record)
		if err != nil {
			return status, err
		}
		if described.Resolved {
			if err := session.store.Delete(record.ID); err != nil {
				return status, err
			}
			status.Transactions = append(status.Transactions, described)
			continue
		}
		if described.Reason != "" {
			manual = append(manual, described.Reason)
			status.Transactions = append(status.Transactions, described)
			continue
		}

		blocked := false
		var manualPaths []string
		var selected []state.PathRecord
		for index, file := range described.Files {
			switch file.State {
			case RecoverFileModified:
				blocked = true
				manualPaths = append(manualPaths, file.Path)
			case RecoverFilePlanned:
				selected = append(selected, record.Paths[index])
			}
		}
		if blocked {
			described.Reason = "files changed after the interruption; memctl will not overwrite them: " + strings.Join(manualPaths, ", ")
			if err := session.restoreSelected(ctx, record, selected); err != nil {
				return status, memerr.Wrap(memerr.RecoveryRequired, err,
					"cannot roll back transaction %s", record.ID)
			}
			manual = append(manual, manualPaths...)
			status.Transactions = append(status.Transactions, described)
			continue
		}

		if err := session.restoreSelected(ctx, record, selected); err != nil {
			return status, memerr.Wrap(memerr.RecoveryRequired, err,
				"cannot roll back transaction %s", record.ID)
		}
		if err := session.store.Delete(record.ID); err != nil {
			return status, err
		}
		described.Resolved = true
		status.Transactions = append(status.Transactions, described)
	}
	if len(manual) > 0 {
		return status, memerr.New(memerr.RecoveryRequired,
			"automatic recovery refused; manual review required for: %s", strings.Join(manual, ", "))
	}
	return status, nil
}

// describe inspects one unfinished transaction against the current Git state
// and worktree.
func (s *writeSession) describe(ctx context.Context, record state.Transaction) (RecoverTransactionStatus, error) {
	described := RecoverTransactionStatus{
		ID:           record.ID,
		StartedAt:    record.StartedAt,
		OriginalHead: record.OriginalHead,
	}

	head, err := git.Head(ctx, s.repoPath)
	if err != nil {
		return described, err
	}
	if head != record.OriginalHead {
		message, err := git.HeadMessage(ctx, s.repoPath)
		if err != nil {
			return described, err
		}
		if strings.Contains(message, transactionTrailer+record.ID) {
			described.Commit = head
			described.Resolved = true
			return described, nil
		}
		described.Reason = "HEAD moved to " + head + " without the transaction marker; manual review required"
		return described, nil
	}

	for _, pathRecord := range record.Paths {
		fileState := RecoverFileStatus{Path: pathRecord.Path}
		meta, err := s.root.Stat(pathRecord.Path)
		if err != nil {
			return described, err
		}
		switch {
		case pathRecord.Existed:
			switch {
			case !meta.Exists:
				if pathRecord.PlannedRevision == vault.AbsentRevision {
					fileState.State = RecoverFilePlanned
				} else {
					fileState.State = RecoverFileModified
				}
			case meta.Revision == vault.FileRevision(pathRecord.Mode, []byte(pathRecord.Before)):
				fileState.State = RecoverFileUnmodified
			case meta.Revision == pathRecord.PlannedRevision:
				fileState.State = RecoverFilePlanned
			default:
				fileState.State = RecoverFileModified
			}
		default:
			if !meta.Exists {
				fileState.State = RecoverFileUnmodified
			} else if meta.Revision == pathRecord.PlannedRevision {
				fileState.State = RecoverFilePlanned
			} else {
				fileState.State = RecoverFileModified
			}
		}
		described.Files = append(described.Files, fileState)
	}
	return described, nil
}
