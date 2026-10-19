package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/vault"
)

// TestRevertSingleParentCommit covers the success path of C05: a single-parent
// commit is reverted into a new commit and the earlier content is restored.
func TestRevertSingleParentCommit(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- v1\n", vault.AbsentRevision)
	reverted := applyUpdate(t, service, "alpha.md", "# Alpha\n\n- v2\n")

	revision := snapshotRevision(t, service)
	result, err := service.Revert(ctx, reverted.Commit, revision, "dsh", "sid-1", "laptop")
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if result.Commit == "" || result.Commit == reverted.Commit {
		t.Fatalf("revert did not create a new commit: %+v", result)
	}
	// The revert commit keeps Git's own message and gains the same provenance
	// trailers as a managed write, so the audit view can resolve its source.
	message := runGit(t, repo, "log", "-1", "--format=%B", result.Commit)
	for _, want := range []string{"This reverts commit", "Agent: dsh", "Session: sid-1", "Device: laptop"} {
		if !strings.Contains(message, want) {
			t.Fatalf("revert message %q is missing %q", message, want)
		}
	}
	if got := runGit(t, repo, "rev-parse", "HEAD"); got != result.Commit {
		t.Fatalf("revert result commit %s is not HEAD %s", result.Commit, got)
	}
	if !containsString(result.ChangedFiles, "alpha.md") {
		t.Fatalf("changed files = %v, want alpha.md", result.ChangedFiles)
	}
	if got := readTestFile(t, repo+"/alpha.md"); got != "# Alpha\n\n- v1\n" {
		t.Fatalf("reverted content = %q", got)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the revert: %q", status)
	}
	if _, has, err := readOperationRecord(t, repo); err != nil || has {
		t.Fatalf("revert left an operation record (has=%v err=%v)", has, err)
	}
}

// TestRevertRejectsRootAndMerge covers the unsupported objects of C05.
func TestRevertRejectsRootAndMerge(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	root := runGit(t, repo, "rev-list", "--max-parents=0", "HEAD")
	_, err := service.Revert(ctx, root, snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.Unsupported)

	// A merge commit has two parents and cannot be reverted without guessing a
	// mainline.
	runGit(t, repo, "checkout", "-q", "-b", "side")
	applyPut(t, service, repo, "side.md", "# Side\n\n- side\n", vault.AbsentRevision)
	runGit(t, repo, "checkout", "-q", "main")
	applyPut(t, service, repo, "main.md", "# Main\n\n- main\n", vault.AbsentRevision)
	runGit(t, repo, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	merge := runGit(t, repo, "rev-parse", "HEAD")
	if parents := runGit(t, repo, "rev-list", "--parents", "-n", "1", merge); len(strings.Fields(parents)) != 3 {
		t.Fatalf("fixture merge commit parents = %q", parents)
	}
	_, err = service.Revert(ctx, merge, snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.Unsupported)
}

// TestRevertRejectsUnknownAndUnreachable covers the NOT_FOUND and
// INVALID_ARGUMENT preconditions of the revert route.
func TestRevertRejectsUnknownAndUnreachable(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	_, err := service.Revert(ctx, strings.Repeat("f", 40), snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.NotFound)

	_, err = service.Revert(ctx, "not-a-hash", snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.InvalidArgument)

	runGit(t, repo, "checkout", "-q", "-b", "orphan")
	orphan := applyPut(t, service, repo, "orphan.md", "# Orphan\n\n- orphan\n", vault.AbsentRevision)
	runGit(t, repo, "checkout", "-q", "main")
	_, err = service.Revert(ctx, orphan.Commit, snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.NotFound)

	if _, err := service.Revert(ctx, orphan.Commit, "", "", "", ""); err == nil {
		t.Fatal("revert without a baseRevision succeeded")
	} else {
		requireContractCode(t, err, memerr.PreconditionRequired)
	}
}

// TestRevertConflictAbortsAndRestores covers the revert half of C05: a
// conflicting revert is diagnosed, aborted and leaves the original history,
// worktree and ownership record untouched.
func TestRevertConflictAbortsAndRestores(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- v1\n", vault.AbsentRevision)
	reverted := applyUpdate(t, service, "alpha.md", "# Alpha\n\n- v2\n")
	head := applyUpdate(t, service, "alpha.md", "# Alpha\n\n- v3\n")

	_, err := service.Revert(ctx, reverted.Commit, snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.ContentConflict)
	contractErr, _ := memerr.As(err)
	if !strings.Contains(contractErr.Message, "alpha.md") || !strings.Contains(contractErr.Message, "text") {
		t.Fatalf("conflict message lost the diagnosed path: %q", contractErr.Message)
	}

	if current := runGit(t, repo, "rev-parse", "HEAD"); current != head.Commit {
		t.Fatalf("conflicting revert moved HEAD: %s -> %s", head.Commit, current)
	}
	if got := readTestFile(t, repo+"/alpha.md"); got != "# Alpha\n\n- v3\n" {
		t.Fatalf("conflicting revert rewrote content: %q", got)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the aborted revert: %q", status)
	}
	if out, err := gitCommand(t, repo, "", "rev-parse", "--verify", "--quiet", "REVERT_HEAD"); err == nil {
		t.Fatalf("REVERT_HEAD survived the abort: %q", out)
	}
	if _, has, err := readOperationRecord(t, repo); err != nil || has {
		t.Fatalf("aborted revert left an operation record (has=%v err=%v)", has, err)
	}
	// No residual revert state may block the next managed write.
	applyPut(t, service, repo, "after.md", "# After\n\n- ok\n", vault.AbsentRevision)
}

// applyUpdate replaces an existing managed file using its current revision.
func applyUpdate(t *testing.T, service *App, path, content string) MutationResult {
	t.Helper()
	file, err := service.ReadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return applyPut(t, service, service.RepoPath(), path, content, file.FileRevision)
}

// readOperationRecord reads the persisted operation record for a repository.
func readOperationRecord(t *testing.T, repo string) (state.Operation, bool, error) {
	t.Helper()
	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	return state.OpenStore(gitDir).LoadOperation()
}

// TestRevertRecoveryFailureKeepsOwnership covers docs/03 section 5.3: when the
// abort or the recovery verification cannot be proven, the revert ownership
// record must survive so doctor and recover can explain the leftover Git state.
func TestRevertRecoveryFailureKeepsOwnership(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- v1\n", vault.AbsentRevision)
	reverted := applyUpdate(t, service, "alpha.md", "# Alpha\n\n- v2\n")
	applyUpdate(t, service, "alpha.md", "# Alpha\n\n- v3\n")

	service.testFault = func(stage string) error {
		if stage == "revert-abort" {
			return errors.New("simulated abort failure")
		}
		return nil
	}
	_, err := service.Revert(ctx, reverted.Commit, snapshotRevision(t, service), "", "", "")
	service.testFault = nil
	requireContractCode(t, err, memerr.RecoveryRequired)

	record, has, err := readOperationRecord(t, repo)
	if err != nil {
		t.Fatalf("read operation record: %v", err)
	}
	if !has {
		t.Fatal("a recovery failure deleted the revert ownership record")
	}
	if record.Kind != state.KindRevert || record.Owner != state.OwnerMemctl || record.OriginalHead == "" {
		t.Fatalf("unexpected operation record: %+v", record)
	}
}

// TestRevertRejectsUnrelatedHistory covers NOT_FOUND for a commit whose history
// shares no ancestor with HEAD: Git answers "no common ancestor" instead of a
// merge base, and that must not surface as a Git failure.
func TestRevertRejectsUnrelatedHistory(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	applyPut(t, service, repo, "alpha.md", "# Alpha\n\n- v1\n", vault.AbsentRevision)

	runGit(t, repo, "checkout", "-q", "--orphan", "unrelated")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "unrelated root")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "unrelated child")
	unrelated := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "checkout", "-q", "-f", "main")

	if out, err := gitCommand(t, repo, "", "merge-base", unrelated, "HEAD"); err == nil {
		t.Fatalf("fixture histories unexpectedly share %q", out)
	}

	_, err := service.Revert(ctx, unrelated, snapshotRevision(t, service), "", "", "")
	requireContractCode(t, err, memerr.NotFound)
}
