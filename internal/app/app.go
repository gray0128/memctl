// Package app is the application service: the single entry point for
// repository mutations and reads. This file implements the local-only
// initialization (stage A-2) and file reads; it performs no network access.
package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// Initial entry file content. It mirrors the upstream format baseline:
// one bullet per entry, root-relative [[links]].
const initialMemoryContent = "# Memory\n\n## Index\n"

// Init states reported to the caller.
const (
	InitStateCreated   = "created"
	InitStateCompleted = "completed"
	InitStateExisting  = "existing"
)

// InitResult describes the repository after init, including idempotent runs.
type InitResult struct {
	RepoPath    string
	RepoID      string
	State       string
	Head        string
	Branch      string
	RemoteState string
}

// MutationEvent describes a managed transaction that has committed locally.
// It is an invalidation hint for the UI: the observer re-reads the snapshot
// instead of trusting the payload. See docs/02-domain-models-and-contracts.md
// section 5.
type MutationEvent struct {
	TransactionID string
	Revision      string
	Commit        string
	ChangedFiles  []string
}

// MutationObserver receives one event after a managed transaction commits
// locally. It must not block; the HTTP server publishes it to a bounded queue.
type MutationObserver func(MutationEvent)

// App binds the process to one repository and one state root.
type App struct {
	cfg config.Config
	// testFault injects interruption points in tests. It is always nil in
	// production builds.
	testFault func(stage string) error
	// backgroundSync enables best-effort background synchronization after a
	// managed write. It is off unless EnableBackgroundSync is called so unit
	// tests never re-execute the test binary.
	backgroundSync bool
	// mutationObserver is set by the UI server so a committed local write
	// invalidates the client view without waiting for the file watcher.
	mutationObserver MutationObserver
	// snapshotMu guards cachedSnapshot, the most recent complete snapshot. A
	// protected state serves it with stale: true instead of BUSY when one is
	// available. See docs/02-domain-models-and-contracts.md section 1.1.
	snapshotMu     sync.Mutex
	cachedSnapshot *Snapshot
}

// New builds the application service for the given configuration.
func New(cfg config.Config) *App { return &App{cfg: cfg} }

// EnableBackgroundSync turns on best-effort background synchronization after a
// managed write. The CLI enables it; direct library callers opt in explicitly.
func (a *App) EnableBackgroundSync() { a.backgroundSync = true }

// SetMutationObserver registers the single observer notified after a managed
// transaction commits locally. The UI server owns this slot; passing nil
// clears it. The observer must not block or mutate the repository.
func (a *App) SetMutationObserver(observer MutationObserver) { a.mutationObserver = observer }

// notifyMutation emits one invalidation hint for a committed local write. A
// failed or rolled-back transaction is never reported.
func (a *App) notifyMutation(result MutationResult) {
	if a == nil || a.mutationObserver == nil || result.Commit == "" {
		return
	}
	a.mutationObserver(MutationEvent{
		TransactionID: result.TransactionID,
		Revision:      result.Revision,
		Commit:        result.Commit,
		ChangedFiles:  append([]string(nil), result.ChangedFiles...),
	})
}

// RepoPath returns the configured repository path.
func (a *App) RepoPath() string { return a.cfg.RepoPath }

// Init creates or connects a memory repository at the configured path. With
// no URL it creates or reuses a local-only repository without touching files,
// config or remotes. With a URL it requires --confirm-private and follows the
// empty-remote, clone or connect-existing branch of the initialization
// protocol in docs/03-workflows-and-dataflows.md section 1.
func (a *App) Init(ctx context.Context, opts InitOptions) (InitResult, error) {
	if opts.RemoteURL != "" && !opts.ConfirmPrivate {
		return InitResult{}, memerr.New(memerr.InvalidArgument,
			"connecting a remote requires --confirm-private to confirm the remote is private and owned by you")
	}
	if opts.RemoteURL == "" && opts.ConfirmPrivate {
		return InitResult{}, memerr.New(memerr.InvalidArgument, "--confirm-private requires a remote URL")
	}
	if err := git.Available(); err != nil {
		return InitResult{}, err
	}

	repoPath := config.Canonical(a.cfg.RepoPath)
	info, statErr := os.Stat(repoPath)
	switch {
	case statErr == nil && !info.IsDir():
		return InitResult{}, memerr.New(memerr.InvalidArgument,
			"target path %s exists and is not a directory", repoPath)
	case statErr == nil:
		entries, err := os.ReadDir(repoPath)
		if err != nil {
			return InitResult{}, memerr.Wrap(memerr.IOFailed, err, "cannot inspect %s", repoPath)
		}
		if len(entries) > 0 {
			return a.initExisting(ctx, repoPath, opts)
		}
	case errors.Is(statErr, fs.ErrNotExist):
		// Nothing to inspect; the path is created below.
	default:
		return InitResult{}, memerr.Wrap(memerr.IOFailed, statErr, "cannot inspect %s", repoPath)
	}

	if err := ensureOutsideRepositories(repoPath); err != nil {
		return InitResult{}, err
	}
	createdDir := errors.Is(statErr, fs.ErrNotExist)
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		return InitResult{}, memerr.Wrap(memerr.IOFailed, err, "cannot create %s", repoPath)
	}

	if opts.RemoteURL != "" {
		return a.initWithNewRemote(ctx, repoPath, createdDir, opts)
	}

	if err := git.Init(ctx, repoPath); err != nil {
		return InitResult{}, err
	}
	head, err := bootstrapFirstCommit(ctx, repoPath)
	if err != nil {
		return InitResult{}, err
	}
	result := a.result(repoPath, InitStateCreated, head)
	result.RemoteState = "local_only"
	return result, nil
}

// ReadFile reads one managed text file and returns its revision baseline. It
// holds the repository lock without waiting and refuses to read while a
// transaction, a conflict or a Git intermediate state is active, so the
// returned content is never a transaction-intermediate tree.
func (a *App) ReadFile(ctx context.Context, relPath string) (vault.FileRead, error) {
	session, err := a.beginRead(ctx)
	if err != nil {
		return vault.FileRead{}, err
	}
	defer session.Close()
	return session.root.ReadFile(relPath)
}

// initExisting handles a non-empty target directory: an existing memory
// repository is reused, anything else is refused without being modified.
func (a *App) initExisting(ctx context.Context, repoPath string, opts InitOptions) (InitResult, error) {
	topLevel, err := git.TopLevel(ctx, repoPath)
	if err != nil || config.Canonical(topLevel) != repoPath {
		return InitResult{}, memerr.New(memerr.InvalidArgument,
			"target path %s is not empty and is not a memory repository root; memctl does not take over existing directories", repoPath)
	}

	memoryPath := filepath.Join(repoPath, vault.EntryPointName)
	info, err := os.Lstat(memoryPath)
	if errors.Is(err, fs.ErrNotExist) {
		return InitResult{}, memerr.New(memerr.InvalidArgument,
			"repository at %s has no %s; refusing to adopt an unrelated Git repository", repoPath, vault.EntryPointName)
	}
	if err != nil {
		return InitResult{}, memerr.Wrap(memerr.IOFailed, err, "cannot access %s", memoryPath)
	}
	if !info.Mode().IsRegular() {
		return InitResult{}, memerr.New(memerr.InvalidArgument,
			"%s is not a regular file", memoryPath)
	}

	if git.UnbornHead(ctx, repoPath) {
		head, err := finishFirstCommit(ctx, repoPath)
		if err != nil {
			return InitResult{}, err
		}
		if opts.RemoteURL != "" {
			return connectExistingRemote(ctx, repoPath, opts)
		}
		result := a.result(repoPath, InitStateCompleted, head)
		result.RemoteState = "local_only"
		return result, nil
	}

	if opts.RemoteURL != "" {
		return connectExistingRemote(ctx, repoPath, opts)
	}

	head, err := git.Head(ctx, repoPath)
	if err != nil {
		return InitResult{}, err
	}
	result := a.result(repoPath, InitStateExisting, head)
	result.Branch = branchName(ctx, repoPath)
	if remotes, err := git.Remotes(ctx, repoPath); err == nil && len(remotes) > 0 {
		result.RemoteState = "existing"
	} else {
		result.RemoteState = "local_only"
	}
	return result, nil
}

// requireRepo validates the documented repository identity: the configured
// path is the Git working-tree root and contains the entry file.
func (a *App) requireRepo(ctx context.Context) (string, error) {
	repoPath, err := a.requireRepoRoot(ctx)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(filepath.Join(repoPath, vault.EntryPointName)); err != nil {
		return "", memerr.New(memerr.NoRepo, "%s has no %s; run memctl init", repoPath, vault.EntryPointName)
	}
	return repoPath, nil
}

// requireRepoRoot validates that the configured path is a Git working-tree
// root without requiring the entry file. Recovery uses it because an
// interrupted transaction may have deleted the entry file.
func (a *App) requireRepoRoot(ctx context.Context) (string, error) {
	repoPath := config.Canonical(a.cfg.RepoPath)
	info, err := os.Stat(repoPath)
	if err != nil || !info.IsDir() {
		return "", memerr.New(memerr.NoRepo, "memory repository not found at %s; run memctl init", repoPath)
	}
	if err := git.Available(); err != nil {
		return "", err
	}
	topLevel, err := git.TopLevel(ctx, repoPath)
	if err != nil || config.Canonical(topLevel) != repoPath {
		return "", memerr.New(memerr.NoRepo, "%s is not a Git working-tree root", repoPath)
	}
	return repoPath, nil
}

func (a *App) result(repoPath, state, head string) InitResult {
	return InitResult{
		RepoPath: repoPath,
		RepoID:   config.RepoID(repoPath),
		State:    state,
		Head:     head,
	}
}

// bootstrapFirstCommit creates the entry file and the first commit of a new
// repository.
func bootstrapFirstCommit(ctx context.Context, repoPath string) (string, error) {
	memoryPath := filepath.Join(repoPath, vault.EntryPointName)
	if err := os.WriteFile(memoryPath, []byte(initialMemoryContent), 0o644); err != nil {
		return "", memerr.Wrap(memerr.IOFailed, err, "cannot create %s", memoryPath)
	}
	return finishFirstCommit(ctx, repoPath)
}

// finishFirstCommit commits only the entry file. A missing Git identity is
// reported explicitly; memctl never writes global Git configuration.
func finishFirstCommit(ctx context.Context, repoPath string) (string, error) {
	if err := git.RequireIdentity(ctx, repoPath); err != nil {
		return "", err
	}
	if err := git.Add(ctx, repoPath, vault.EntryPointName); err != nil {
		return "", err
	}
	return git.Commit(ctx, repoPath, "Initialize memory repository")
}

// ensureOutsideRepositories refuses nested repositories, which are not valid
// memory roots and would silently capture the user's project history.
func ensureOutsideRepositories(repoPath string) error {
	for dir := filepath.Dir(repoPath); ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return memerr.New(memerr.InvalidArgument,
				"target path %s is inside the Git repository at %s; choose a path outside any repository", repoPath, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
