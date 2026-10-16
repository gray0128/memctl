// Package syncer orchestrates the foreground Git synchronization of one
// repository: fetch, fast-forward or replay, and push. The application service
// holds the repository lock and calls this package; the package never acquires
// the lock itself and never decides to keep a user's Git operation.
//
// See docs/03-workflows-and-dataflows.md section 4.
package syncer

import (
	"context"
	"errors"
	"strings"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
)

// Remote state values from docs/02-domain-models-and-contracts.md section 2.2.
const (
	StateLocalOnly = "local_only"
	StatePending   = "pending"
	StateSynced    = "synced"
	StateConflict  = "conflict"
	StateFailed    = "failed"
)

// Request describes one synchronization attempt.
type Request struct {
	// RepoPath is the canonical working-tree root. The caller holds the lock.
	RepoPath string
	// RepoID partitions local state; it is not a cross-device identity.
	RepoID string
	// Remote overrides remote detection when set.
	Remote string
	// Interactive allows Git and SSH native prompts when the caller has a
	// terminal. NonInteractive forces a prompt-free child.
	Interactive    bool
	NonInteractive bool
}

// Result is the observable outcome of one synchronization attempt.
type Result struct {
	Branch      string
	Head        string
	HasRemote   bool
	HasUpstream bool
	Ahead       int
	Behind      int
	RemoteState string
	ReasonCode  string
	// Conflict is set when the replay stopped on a content conflict. Its
	// RepoID and BaseRevision are filled in by the application service.
	Conflict *state.Conflict
}

// Run performs one foreground synchronization. It returns a Result even on
// failure so the caller can persist the reason code and remote state.
func Run(ctx context.Context, req Request) (Result, error) {
	res := Result{RemoteState: StateLocalOnly}
	repo := req.RepoPath

	if git.UnbornHead(ctx, repo) {
		return res, nil
	}
	head, err := git.Head(ctx, repo)
	if err != nil {
		return res, err
	}
	res.Head = head

	branch, err := git.HeadBranch(ctx, repo)
	if err != nil {
		return res, err
	}
	res.Branch = branch

	remote, hasRemote, err := git.DefaultRemote(ctx, repo)
	if err != nil {
		return res, err
	}
	if !hasRemote {
		return res, nil
	}
	remote = chooseRemote(req.Remote, remote)
	res.HasRemote = true

	opts := git.Options{Interactive: req.Interactive, NonInteractive: req.NonInteractive}

	upstream, hasUpstream, err := git.UpstreamRef(ctx, repo)
	if err != nil {
		return res, err
	}
	pushed := false
	if !hasUpstream {
		upstream, pushed, err = establishUpstream(ctx, repo, remote, branch, opts)
		if err != nil {
			return failRemote(res, Classify(err), err)
		}
	}
	res.HasUpstream = true
	if pushed {
		// An empty remote received the first push with -u, so there is
		// nothing left to fetch, replay or push.
		if head, err := git.Head(ctx, repo); err == nil {
			res.Head = head
		}
		res.Ahead, res.Behind = 0, 0
		res.RemoteState = StateSynced
		return res, nil
	}

	if err := git.Fetch(ctx, repo, opts, remote); err != nil {
		return failRemote(res, Classify(err), err)
	}

	// Fix the target commit now so a later remote movement cannot change what
	// this attempt replays onto.
	upstreamCommit, err := git.RevParse(ctx, repo, upstream)
	if err != nil {
		return res, err
	}

	ahead, behind, err := git.AheadBehind(ctx, repo, upstreamCommit)
	if err != nil {
		return res, err
	}
	res.Ahead, res.Behind = ahead, behind

	switch {
	case behind > 0 && ahead == 0:
		if err := git.FastForward(ctx, repo, upstreamCommit); err != nil {
			return failRemote(res, Classify(err), err)
		}
	case behind > 0 && ahead > 0:
		merges, err := git.HasMergeCommits(ctx, repo, upstreamCommit+"..HEAD")
		if err != nil {
			return res, err
		}
		if merges {
			return res, memerr.New(memerr.Unsupported,
				"local history contains merge commits; memctl does not replay merges automatically, resolve manually")
		}
		if err := replay(ctx, req, upstreamCommit); err != nil {
			var conflictErr *ConflictError
			if errors.As(err, &conflictErr) {
				res.RemoteState = StateConflict
				res.ReasonCode = string(memerr.ContentConflict)
				res.Conflict = conflictErr.Conflict
				return res, nil
			}
			return failRemote(res, Classify(err), err)
		}
	}

	pushOpts := git.Options{Interactive: req.Interactive, NonInteractive: req.NonInteractive}
	// Tracking is already established: either it existed before or
	// establishUpstream set it, so this push must not set it again.
	if err := git.Push(ctx, repo, pushOpts, remote, res.Branch, false); err != nil {
		return failRemote(res, Classify(err), err)
	}

	if head, err := git.Head(ctx, repo); err == nil {
		res.Head = head
	}
	res.HasUpstream = true
	// A completed sync leaves nothing ahead or behind, whatever the counts
	// were before the fast-forward, replay or push.
	res.Ahead, res.Behind = 0, 0
	res.RemoteState = StateSynced
	res.ReasonCode = ""
	return res, nil
}

// establishUpstream implements the no-upstream branch of the initialization
// protocol: an empty remote receives the first push, while a remote with
// history must share a common ancestor and declare a default branch. pushed
// reports that the first push already published the branch, so the caller does
// not repeat the sync.
func establishUpstream(ctx context.Context, repo, remote, branch string, opts git.Options) (upstream string, pushed bool, err error) {
	advertised, err := git.LsRemote(ctx, repo, opts, remote)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(advertised) == "" {
		if err := git.Push(ctx, repo, opts, remote, branch, true); err != nil {
			return "", false, err
		}
		return remote + "/" + branch, true, nil
	}

	if err := git.Fetch(ctx, repo, opts, remote); err != nil {
		return "", false, err
	}
	defaultBranch, err := git.RemoteDefaultBranch(ctx, repo, opts, remote)
	if err != nil {
		return "", false, err
	}
	remoteRef := remote + "/" + defaultBranch
	if _, err := git.MergeBase(ctx, repo, "HEAD", remoteRef); err != nil {
		return "", false, memerr.New(memerr.NoUpstream,
			"%s has no common history with this repository; connecting it requires manual preparation", remoteRef)
	}
	if _, err := git.Run(ctx, repo, "branch", "--set-upstream-to", remoteRef, branch); err != nil {
		return "", false, err
	}
	return remoteRef, false, nil
}

// replay rebases the local branch onto upstream. A stopped rebase with
// unmerged paths is captured and aborted; the returned error wraps the
// captured *state.Conflict so Run can report it. Any other stopped operation
// is aborted and reported as a plain error.
func replay(ctx context.Context, req Request, upstream string) error {
	repo := req.RepoPath
	store, err := openStore(ctx, repo)
	if err != nil {
		return err
	}
	head, err := git.Head(ctx, repo)
	if err != nil {
		return err
	}
	operationID, err := state.NewOperationID()
	if err != nil {
		return err
	}
	operation := state.Operation{
		OperationID:  operationID,
		Kind:         state.KindRebase,
		Owner:        state.OwnerMemctl,
		OriginalHead: head,
		TargetHead:   upstream,
		Phase:        "running",
		StartedAt:    state.Now(),
	}
	if err := store.SaveOperation(operation); err != nil {
		return err
	}

	rebaseErr := git.Rebase(ctx, repo, upstream)
	if rebaseErr == nil {
		if err := store.DeleteOperation(); err != nil {
			return err
		}
		return nil
	}

	unmerged, err := git.UnmergedPaths(ctx, repo)
	if err != nil {
		return err
	}
	if len(unmerged) == 0 {
		_ = abortRebase(ctx, repo)
		_ = store.DeleteOperation()
		return rebaseErr
	}

	conflict, err := CaptureStopped(ctx, repo, req.RepoID, operationID, upstream, PhaseCaptured)
	if err != nil {
		return err
	}
	conflict.OriginalHead = head
	conflict.ExpectedHead = head
	if err := abortRebase(ctx, repo); err != nil {
		return memerr.Wrap(memerr.RecoveryRequired, err,
			"the rebase stopped on a conflict but could not be aborted; inspect the repository manually")
	}
	if restored, err := git.Head(ctx, repo); err != nil || restored != head {
		return memerr.New(memerr.RecoveryRequired,
			"the rebase was aborted but HEAD is not the original commit; inspect the repository manually")
	}
	if dirty, err := git.StatusPorcelain(ctx, repo); err != nil {
		return err
	} else if dirty != "" {
		return memerr.New(memerr.RecoveryRequired,
			"the aborted rebase left worktree changes; inspect the repository manually")
	}

	operation.Phase = "captured"
	operation.ConflictID = conflict.ConflictID
	if err := store.SaveOperation(operation); err != nil {
		return err
	}
	return &ConflictError{Conflict: conflict}
}

// ConflictError reports a stopped rebase that was captured and aborted. It
// carries the snapshot for the application service to persist.
type ConflictError struct {
	Conflict *state.Conflict
}

func (e *ConflictError) Error() string {
	return "content conflict captured: " + e.Conflict.ConflictID
}

// Conflict returns the persisted conflict snapshot, if one is active.
func Conflict(ctx context.Context, repo string) (state.Conflict, bool, error) {
	store, err := openStore(ctx, repo)
	if err != nil {
		return state.Conflict{}, false, err
	}
	return store.LoadConflict()
}

func abortRebase(ctx context.Context, repo string) error {
	repoState, err := git.Inspect(ctx, repo)
	if err != nil {
		return err
	}
	if repoState.Operation != "rebase" {
		return nil
	}
	return git.RebaseAbort(ctx, repo)
}

func openStore(ctx context.Context, repo string) (*state.Store, error) {
	gitDir, err := git.GitDir(ctx, repo)
	if err != nil {
		return nil, err
	}
	return state.OpenStore(gitDir), nil
}

func chooseRemote(override, detected string) string {
	if override != "" {
		return override
	}
	return detected
}

// Classify maps a Git failure to one of the internal error classes using the
// recorded stderr. A deadline always wins because the exchange cannot be
// proven complete. A contract error that already carries a specific code, such
// as NO_UPSTREAM from the initialization protocol, keeps that code instead of
// being flattened to GIT_FAILED. See docs/03-workflows-and-dataflows.md
// section 4.3.
func Classify(err error) memerr.Code {
	var gitErr *git.Error
	if errors.As(err, &gitErr) {
		if gitErr.TimedOut {
			return memerr.Timeout
		}
		text := strings.ToLower(gitErr.Stderr)
		switch {
		case containsAny(text,
			"permission denied", "authentication failed", "could not read username",
			"host key verification failed", "no such identity", "invalid credentials",
			"terminal prompts disabled", "publickey", "access denied", "authentication required"):
			return memerr.AuthFailed
		case containsAny(text,
			"non-fast-forward", "fetch first", "failed to push some refs"):
			return memerr.NonFastForward
		case containsAny(text,
			"could not resolve host", "connection refused", "connection timed out",
			"network is unreachable", "unable to access", "could not read from remote repository",
			"operation timed out", "temporary failure in name resolution", "no route to host",
			"connection reset", "ssl", "tls", "proxy"):
			return memerr.NetworkUnavailable
		default:
			return memerr.GitFailed
		}
	}
	var contractErr *memerr.Error
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	return memerr.GitFailed
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func failRemote(res Result, code memerr.Code, err error) (Result, error) {
	res.ReasonCode = string(code)
	switch code {
	case memerr.NetworkUnavailable, memerr.Timeout, memerr.NonFastForward:
		res.RemoteState = StatePending
	default:
		res.RemoteState = StateFailed
	}
	return res, err
}
