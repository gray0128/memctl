package dream

import (
	"context"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file implements the explicit apply workflow of stage D-2: it validates
// one persisted proposal into an ordered, ready-to-commit change set and
// records the reviewing status. It never touches the main repository; the
// application service revalidates the baseline under the repository lock and
// commits the plan with the managed transaction. See
// docs/03-workflows-and-dataflows.md section 6 steps 6-7 and
// docs/02-domain-models-and-contracts.md section 3 (app.ApplyDream).

// PrepareApply loads one proposal and validates that it can be applied under
// the quoted patch hash. It checks the ordered change list, the recorded apply
// input and the planned revisions, but deliberately not the repository
// baseline: app.ApplyDream revalidates that under the lock and marks the
// proposal stale when it moved.
func (s *Service) PrepareApply(ctx context.Context, proposalID, expectedHash string) (ApplyPlan, error) {
	value, err := s.loadRecord(ctx, proposalID)
	if err != nil {
		return ApplyPlan{}, err
	}
	switch value.Status {
	case StatusReady, StatusApplying:
	case StatusStale:
		return ApplyPlan{}, memerr.New(memerr.ProposalStale,
			"proposal %s was generated against an older repository revision; generate a new proposal", proposalID)
	default:
		return ApplyPlan{}, memerr.New(memerr.InvalidArgument,
			"proposal %s is %s and cannot be applied", proposalID, value.Status)
	}
	if expectedHash == "" || expectedHash != value.PatchHash {
		return ApplyPlan{}, memerr.New(memerr.InvalidArgument,
			"the patch hash does not match proposal %s; reread the proposal before applying", proposalID)
	}
	if len(value.Changes) == 0 {
		return ApplyPlan{}, memerr.New(memerr.InvalidArgument,
			"proposal %s has no changes to apply", proposalID)
	}
	// Recompute the hash from the persisted ordered list so a record that was
	// modified after generation is rejected instead of applied.
	if computed := patchHash(value.Changes); computed != value.PatchHash {
		return ApplyPlan{}, memerr.New(memerr.IOFailed,
			"proposal %s record is inconsistent; generate a new proposal", proposalID)
	}

	plan := ApplyPlan{
		ProposalID:   proposalID,
		BaseRevision: value.BaseRevision,
		BaseHead:     value.BaseHead,
		Changes:      make([]ApplyChange, 0, len(value.Changes)),
	}
	for _, change := range value.Changes {
		switch change.Action {
		case ChangeCreate, ChangeUpdate:
			planned, ok := value.Planned[change.Path]
			if !ok {
				return ApplyPlan{}, memerr.New(memerr.IOFailed,
					"proposal %s has no recorded content for %q; generate a new proposal", proposalID, change.Path)
			}
			if err := verifyPlanned(proposalID, change, planned); err != nil {
				return ApplyPlan{}, err
			}
			expected := change.BaseRevision
			if change.Action == ChangeCreate && expected == "" {
				expected = vault.AbsentRevision
			}
			plan.Changes = append(plan.Changes, ApplyChange{
				Path:             change.Path,
				Action:           change.Action,
				ExpectedRevision: expected,
				Content:          planned.Content,
			})
		case ChangeDelete:
			plan.Changes = append(plan.Changes, ApplyChange{
				Path:             change.Path,
				Action:           ChangeDelete,
				ExpectedRevision: change.BaseRevision,
			})
		default:
			return ApplyPlan{}, memerr.New(memerr.Unsupported,
				"proposal %s uses the unsupported change action %q", proposalID, change.Action)
		}
	}
	return plan, nil
}

// verifyPlanned enforces that the recorded apply input reproduces the planned
// revision. The managed write preserves the file mode, so a proposal that
// changes the mode is refused instead of silently committing a different
// revision. See docs/03-workflows-and-dataflows.md section 6 step 4.
func verifyPlanned(proposalID string, change Change, planned plannedFile) error {
	switch change.Action {
	case ChangeCreate:
		if planned.GitMode != "100644" {
			return memerr.New(memerr.Unsupported,
				"proposal %s marks %q executable; applying a mode change is not supported", proposalID, change.Path)
		}
	case ChangeUpdate:
		if planned.BaseGitMode == "" || planned.GitMode != planned.BaseGitMode {
			return memerr.New(memerr.Unsupported,
				"proposal %s changes the file mode of %q; applying a mode change is not supported", proposalID, change.Path)
		}
	}
	if got := vault.FileRevision(planned.GitMode, []byte(planned.Content)); got != change.PlannedRevision {
		return memerr.New(memerr.IOFailed,
			"proposal %s content for %q does not match its planned revision; generate a new proposal", proposalID, change.Path)
	}
	return nil
}

// MarkApplying records that the explicit apply started. It is written before
// the managed transaction so an interrupted apply is distinguishable from a
// proposal that was never confirmed.
func (s *Service) MarkApplying(_ context.Context, proposalID string) error {
	return s.setStatus(proposalID, StatusApplying, "")
}

// MarkReady records that an apply attempt failed cleanly, so the reviewed
// proposal can be applied again without regenerating it.
func (s *Service) MarkReady(_ context.Context, proposalID string) error {
	return s.setStatus(proposalID, StatusReady, "")
}

// MarkStale records that the repository moved past the proposal baseline.
func (s *Service) MarkStale(_ context.Context, proposalID string) error {
	return s.setStatus(proposalID, StatusStale, "")
}

// MarkApplied records the commit that applied the proposal. It is written after
// the local commit so the proposal status never claims a commit that did not
// happen.
func (s *Service) MarkApplied(_ context.Context, proposalID, commit string) error {
	return s.setStatus(proposalID, StatusApplied, commit)
}

// setStatus persists one proposal status transition. A proposal that is already
// applied is never moved backwards, so a lost response cannot make an applied
// proposal applyable again.
func (s *Service) setStatus(proposalID, status, commit string) error {
	value, err := s.store.Load(proposalID)
	if err != nil {
		return err
	}
	if value.Status == StatusApplied {
		if status == StatusApplied && value.AppliedCommit == commit {
			return nil
		}
		return memerr.New(memerr.InvalidArgument,
			"proposal %s is already applied as %s", proposalID, value.AppliedCommit)
	}
	if value.Status == StatusStale && status != StatusStale {
		return memerr.New(memerr.ProposalStale,
			"proposal %s is stale; generate a new proposal", proposalID)
	}
	value.Status = status
	value.Error = ""
	switch status {
	case StatusApplied:
		value.AppliedCommit = commit
	case StatusStale:
		value.Error = "the repository changed since the proposal was generated; generate a new proposal"
	}
	return s.store.Save(value)
}
