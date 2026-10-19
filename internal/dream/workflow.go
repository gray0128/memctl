package dream

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/gray0128/memctl/internal/assets"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
)

// This file implements the two workflow-facing entry points of
// docs/11-github-actions-dreaming.md section 2.5:
//
//   - `dream --init-workflow` writes the embedded Actions workflow into the
//     memory repository and commits it under the repository lock;
//   - `dream --worker` is the entry point the workflow itself calls.

// WorkflowRelativePath is the fixed managed workflow path. It is the only path
// under `.github` memctl ever writes, and it is written only here.
const WorkflowRelativePath = ".github/workflows/memctl-dream.yml"

// InitWorkflow installs or refreshes the managed workflow file. It is
// idempotent: an already current file produces no commit. The write bypasses the
// ordinary `.github` path boundary by design, because this is the single
// authorized exception of docs/11-github-actions-dreaming.md section 3.2.
//
// version is the running memctl version, which the workflow pins when it
// downloads the Linux binary. It is substituted into the embedded template so a
// freshly installed workflow always runs the release it was installed from.
func (s *Service) InitWorkflow(ctx context.Context, version string) (InitWorkflowResult, error) {
	if err := git.Available(); err != nil {
		return InitWorkflowResult{}, err
	}
	gitDir, err := git.GitDir(ctx, s.Source.RepoPath)
	if err != nil {
		return InitWorkflowResult{}, err
	}
	held, err := lock.Acquire(ctx, gitDir)
	if err != nil {
		return InitWorkflowResult{}, err
	}
	defer held.Release()
	ctx = git.WithInheritedFile(ctx, held.File())

	status, err := git.StatusPorcelain(ctx, s.Source.RepoPath)
	if err != nil {
		return InitWorkflowResult{}, err
	}
	if status != "" {
		return InitWorkflowResult{}, memerr.New(memerr.ExternalChanges,
			"the repository has staged, unstaged or untracked changes; commit them before installing the Dreaming workflow")
	}

	content := workflowContent(version)
	target := filepath.Join(s.Source.RepoPath, filepath.FromSlash(WorkflowRelativePath))
	existing, readErr := os.ReadFile(target)
	if readErr == nil && string(existing) == string(content) {
		return InitWorkflowResult{Path: WorkflowRelativePath, State: InitWorkflowUnchanged}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return InitWorkflowResult{}, memerr.Wrap(memerr.IOFailed, err,
			"cannot create the workflow directory")
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return InitWorkflowResult{}, memerr.Wrap(memerr.IOFailed, err,
			"cannot write %s", WorkflowRelativePath)
	}
	if err := git.RequireIdentity(ctx, s.Source.RepoPath); err != nil {
		return InitWorkflowResult{}, err
	}
	if err := git.Add(ctx, s.Source.RepoPath, WorkflowRelativePath); err != nil {
		return InitWorkflowResult{}, err
	}
	commit, err := git.Commit(ctx, s.Source.RepoPath,
		"memctl dream: install the Dreaming workflow\n\nThe workflow runs `memctl dream --worker` for a dream/req branch and publishes\nits result to dream/res. See docs/11-github-actions-dreaming.md.\n")
	if err != nil {
		return InitWorkflowResult{}, err
	}
	return InitWorkflowResult{Path: WorkflowRelativePath, State: InitWorkflowWritten, Commit: commit}, nil
}

// workflowVersionPlaceholder is replaced by the running memctl version.
const workflowVersionPlaceholder = "__MEMCTL_DREAM_VERSION__"

// workflowContent renders the embedded workflow with the running version.
func workflowContent(version string) []byte {
	version = strings.TrimSpace(version)
	if version == "" || version == "v2.0.0-dev" {
		// A development build has no release artifact to download. Keeping the
		// placeholder visible makes the missing pin obvious instead of writing
		// a version that does not exist.
		return assets.DreamWorkflow
	}
	return []byte(strings.ReplaceAll(string(assets.DreamWorkflow), workflowVersionPlaceholder, version))
}

// InitWorkflowResult is the outcome of `--init-workflow`.
type InitWorkflowResult struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	Commit string `json:"commit,omitempty"`
}

// Init workflow states.
const (
	InitWorkflowWritten   = "written"
	InitWorkflowUnchanged = "unchanged"
)

// Worker runs one GitHub Actions Dreaming job: it parses the request ref,
// executes the bounded multi-step worker against the checked-out baseline and
// publishes exactly one result commit. It returns an error only when the result
// could not be published at all; a failed organization run is reported through
// the result commit so the local side stops waiting immediately.
func (s *Service) Worker(ctx context.Context, requestRef, baseHead string) (WorkerRunResult, error) {
	if strings.TrimSpace(baseHead) == "" {
		baseHead = os.Getenv("GITHUB_SHA")
	}
	baseHead = strings.TrimSpace(baseHead)
	if baseHead == "" {
		return WorkerRunResult{}, memerr.New(memerr.InvalidArgument,
			"the worker needs --base-head or GITHUB_SHA")
	}
	ref, err := parseRequestRef(requestRef)
	if err != nil {
		return WorkerRunResult{}, err
	}
	remote, err := s.remote()
	if err != nil {
		return WorkerRunResult{}, err
	}
	head, err := git.Head(ctx, s.Source.RepoPath)
	if err != nil {
		return WorkerRunResult{}, err
	}
	if head != baseHead {
		return WorkerRunResult{}, memerr.New(memerr.RevisionMismatch,
			"the checked-out commit %s is not the requested baseline %s", head, baseHead)
	}

	provider, providerErr := s.providerForWorker(ref.Provider)
	meta := WorkerMeta{
		SchemaVersion: 1,
		ProposalID:    ref.ProposalID,
		BaseHead:      baseHead,
		Provider:      ref.Provider,
		Status:        WorkerStatusFailed,
		Warnings:      []string{},
	}
	if providerErr != nil {
		meta.Error = errorMessage(providerErr)
		return s.publishResult(ctx, remote, meta)
	}
	meta.Model = provider.Model()

	budgetCtx, cancel := context.WithTimeout(ctx, WorkerBudget)
	defer cancel()
	engine := &worker{Provider: provider, Now: s.timeNow}
	result, runErr := engine.run(budgetCtx, s.Source.RepoPath, baseHead)
	meta.Steps = result.Steps
	meta.Warnings = result.Warnings
	if runErr != nil {
		meta.Error = errorMessage(runErr)
		return s.publishResult(ctx, remote, meta)
	}
	// The result tree is complete, so materializing it also applies deletions.
	if err := s.writeResultTree(touchedPaths(baseTree(s.Source.RepoPath, baseHead), result.Tree), result.Tree); err != nil {
		meta.Error = errorMessage(err)
		return s.publishResult(ctx, remote, meta)
	}
	meta.Status = WorkerStatusReady
	return s.publishResult(ctx, remote, meta)
}

// WorkerRunResult reports what the worker published.
type WorkerRunResult struct {
	ProposalID string `json:"proposalId"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Status     string `json:"status"`
	Steps      int    `json:"steps"`
	Commit     string `json:"commit"`
	Ref        string `json:"ref"`
}

// baseTree reads the baseline tree and returns an empty map when it cannot be
// read; the worker has already verified the baseline by then.
func baseTree(repoPath, baseHead string) map[string][]byte {
	baseline, err := git.SourceFiles(context.Background(), repoPath, baseHead)
	if err != nil {
		return map[string][]byte{}
	}
	return baseline
}

// touchedPaths lists the repository paths the worker output adds or removes.
func touchedPaths(baseline map[string][]byte, files map[string][]byte) []string {
	var paths []string
	for path := range files {
		if protectedPath(path) {
			continue
		}
		paths = append(paths, path)
	}
	for path := range baseline {
		if protectedPath(path) {
			continue
		}
		if _, ok := files[path]; !ok {
			paths = append(paths, path)
		}
	}
	return paths
}

// writeResultTree applies the worker output to the checked-out work tree. Only
// the files the worker actually produced or removed are touched, so the
// checkout keeps everything else (including `.git`) untouched.
func (s *Service) writeResultTree(touched []string, files map[string][]byte) error {
	root := s.Source.RepoPath
	for _, path := range touched {
		if protectedPath(path) {
			return memerr.New(memerr.Unsupported,
				"the worker produced the protected path %q", path)
		}
		target := filepath.Join(root, filepath.FromSlash(path))
		content, keep := files[path]
		if !keep {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return memerr.Wrap(memerr.IOFailed, err, "cannot remove %s", path)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot create %s", filepath.Dir(target))
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return memerr.Wrap(memerr.IOFailed, err, "cannot write %s", path)
		}
	}
	return nil
}

// publishResult commits the work tree as one result commit whose parent is the
// baseline, then pushes it. It always creates a commit, even for a failed run,
// so the local side learns the reason immediately instead of timing out.
func (s *Service) publishResult(ctx context.Context, remote string, meta WorkerMeta) (WorkerRunResult, error) {
	if err := git.Add(ctx, s.Source.RepoPath, "."); err != nil {
		return WorkerRunResult{}, err
	}
	identity := []string{
		"GIT_AUTHOR_NAME=" + dreamWorkerName,
		"GIT_AUTHOR_EMAIL=" + dreamWorkerEmail,
		"GIT_COMMITTER_NAME=" + dreamWorkerName,
		"GIT_COMMITTER_EMAIL=" + dreamWorkerEmail,
	}
	commit, err := git.CommitWithEnv(ctx, s.Source.RepoPath, resultCommitMessage(meta), identity, true)
	if err != nil {
		return WorkerRunResult{}, err
	}
	ref := ResultRef(meta.ProposalID)
	if err := git.PushRef(ctx, s.Source.RepoPath, git.Options{NonInteractive: true}, remote, commit, ref); err != nil {
		return WorkerRunResult{}, remoteFailure(err, "cannot publish the Dreaming result branch")
	}
	return WorkerRunResult{
		ProposalID: meta.ProposalID,
		Provider:   meta.Provider,
		Model:      meta.Model,
		Status:     meta.Status,
		Steps:      meta.Steps,
		Commit:     commit,
		Ref:        ref,
	}, nil
}

// Worker identity used when the workflow did not configure one.
const (
	dreamWorkerName  = "memctl-dream[bot]"
	dreamWorkerEmail = "memctl-dream@users.noreply.github.com"
)

// resultCommitMessage renders the result commit title and metadata body.
func resultCommitMessage(meta WorkerMeta) string {
	meta.SchemaVersion = 1
	if meta.Warnings == nil {
		meta.Warnings = []string{}
	}
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		body = []byte(`{"schemaVersion":1,"status":"failed","error":"cannot encode the run metadata"}`)
	}
	return "dream(" + meta.ProposalID + "): " + meta.Status + "\n\n" + string(body) + "\n"
}
