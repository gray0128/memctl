package dream

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
)

// This file implements the Git-ref transport of docs/11-github-actions-dreaming.md
// section 2.2: memctl and the GitHub Actions worker exchange only branch refs
// and commit objects, never a GitHub API token.
//
//   - the local side pushes the fixed baseline to
//     refs/heads/dream/req/<proposalId>/<provider>;
//   - the worker pushes its result to refs/heads/dream/res/<proposalId>;
//   - the local side fetches that one ref into refs/memctl/dream/<proposalId>,
//     verifies the parent commit, and deletes both refs afterwards.

// RequestRef is the branch name that triggers the Actions workflow.
func RequestRef(proposalID, provider string) string {
	return "refs/heads/dream/req/" + proposalID + "/" + provider
}

// ResultRef is the branch name the worker pushes its result to.
func ResultRef(proposalID string) string {
	return "refs/heads/dream/res/" + proposalID
}

// LocalResultRef is the private local ref one fetched result is fetched into.
func LocalResultRef(proposalID string) string {
	return "refs/memctl/dream/" + proposalID
}

// WorkerMeta is the structured run metadata carried in the result commit body.
// It is display and diagnosis data: the local side still recomputes every range
// check from the result tree instead of trusting Status.
type WorkerMeta struct {
	SchemaVersion int      `json:"schemaVersion"`
	ProposalID    string   `json:"proposalId"`
	BaseHead      string   `json:"baseHead"`
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	Status        string   `json:"status"`
	Steps         int      `json:"steps"`
	Warnings      []string `json:"warnings"`
	Error         string   `json:"error"`
}

// Worker meta status values.
const (
	WorkerStatusReady  = "ready"
	WorkerStatusFailed = "failed"
)

// Dreaming branch names never become user-visible branch names.
const (
	requestRefPrefix = "refs/heads/dream/req/"
	resultRefPrefix  = "refs/heads/dream/res/"
)

// ResultCommitParents is the documented single-parent rule.
const ResultCommitSingleParent = 1

// resultRecord is one fetched worker result.
type resultRecord struct {
	Commit string
	Meta   WorkerMeta
	// Files is the result tree restricted to regular non-`.git`/`.github`
	// files. Symbolic links and submodules make ParseResult fail instead of
	// being silently dropped.
	Files map[string]workFile
}

// pushRequest publishes the fixed baseline as the request branch. The ref
// points directly at an existing local commit, so no extra commit object is
// created and the worker checks out exactly the reviewed baseline.
func (s *Service) pushRequest(ctx context.Context, proposalID, provider, baseHead string) error {
	remote, err := s.remote()
	if err != nil {
		return err
	}
	if err := git.PushRef(ctx, s.Source.RepoPath, git.Options{Interactive: s.Interactive}, remote, baseHead, RequestRef(proposalID, provider)); err != nil {
		return remoteFailure(err, "cannot push the Dreaming request branch")
	}
	return nil
}

// remote resolves the confirmed private remote.
func (s *Service) remote() (string, error) {
	name, ok, err := git.DefaultRemote(context.Background(), s.Source.RepoPath)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", memerr.New(memerr.Unsupported,
			"the repository has no remote; Dreaming runs on GitHub Actions, so configure a private GitHub remote first")
	}
	return name, nil
}

// fetchResult looks for the worker result branch exactly once. It reports
// found=false when the worker has not published a result yet.
func (s *Service) fetchResult(ctx context.Context, proposalID string) (resultRecord, bool, error) {
	remote, err := s.remote()
	if err != nil {
		return resultRecord{}, false, err
	}
	ref := ResultRef(proposalID)
	listing, err := git.LsRemote(ctx, s.Source.RepoPath, git.Options{NonInteractive: true}, remote)
	if err != nil {
		return resultRecord{}, false, remoteFailure(err, "cannot read the Dreaming result branch")
	}
	if !advertisesRef(listing, ref) {
		return resultRecord{}, false, nil
	}
	local := LocalResultRef(proposalID)
	if err := git.FetchRef(ctx, s.Source.RepoPath, git.Options{NonInteractive: true}, remote, ref, local); err != nil {
		return resultRecord{}, false, remoteFailure(err, "cannot fetch the Dreaming result branch")
	}
	commit, err := git.RevParse(ctx, s.Source.RepoPath, local)
	if err != nil {
		return resultRecord{}, false, err
	}
	parents, err := git.CommitParents(ctx, s.Source.RepoPath, commit)
	if err != nil {
		return resultRecord{}, false, err
	}
	if len(parents) != ResultCommitSingleParent {
		return resultRecord{}, false, memerr.New(memerr.Unsupported,
			"the Dreaming result commit %s has %d parents; exactly one parent is required", commit, len(parents))
	}
	message, err := git.CommitMessageOf(ctx, s.Source.RepoPath, commit)
	if err != nil {
		return resultRecord{}, false, err
	}
	meta, err := parseWorkerMeta(message)
	if err != nil {
		return resultRecord{}, false, err
	}
	record := resultRecord{Commit: commit, Meta: meta}
	if meta.Status != WorkerStatusReady {
		return record, true, nil
	}
	files, err := s.resultTree(ctx, commit, meta.BaseHead)
	if err != nil {
		return resultRecord{}, false, err
	}
	record.Files = files
	return record, true, nil
}

// resultTree materializes the result commit as an in-memory file set. The tree
// never touches the local disk: untrusted content parsed in memory cannot
// become a symlink, a hard link or a special file on the user's machine.
//
// Protected paths (`.git`, `.github`, host configuration) are not memory
// content and are not part of the proposal. A result that leaves them exactly
// as they were in the baseline is accepted and the entries are dropped; a
// result that adds or changes one is refused, so a tampered result branch can
// never smuggle a workflow change into a local apply.
func (s *Service) resultTree(ctx context.Context, commit, baseHead string) (map[string]workFile, error) {
	out, err := git.SourceFiles(ctx, s.Source.RepoPath, commit)
	if err != nil {
		return nil, err
	}
	var baseline map[string][]byte
	files := make(map[string]workFile, len(out))
	for path, content := range out {
		if protectedPath(path) {
			if baseline == nil {
				if baseline, err = git.SourceFiles(ctx, s.Source.RepoPath, baseHead); err != nil {
					return nil, err
				}
			}
			if previous, ok := baseline[path]; ok && string(previous) == string(content) {
				continue
			}
			return nil, memerr.New(memerr.Unsupported,
				"the Dreaming result modifies the protected path %q", path)
		}
		if !utf8Text(content) {
			return nil, memerr.New(memerr.Unsupported,
				"the Dreaming result contains binary content at %q", path)
		}
		files[path] = workFile{content: content, fsMode: 0o644}
	}
	return files, nil
}

// parseWorkerMeta extracts the JSON metadata block from a result commit body.
func parseWorkerMeta(message string) (WorkerMeta, error) {
	start := strings.Index(message, "{")
	end := strings.LastIndex(message, "}")
	if start < 0 || end <= start {
		return WorkerMeta{}, memerr.New(memerr.Unsupported,
			"the Dreaming result commit has no structured run metadata")
	}
	var meta WorkerMeta
	if err := json.Unmarshal([]byte(message[start:end+1]), &meta); err != nil {
		return WorkerMeta{}, memerr.Wrap(memerr.Unsupported, err,
			"the Dreaming result commit metadata is unreadable")
	}
	if meta.SchemaVersion != 1 {
		return WorkerMeta{}, memerr.New(memerr.Unsupported,
			"the Dreaming result metadata uses the unsupported schema version %d", meta.SchemaVersion)
	}
	return meta, nil
}

// advertisesRef reports whether an ls-remote listing contains one ref.
func advertisesRef(listing, ref string) bool {
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return true
		}
	}
	return false
}

// cleanupRefs removes the temporary refs of one finished proposal. It is
// best-effort: a failed cleanup never changes the proposal status.
func (s *Service) cleanupRefs(ctx context.Context, proposalID, provider string) {
	_ = git.UpdateRef(ctx, s.Source.RepoPath, LocalResultRef(proposalID), "")
	if s.KeepRemoteRefs {
		return
	}
	remote, err := s.remote()
	if err != nil {
		return
	}
	opts := git.Options{NonInteractive: true}
	_ = git.DeleteRemoteRef(ctx, s.Source.RepoPath, opts, remote, RequestRef(proposalID, provider))
	_ = git.DeleteRemoteRef(ctx, s.Source.RepoPath, opts, remote, ResultRef(proposalID))
}

// remoteFailure maps one Git transport failure to a stable remote code.
func remoteFailure(err error, message string) error {
	code := memerr.GitFailed
	if contract, ok := memerr.As(err); ok {
		code = contract.Code
	}
	return memerr.Wrap(code, err, "%s", message)
}

// pollLoop waits for the worker result of one proposal.
func (s *Service) pollLoop(ctx context.Context, proposalID string) (resultRecord, error) {
	interval := s.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	deadline := s.timeNow().Add(s.timeout())
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return resultRecord{}, memerr.Wrap(memerr.Timeout, ctx.Err(),
				"stopped waiting for the Dreaming result")
		case <-timer.C:
		}
		record, found, err := s.fetchResult(ctx, proposalID)
		if err != nil {
			return resultRecord{}, err
		}
		if found {
			return record, nil
		}
		if s.timeNow().After(deadline) {
			return resultRecord{}, memerr.New(memerr.Timeout,
				"the GitHub Actions worker did not publish a result within %s; check the workflow run", s.timeout())
		}
		timer.Reset(interval)
	}
}

// DefaultPollInterval is the documented result poll interval.
const DefaultPollInterval = 5 * time.Second

// workerRefState is the parsed form of one request branch.
type workerRefState struct {
	ProposalID string
	Provider   string
}

// parseRequestRef splits refs/heads/dream/req/<proposalId>/<provider>.
func parseRequestRef(ref string) (workerRefState, error) {
	trimmed := strings.TrimPrefix(ref, requestRefPrefix)
	if trimmed == ref {
		return workerRefState{}, memerr.New(memerr.InvalidArgument,
			"the worker ref must be %s<proposalId>/<provider>", requestRefPrefix)
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || !proposalIDPattern.MatchString(parts[0]) {
		return workerRefState{}, memerr.New(memerr.InvalidArgument,
			"the worker ref %q does not name a proposal and a provider", ref)
	}
	if !KnownProvider(parts[1]) {
		return workerRefState{}, memerr.New(memerr.Unsupported,
			"provider %q is not built in; supported providers are %s", parts[1], strings.Join(ProviderIDs, ", "))
	}
	return workerRefState{ProposalID: parts[0], Provider: parts[1]}, nil
}
