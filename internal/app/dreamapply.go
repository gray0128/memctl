package app

import (
	"context"
	"strings"

	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
)

// This file implements the explicit Dreaming apply of stage D-2. It loads a
// persisted, already-reviewed proposal, hands its ordered change set to the
// managed write transaction app.Apply and records the resulting commit on the
// proposal. It never reruns the model and never modifies the main repository
// before the transaction commits. See
// docs/03-workflows-and-dataflows.md section 6 steps 6-7 and
// docs/02-domain-models-and-contracts.md section 3 (app.ApplyDream).

// ApplyDream applies one reviewed Dreaming proposal. The caller quotes the
// proposalId and the patchHash it saw; a stale baseline marks the proposal
// stale and changes nothing. A repeated apply returns the original commit
// instead of committing twice.
func (a *App) ApplyDream(ctx context.Context, proposalID, patchHash string) (MutationResult, error) {
	service := a.dreamService()
	proposal, err := service.Load(ctx, proposalID)
	if err != nil {
		return MutationResult{}, err
	}
	switch proposal.Status {
	case dream.StatusApplied:
		return dreamAppliedResult(proposal, ""), nil
	case dream.StatusStale:
		return MutationResult{}, staleProposal(proposalID)
	case dream.StatusGenerating, dream.StatusFailed:
		return MutationResult{}, memerr.New(memerr.InvalidArgument,
			"proposal %s is %s and cannot be applied; generate a new proposal", proposalID, proposal.Status)
	}

	plan, err := service.PrepareApply(ctx, proposalID, patchHash)
	if err != nil {
		return MutationResult{}, err
	}

	// Reconcile an earlier attempt before starting a new one. A completed
	// transaction record proves the commit happened; an unfinished record is
	// rolled back so the retry commits a clean change set.
	found, recovered, err := a.reconcileDreamApply(ctx, proposalID)
	if err != nil {
		return MutationResult{}, err
	}
	if found {
		if proposal.Status != dream.StatusApplied {
			if markErr := service.MarkApplied(ctx, proposalID, recovered.Commit); markErr != nil {
				return recovered, markErr
			}
		}
		return recovered, nil
	}

	if err := service.MarkApplying(ctx, proposalID); err != nil {
		return MutationResult{}, err
	}
	result, err := a.Apply(ctx, MutationRequest{
		BaseRevision:    plan.BaseRevision,
		DreamProposalID: proposalID,
		Operation:       "dream apply",
		Changes:         dreamFileChanges(plan),
	})
	if err != nil {
		if dreamBaselineError(err) {
			// A concurrent apply may have won the race; prefer its result.
			if foundAgain, concurrent, rerr := a.reconcileDreamApply(ctx, proposalID); rerr == nil && foundAgain {
				_ = service.MarkApplied(ctx, proposalID, concurrent.Commit)
				return concurrent, nil
			}
			if markErr := service.MarkStale(ctx, proposalID); markErr != nil {
				return result, markErr
			}
			return result, staleProposal(proposalID)
		}
		if code, ok := memerr.As(err); !ok || code.Code != memerr.RecoveryRequired {
			// The managed transaction rolled back cleanly, so the reviewed
			// proposal stays applyable.
			_ = service.MarkReady(ctx, proposalID)
		}
		return result, err
	}
	if err := service.MarkApplied(ctx, proposalID, result.Commit); err != nil {
		return result, err
	}
	// Best-effort cleanup of the completed transaction record the apply kept
	// for reconciliation. It does not block on a concurrent background sync.
	a.clearDreamRecords(ctx, proposalID)
	return result, nil
}

// clearDreamRecords deletes the completed transaction records a successful
// apply kept for reconciliation. It takes the repository lock without waiting
// so a background sync that already holds it is never stalled; a skipped
// cleanup only leaves a small completed record behind.
func (a *App) clearDreamRecords(ctx context.Context, proposalID string) {
	session, err := a.beginLocked(ctx, false, false, true)
	if err != nil {
		return
	}
	defer session.Close()
	records, err := session.store.List()
	if err != nil {
		return
	}
	for _, record := range records {
		if record.ProposalID == proposalID && record.Completed {
			_ = session.store.Delete(record.ID)
		}
	}
}

// reconcileDreamApply inspects every transaction recorded for one proposal.
// It returns found when a completed record already proves the commit, restores
// the pre-images of an unfinished record and deletes each reconciled record.
func (a *App) reconcileDreamApply(ctx context.Context, proposalID string) (found bool, result MutationResult, err error) {
	session, err := a.beginLocked(ctx, false, false, false)
	if err != nil {
		return false, MutationResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	records, err := session.store.List()
	if err != nil {
		return false, MutationResult{}, err
	}
	for _, record := range records {
		if record.ProposalID != proposalID {
			continue
		}
		if record.Completed && record.Commit != "" {
			if err := session.store.Delete(record.ID); err != nil {
				return false, MutationResult{}, err
			}
			return true, session.dreamResult(record, record.Commit), nil
		}
		described, err := session.describe(ctx, record)
		if err != nil {
			return false, MutationResult{}, err
		}
		if described.Resolved && described.Commit != "" {
			if err := session.store.Delete(record.ID); err != nil {
				return false, MutationResult{}, err
			}
			return true, session.dreamResult(record, described.Commit), nil
		}
		if described.Reason != "" {
			return false, MutationResult{}, memerr.New(memerr.RecoveryRequired,
				"the interrupted apply of proposal %s needs manual recovery: %s", proposalID, described.Reason)
		}
		// Restore only the files that are still exactly the planned content.
		// A file edited by hand after the interruption is never overwritten,
		// matching the W06 rule of `memctl recover`.
		var selected []state.PathRecord
		var manual []string
		for index, file := range described.Files {
			switch file.State {
			case RecoverFileModified:
				manual = append(manual, file.Path)
			case RecoverFilePlanned:
				selected = append(selected, record.Paths[index])
			}
		}
		if len(manual) > 0 {
			return false, MutationResult{}, memerr.New(memerr.RecoveryRequired,
				"the interrupted apply of proposal %s left files that were then edited by hand: %s; run `memctl recover --status`",
				proposalID, strings.Join(manual, ", "))
		}
		if err := session.restoreSelected(ctx, record, selected); err != nil {
			return false, MutationResult{}, memerr.Wrap(memerr.RecoveryRequired, err,
				"cannot roll back the interrupted apply of proposal %s", proposalID)
		}
		if err := session.store.Delete(record.ID); err != nil {
			return false, MutationResult{}, err
		}
		return false, MutationResult{}, nil
	}
	return false, MutationResult{}, nil
}

// dreamResult builds the repeated-apply result from a reconciled transaction.
func (s *writeSession) dreamResult(record state.Transaction, commit string) MutationResult {
	return MutationResult{
		TransactionID: record.ID,
		Commit:        commit,
		ChangedFiles:  record.AffectedPaths(),
		SyncState:     s.remoteStateAfterCommit(s.ctx),
	}
}

// dreamFileChanges maps the validated proposal changes to the managed write
// change set of app.Apply.
func dreamFileChanges(plan dream.ApplyPlan) []FileChange {
	changes := make([]FileChange, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		action := ActionPut
		if change.Action == dream.ChangeDelete {
			action = ActionDelete
		}
		changes = append(changes, FileChange{
			Path:             change.Path,
			ExpectedRevision: change.ExpectedRevision,
			Action:           action,
			RawContent:       change.Content,
		})
	}
	return changes
}

// dreamBaselineError reports whether err means the repository moved past the
// proposal baseline.
func dreamBaselineError(err error) bool {
	code, ok := memerr.As(err)
	if !ok {
		return false
	}
	return code.Code == memerr.RevisionMismatch || code.Code == memerr.ExternalChanges
}

// dreamAppliedResult builds the idempotent result of an already-applied
// proposal.
func dreamAppliedResult(proposal dream.Proposal, syncState string) MutationResult {
	files := make([]string, 0, len(proposal.Changes))
	for _, change := range proposal.Changes {
		files = append(files, change.Path)
	}
	return MutationResult{Commit: proposal.AppliedCommit, ChangedFiles: files, SyncState: syncState}
}

// staleProposal is the shared rejection of an expired proposal.
func staleProposal(proposalID string) error {
	return memerr.New(memerr.ProposalStale,
		"proposal %s was generated against an older repository revision; generate a new proposal", proposalID)
}
