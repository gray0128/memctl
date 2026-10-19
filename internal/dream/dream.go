// Package dream implements the Dreaming pipeline: it fixes a clean repository
// baseline, publishes it as the GitHub Actions request branch, validates the
// untrusted result tree the worker publishes against that baseline and persists
// a reviewable proposal. The local side never calls a model and never modifies
// the main repository. See docs/03-workflows-and-dataflows.md section 6,
// docs/02-domain-models-and-contracts.md section 2.3 and
// docs/11-github-actions-dreaming.md.
package dream

import "time"

// Proposal status values. Generating and the terminal ready/failed states are
// produced by stage D-1; applying/applied/stale are produced by the explicit
// apply workflow of stage D-2.
const (
	StatusGenerating = "generating"
	StatusReady      = "ready"
	StatusApplying   = "applying"
	StatusApplied    = "applied"
	StatusStale      = "stale"
	StatusFailed     = "failed"
)

// Change actions of one ordered proposal change.
const (
	ChangeCreate = "create"
	ChangeUpdate = "update"
	ChangeDelete = "delete"
)

// Documented runtime limits. They mirror the request limits of
// docs/02-domain-models-and-contracts.md section 4.1 and the text limit of
// internal/vault.
const (
	// MaxChanges bounds the number of files one proposal may touch.
	MaxChanges = 100
	// MaxChangeBytes bounds the total planned content of one proposal.
	MaxChangeBytes = 8 << 20
	// DefaultTimeout bounds the wait for one GitHub Actions worker result. It is
	// longer than the workflow timeout so a slow Actions queue still completes.
	DefaultTimeout = 10 * time.Minute
	// MaxPatchBytes bounds the human-readable unified diff stored with a
	// proposal. A larger diff is replaced by a truncation notice; the ordered
	// change list remains authoritative.
	MaxPatchBytes = 4 << 20
)

// Change is one ordered entry of a proposal. Path uses `/` separators and is
// root-relative to the exported memory tree. BaseRevision is the file revision
// at the fixed baseline (absent for a creation) and PlannedRevision binds the
// proposed content, so patchHash does not depend on the rendered patch text.
type Change struct {
	Path            string `json:"path"`
	Action          string `json:"action"`
	BaseRevision    string `json:"baseRevision,omitempty"`
	PlannedRevision string `json:"plannedRevision"`
	Patch           string `json:"patch,omitempty"`
}

// Proposal is the DreamProposal of docs/02-domain-models-and-contracts.md
// section 2.3. It is persisted under $STATE/repos/<repoId>/proposals and stays
// queryable after the generating process exits.
type Proposal struct {
	ProposalID    string   `json:"proposalId"`
	RepoID        string   `json:"repoId"`
	BaseRevision  string   `json:"baseRevision"`
	BaseHead      string   `json:"baseHead"`
	Runner        string   `json:"runner"`
	Status        string   `json:"status"`
	PatchHash     string   `json:"patchHash,omitempty"`
	Diff          string   `json:"diff,omitempty"`
	Changes       []Change `json:"changes,omitempty"`
	Warnings      []string `json:"warnings"`
	CreatedAt     string   `json:"createdAt"`
	AppliedCommit string   `json:"appliedCommit,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// Source identifies the repository and state root the service operates on.
// RepoPath is the canonical absolute repository root; RepoID is its local
// state partition key.
type Source struct {
	RepoPath string
	RepoID   string
	StateDir string
}

// PreviewRequest is one generation request. BaseRevision is optional at the
// library level and, when non-empty, is checked against the fixed baseline like
// every other managed precondition. Runner names a built-in model provider.
type PreviewRequest struct {
	Runner       string
	BaseRevision string
}

// plannedFile is the internal apply input of one create or update change: the
// exact bytes the worker produced and the Git mode they were read with. It is
// persisted with the proposal record but deliberately not part of the
// DreamProposal API shape. See
// docs/03-workflows-and-dataflows.md section 6 steps 5-6.
type plannedFile struct {
	Content string `json:"content"`
	GitMode string `json:"gitMode"`
	// BaseGitMode is the fixed baseline mode. An apply rejects a proposal that
	// changes the mode because the managed write preserves it.
	BaseGitMode string `json:"baseGitMode,omitempty"`
}

// ApplyChange is one validated change of an applyable proposal.
type ApplyChange struct {
	Path             string
	Action           string
	ExpectedRevision string
	Content          string
}

// ApplyPlan is the validated, ordered change set of one proposal that
// app.ApplyDream hands to the managed transaction. It carries no repository
// state: the caller revalidates the baseline under the repository lock.
type ApplyPlan struct {
	ProposalID   string
	BaseRevision string
	BaseHead     string
	Changes      []ApplyChange
}
