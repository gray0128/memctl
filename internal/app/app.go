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
	RepoPath string
	RepoID   string
	State    string
	Head     string
}

// App binds the process to one repository and one state root.
type App struct {
	cfg config.Config
}

// New builds the application service for the given configuration.
func New(cfg config.Config) *App { return &App{cfg: cfg} }

// RepoPath returns the configured repository path.
func (a *App) RepoPath() string { return a.cfg.RepoPath }

// Init creates a memory repository at the configured path, or reports the
// existing one without modifying files, config or remotes. Remote connection
// is not implemented in stage A-2.
func (a *App) Init(ctx context.Context) (InitResult, error) {
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
			return a.initExisting(ctx, repoPath)
		}
	case errors.Is(statErr, fs.ErrNotExist):
		// Nothing to inspect; the path is created below.
	default:
		return InitResult{}, memerr.Wrap(memerr.IOFailed, statErr, "cannot inspect %s", repoPath)
	}

	if err := ensureOutsideRepositories(repoPath); err != nil {
		return InitResult{}, err
	}
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		return InitResult{}, memerr.Wrap(memerr.IOFailed, err, "cannot create %s", repoPath)
	}
	if err := git.Init(ctx, repoPath); err != nil {
		return InitResult{}, err
	}
	head, err := bootstrapFirstCommit(ctx, repoPath)
	if err != nil {
		return InitResult{}, err
	}
	return a.result(repoPath, InitStateCreated, head), nil
}

// ReadFile reads one managed text file and returns its revision baseline.
func (a *App) ReadFile(ctx context.Context, relPath string) (vault.FileRead, error) {
	repoPath, err := a.requireRepo(ctx)
	if err != nil {
		return vault.FileRead{}, err
	}
	root, err := vault.OpenRoot(repoPath)
	if err != nil {
		return vault.FileRead{}, err
	}
	defer root.Close()
	return root.ReadFile(relPath)
}

// initExisting handles a non-empty target directory: an existing memory
// repository is reused, anything else is refused without being modified.
func (a *App) initExisting(ctx context.Context, repoPath string) (InitResult, error) {
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

	if !git.UnbornHead(ctx, repoPath) {
		head, err := git.Head(ctx, repoPath)
		if err != nil {
			return InitResult{}, err
		}
		return a.result(repoPath, InitStateExisting, head), nil
	}

	head, err := finishFirstCommit(ctx, repoPath)
	if err != nil {
		return InitResult{}, err
	}
	return a.result(repoPath, InitStateCompleted, head), nil
}

// requireRepo validates the documented repository identity: the configured
// path is the Git working-tree root and contains the entry file.
func (a *App) requireRepo(ctx context.Context) (string, error) {
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
	if _, err := os.Lstat(filepath.Join(repoPath, vault.EntryPointName)); err != nil {
		return "", memerr.New(memerr.NoRepo, "%s has no %s; run memctl init", repoPath, vault.EntryPointName)
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
