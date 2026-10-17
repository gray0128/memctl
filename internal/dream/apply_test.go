package dream

import (
	"context"
	"testing"
)

// This file verifies the explicit-apply validation of stage D-2 (T036): the
// ordered change list is turned into an apply plan, a wrong patch hash is
// refused, a moved baseline marks the proposal stale and a mode change is
// rejected instead of committing a different revision.

const applyRunnerBody = `cat > MEMORY.md <<'EOF'
# Memory

## Index
- [[topic]]
- [[new]]
EOF
printf '# New\n- added\n' > new.md
`

func TestPrepareApplyBuildsOrderedPlan(t *testing.T) {
	repo := initRepo(t)
	service, _ := newTestService(t, repo, t.TempDir(), applyRunnerBody)
	ctx := context.Background()

	proposal, err := service.Preview(ctx, PreviewRequest{Runner: "test"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	plan, err := service.PrepareApply(ctx, proposal.ProposalID, proposal.PatchHash)
	if err != nil {
		t.Fatalf("PrepareApply: %v", err)
	}
	if plan.ProposalID != proposal.ProposalID || plan.BaseRevision != proposal.BaseRevision {
		t.Fatalf("plan does not quote the proposal: %+v", plan)
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("plan changes = %+v, want 2", plan.Changes)
	}
	byPath := map[string]ApplyChange{}
	for _, change := range plan.Changes {
		byPath[change.Path] = change
	}
	if got := byPath["MEMORY.md"]; got.Action != ChangeUpdate || got.Content == "" || got.ExpectedRevision == "" {
		t.Fatalf("update change = %+v", got)
	}
	if got := byPath["new.md"]; got.Action != ChangeCreate || got.ExpectedRevision != "absent" || got.Content != "# New\n- added\n" {
		t.Fatalf("create change = %+v", got)
	}

	// A wrong hash is refused without changing the proposal.
	if _, err := service.PrepareApply(ctx, proposal.ProposalID, "sha256:not-the-hash"); !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("wrong hash: err=%v, want INVALID_ARGUMENT", err)
	}
	if _, err := service.PrepareApply(ctx, "dp-00000000000000000000000000000000", proposal.PatchHash); !contractCode(err, "NOT_FOUND") {
		t.Fatalf("unknown proposal: err=%v, want NOT_FOUND", err)
	}
}

func TestPrepareApplyRejectsStaleAndModeChange(t *testing.T) {
	repo := initRepo(t)
	service, _ := newTestService(t, repo, t.TempDir(), applyRunnerBody)
	ctx := context.Background()

	proposal, err := service.Preview(ctx, PreviewRequest{Runner: "test"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	// A mode change cannot be reproduced by the managed write, so it is
	// rejected instead of committing under the original planned revision.
	value, err := service.store.Load(proposal.ProposalID)
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	entry := value.Planned["new.md"]
	entry.GitMode = "100755"
	value.Planned["new.md"] = entry
	if err := service.store.Save(value); err != nil {
		t.Fatalf("save record: %v", err)
	}
	if _, err := service.PrepareApply(ctx, proposal.ProposalID, proposal.PatchHash); !contractCode(err, "UNSUPPORTED") {
		t.Fatalf("mode change: err=%v, want UNSUPPORTED", err)
	}

	// A stale proposal cannot be applied at all.
	if err := service.MarkStale(ctx, proposal.ProposalID); err != nil {
		t.Fatalf("MarkStale: %v", err)
	}
	if _, err := service.PrepareApply(ctx, proposal.ProposalID, proposal.PatchHash); !contractCode(err, "PROPOSAL_STALE") {
		t.Fatalf("stale proposal: err=%v, want PROPOSAL_STALE", err)
	}
	if err := service.MarkApplying(ctx, proposal.ProposalID); !contractCode(err, "PROPOSAL_STALE") {
		t.Fatalf("reopening a stale proposal: err=%v, want PROPOSAL_STALE", err)
	}
}

func TestMarkAppliedIsTerminal(t *testing.T) {
	repo := initRepo(t)
	service, _ := newTestService(t, repo, t.TempDir(), applyRunnerBody)
	ctx := context.Background()

	proposal, err := service.Preview(ctx, PreviewRequest{Runner: "test"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if err := service.MarkApplying(ctx, proposal.ProposalID); err != nil {
		t.Fatalf("MarkApplying: %v", err)
	}
	if err := service.MarkApplied(ctx, proposal.ProposalID, "abc123"); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	// The idempotent repeat is accepted; moving an applied proposal forward is
	// not.
	if err := service.MarkApplied(ctx, proposal.ProposalID, "abc123"); err != nil {
		t.Fatalf("idempotent MarkApplied: %v", err)
	}
	if err := service.MarkApplied(ctx, proposal.ProposalID, "other"); !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("reapplying with another commit: err=%v, want INVALID_ARGUMENT", err)
	}
	if _, err := service.PrepareApply(ctx, proposal.ProposalID, proposal.PatchHash); !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("applying an applied proposal: err=%v, want INVALID_ARGUMENT", err)
	}
}
