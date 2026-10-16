package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
	"github.com/gray0128/memctl/internal/vault"
)

// defaultRemoteName is the remote memctl configures and confirms.
const defaultRemoteName = "origin"

// Init options for remote connection.
type InitOptions struct {
	// RemoteURL connects an existing or empty repository to a private remote.
	RemoteURL string
	// ConfirmPrivate is the user's explicit statement that the remote is
	// private and owned by the user. It is required with RemoteURL.
	ConfirmPrivate bool
	// Interactive allows Git and SSH native prompts during remote access.
	Interactive bool
}

// initWithNewRemote creates or clones the repository behind the given URL.
// An empty remote receives a local first commit and the first push; a remote
// with history is cloned. See docs/03-workflows-and-dataflows.md section 1.
func (a *App) initWithNewRemote(ctx context.Context, repoPath string, opts InitOptions) (InitResult, error) {
	advertised, err := git.LsRemote(ctx, repoPath, git.Options{Interactive: opts.Interactive}, opts.RemoteURL)
	if err != nil {
		_ = os.Remove(repoPath)
		return InitResult{RepoPath: repoPath}, remoteFailure(err)
	}

	if strings.TrimSpace(advertised) != "" {
		if err := os.Remove(repoPath); err != nil {
			return InitResult{RepoPath: repoPath}, memerr.Wrap(memerr.IOFailed, err, "cannot prepare the clone target %s", repoPath)
		}
		if err := git.Clone(ctx, git.Options{Interactive: opts.Interactive}, opts.RemoteURL, repoPath); err != nil {
			return InitResult{RepoPath: repoPath}, remoteFailure(err)
		}
		if err := verifyEntryFile(repoPath); err != nil {
			return InitResult{RepoPath: repoPath}, err
		}
		if err := recordApproval(ctx, repoPath); err != nil {
			return InitResult{RepoPath: repoPath}, err
		}
		head, err := git.Head(ctx, repoPath)
		if err != nil {
			return InitResult{RepoPath: repoPath}, err
		}
		result := a.result(repoPath, "cloned", head)
		result.RemoteState = "synced"
		return result, nil
	}

	if err := git.Init(ctx, repoPath); err != nil {
		return InitResult{RepoPath: repoPath}, err
	}
	head, err := bootstrapFirstCommit(ctx, repoPath)
	if err != nil {
		return InitResult{}, err
	}
	if err := git.AddRemote(ctx, repoPath, defaultRemoteName, opts.RemoteURL); err != nil {
		return InitResult{RepoPath: repoPath}, err
	}
	if err := recordApproval(ctx, repoPath); err != nil {
		return InitResult{RepoPath: repoPath}, err
	}
	result := a.result(repoPath, InitStateCreated, head)
	if err := git.Push(ctx, repoPath, git.Options{Interactive: opts.Interactive}, defaultRemoteName, "main", true); err != nil {
		result.RemoteState = "pending"
		return result, remoteFailure(err)
	}
	result.RemoteState = "synced"
	return result, nil
}

// connectExistingRemote records an approval for an existing URL or attaches a
// new remote to an existing local repository. It never repoints a remote whose
// URL differs from the requested one.
func connectExistingRemote(ctx context.Context, repoPath string, opts InitOptions) (InitResult, error) {
	result, err := existingResult(ctx, repoPath)
	if err != nil {
		return result, err
	}

	remotes, err := git.Remotes(ctx, repoPath)
	if err != nil {
		return result, err
	}

	if len(remotes) > 0 {
		urls, err := git.RemoteURLs(ctx, repoPath, remotes[0])
		if err != nil {
			return result, err
		}
		if !containsString(urls, opts.RemoteURL) {
			return result, memerr.New(memerr.InvalidArgument,
				"the repository already has remote %s with a different URL; change the Git remote explicitly, then re-run init %s --confirm-private",
				remotes[0], opts.RemoteURL)
		}
		if err := recordApproval(ctx, repoPath); err != nil {
			return result, err
		}
		result.RemoteState = "existing"
		return result, nil
	}

	advertised, err := git.LsRemote(ctx, repoPath, git.Options{Interactive: opts.Interactive}, opts.RemoteURL)
	if err != nil {
		return result, remoteFailure(err)
	}
	if err := git.AddRemote(ctx, repoPath, defaultRemoteName, opts.RemoteURL); err != nil {
		return result, err
	}
	if err := recordApproval(ctx, repoPath); err != nil {
		return result, err
	}

	if strings.TrimSpace(advertised) == "" {
		if err := git.Push(ctx, repoPath, git.Options{Interactive: opts.Interactive}, defaultRemoteName, result.branch(), true); err != nil {
			result.RemoteState = "pending"
			return result, remoteFailure(err)
		}
		result.RemoteState = "synced"
		return result, nil
	}

	if err := git.Fetch(ctx, repoPath, git.Options{Interactive: opts.Interactive}, defaultRemoteName); err != nil {
		return result, remoteFailure(err)
	}
	defaultBranch, err := git.RemoteDefaultBranch(ctx, repoPath, git.Options{Interactive: opts.Interactive}, defaultRemoteName)
	if err != nil {
		return result, err
	}
	remoteRef := defaultRemoteName + "/" + defaultBranch
	if _, err := git.MergeBase(ctx, repoPath, "HEAD", remoteRef); err != nil {
		return result, memerr.New(memerr.NoUpstream,
			"%s has no common history with this repository; connecting it requires manual preparation", remoteRef)
	}
	if _, err := git.Run(ctx, repoPath, "branch", "--set-upstream-to", remoteRef, result.branch()); err != nil {
		return result, err
	}
	result.RemoteState = "synced"
	return result, nil
}

func (r InitResult) branch() string {
	if r.Branch != "" {
		return r.Branch
	}
	return "main"
}

func existingResult(ctx context.Context, repoPath string) (InitResult, error) {
	head, err := git.Head(ctx, repoPath)
	if err != nil {
		return InitResult{RepoPath: repoPath}, err
	}
	return InitResult{
		RepoPath: repoPath,
		RepoID:   config.RepoID(repoPath),
		State:    InitStateExisting,
		Head:     head,
		Branch:   branchName(ctx, repoPath),
	}, nil
}

func branchName(ctx context.Context, repoPath string) string {
	if branch, err := git.HeadBranch(ctx, repoPath); err == nil {
		return branch
	}
	return ""
}

// verifyEntryFile refuses a clone that lacks the memory entry point.
func verifyEntryFile(repoPath string) error {
	info, err := os.Lstat(filepath.Join(repoPath, vault.EntryPointName))
	if err != nil {
		return memerr.New(memerr.NoRepo,
			"the cloned repository has no %s; prepare it manually before connecting memctl", vault.EntryPointName)
	}
	if !info.Mode().IsRegular() {
		return memerr.New(memerr.NoRepo, "%s is not a regular file", vault.EntryPointName)
	}
	return nil
}

// recordApproval stores the confirmed fetch/push target set. Changing a
// remote URL later invalidates it.
func recordApproval(ctx context.Context, repoPath string) error {
	gitDir, err := git.GitDir(ctx, repoPath)
	if err != nil {
		return err
	}
	urls, err := git.RemoteURLs(ctx, repoPath, defaultRemoteName)
	if err != nil {
		return err
	}
	return state.OpenStore(gitDir).SaveApproval(state.Approval{URLs: urls, ConfirmedAt: state.Now()})
}

// remoteFailure maps a Git error to a stable remote code and message.
func remoteFailure(err error) error {
	code := syncer.Classify(err)
	message := "remote access failed"
	if contractErr, ok := memerr.As(err); ok {
		message = contractErr.Message
	}
	return memerr.New(code, "cannot reach the remote: %s", message)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
