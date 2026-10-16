package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/vault"
)

const testInitialMemory = "# Memory\n\n## Index\n"

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "system-gitconfig"))
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func gitCommand(t *testing.T, dir string, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCommand(t, dir, "", args...)
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return out
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// newTestService builds a committed local repository with MEMORY.md and
// returns the application service bound to it.
func newTestService(t *testing.T) (*App, string) {
	t.Helper()
	isolateGit(t)
	repo := t.TempDir()
	ctx := context.Background()
	if err := git.Init(ctx, repo); err != nil {
		t.Fatalf("git init: %v", err)
	}
	runGit(t, repo, "config", "user.name", "Memctl Test")
	runGit(t, repo, "config", "user.email", "memctl-test@example.invalid")
	writeTestFile(t, filepath.Join(repo, "MEMORY.md"), testInitialMemory, 0o644)
	runGit(t, repo, "add", "--", "MEMORY.md")
	runGit(t, repo, "commit", "-q", "-m", "init")

	cfg := config.Config{
		RepoPath: config.Canonical(repo),
		StateDir: filepath.Join(t.TempDir(), "state"),
	}
	return New(cfg), repo
}

func requireContractCode(t *testing.T, err error, want memerr.Code) {
	t.Helper()
	contractErr, ok := memerr.As(err)
	if !ok {
		t.Fatalf("expected %s, got %v", want, err)
	}
	if contractErr.Code != want {
		t.Fatalf("got %s, want %s (%v)", contractErr.Code, want, err)
	}
}

func headHash(t *testing.T, repo string) string {
	t.Helper()
	return runGit(t, repo, "rev-parse", "HEAD")
}

func commitCount(t *testing.T, repo string) string {
	t.Helper()
	return runGit(t, repo, "rev-list", "--count", "HEAD")
}

// TestApplyPutAndRevisionMismatch covers W01: the first writer succeeds, a
// stale baseline is refused, and the first writer's content survives.
func TestApplyPutAndRevisionMismatch(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	before, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	firstContent := "# Memory\n\n## Index\n- first write\n"
	result, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "MEMORY.md",
		ExpectedRevision: before.FileRevision,
		Action:           ActionPut,
		RawContent:       firstContent,
	}}})
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if result.Commit == "" || result.TransactionID == "" {
		t.Fatalf("apply did not report a commit: %+v", result)
	}
	if result.SyncState != "local_only" {
		t.Fatalf("syncState = %q, want local_only", result.SyncState)
	}
	if !strings.HasPrefix(result.Revision, "sha256:") {
		t.Fatalf("revision = %q, want a sha256 digest", result.Revision)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != firstContent {
		t.Fatalf("file content = %q, want %q", got, firstContent)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after apply: %q", status)
	}

	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "MEMORY.md",
		ExpectedRevision: before.FileRevision,
		Action:           ActionPut,
		RawContent:       "# Memory\n\n## Index\n- stale write\n",
	}}})
	requireContractCode(t, err, memerr.RevisionMismatch)
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != firstContent {
		t.Fatalf("stale write changed the file: %q", got)
	}
	if count := commitCount(t, repo); count != "2" {
		t.Fatalf("commit count = %s, want 2", count)
	}
}

// TestApplyCreatesAndDeletesFiles checks the absent baseline for creation and
// the delete action, including executable-mode preservation for rewrites.
func TestApplyCreatesAndDeletesFiles(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	if err := os.Mkdir(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "notes/new.md",
		ExpectedRevision: vault.AbsentRevision,
		Action:           ActionPut,
		RawContent:       "created\n",
	}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !samePathSet(created.ChangedFiles, []string{"notes/new.md"}) {
		t.Fatalf("changed files = %v", created.ChangedFiles)
	}
	if got := readTestFile(t, filepath.Join(repo, "notes", "new.md")); got != "created\n" {
		t.Fatalf("created content = %q", got)
	}

	// Creating over an existing path must fail the baseline check.
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "notes/new.md",
		ExpectedRevision: vault.AbsentRevision,
		Action:           ActionPut,
		RawContent:       "again\n",
	}}})
	requireContractCode(t, err, memerr.RevisionMismatch)

	script := filepath.Join(repo, "run.sh")
	writeTestFile(t, script, "#!/bin/sh\necho one\n", 0o755)
	runGit(t, repo, "add", "--", "run.sh")
	runGit(t, repo, "commit", "-q", "-m", "script")
	scriptRead, err := service.ReadFile(ctx, "run.sh")
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "run.sh",
		ExpectedRevision: scriptRead.FileRevision,
		Action:           ActionPut,
		RawContent:       "#!/bin/sh\necho two\n",
	}}}); err != nil {
		t.Fatalf("rewrite script: %v", err)
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit was lost: %v", info.Mode())
	}
	if mode := runGit(t, repo, "ls-files", "-s", "--", "run.sh"); !strings.HasPrefix(mode, "100755") {
		t.Fatalf("Git mode = %q, want 100755", mode)
	}

	newRead, err := service.ReadFile(ctx, "notes/new.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "notes/new.md",
		ExpectedRevision: newRead.FileRevision,
		Action:           ActionDelete,
	}}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "notes", "new.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists after delete: %v", err)
	}
}

// TestApplyRejectsExternalChanges covers W03: a normal write refuses to run
// while staged, unstaged or untracked content exists.
func TestApplyRejectsExternalChanges(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	request := func() MutationRequest {
		return MutationRequest{Changes: []FileChange{{
			Path:             "MEMORY.md",
			ExpectedRevision: entry.FileRevision,
			Action:           ActionPut,
			RawContent:       "# Memory\n\n## Index\n- managed\n",
		}}}
	}

	writeTestFile(t, filepath.Join(repo, "untracked.md"), "external\n", 0o644)
	if _, err := service.Apply(ctx, request()); err != nil {
		requireContractCode(t, err, memerr.ExternalChanges)
	} else {
		t.Fatal("apply with an untracked file must fail")
	}

	runGit(t, repo, "add", "--", "untracked.md")
	if _, err := service.Apply(ctx, request()); err != nil {
		requireContractCode(t, err, memerr.ExternalChanges)
	} else {
		t.Fatal("apply with a staged file must fail")
	}

	runGit(t, repo, "reset", "-q", "--", "untracked.md")
	if err := os.Remove(filepath.Join(repo, "untracked.md")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "MEMORY.md"), "# Memory\n\n## Index\n- external edit\n", 0o644)
	if _, err := service.Apply(ctx, request()); err != nil {
		requireContractCode(t, err, memerr.ExternalChanges)
	} else {
		t.Fatal("apply with an unstaged edit must fail")
	}
}

// TestCaptureCommitsExternalEdits covers the capture half of W03.
func TestCaptureCommitsExternalEdits(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	writeTestFile(t, filepath.Join(repo, "extra.md"), "external\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "MEMORY.md"), "# Memory\n\n## Index\n- external\n", 0o644)

	result, err := service.Capture(ctx, "tester", "sid-1")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !samePathSet(result.ChangedFiles, []string{"extra.md", "MEMORY.md"}) {
		t.Fatalf("captured %v", result.ChangedFiles)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after capture: %q", status)
	}

	empty, err := service.Capture(ctx, "tester", "sid-1")
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if empty.Commit != "" {
		t.Fatalf("capture with no changes committed %q", empty.Commit)
	}
}

// TestApplyRejectsGitOperations covers the local part of G06 and G07: an
// in-progress merge and unmerged index entries without any rebase directory
// both block ordinary writes.
func TestApplyRejectsGitOperations(t *testing.T) {
	t.Run("merge in progress", func(t *testing.T) {
		service, repo := newTestService(t)
		ctx := context.Background()
		runGit(t, repo, "checkout", "-q", "-b", "other")
		writeTestFile(t, filepath.Join(repo, "MEMORY.md"), "other\n", 0o644)
		runGit(t, repo, "commit", "-q", "-am", "other")
		runGit(t, repo, "checkout", "-q", "main")
		writeTestFile(t, filepath.Join(repo, "MEMORY.md"), "main\n", 0o644)
		runGit(t, repo, "commit", "-q", "-am", "main")
		if out, err := gitCommand(t, repo, "", "merge", "other"); err == nil {
			t.Fatalf("merge unexpectedly succeeded:\n%s", out)
		}
		_, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
			Path: "MEMORY.md", ExpectedRevision: vault.AbsentRevision, Action: ActionDelete,
		}}})
		requireContractCode(t, err, memerr.OperationActive)
	})

	t.Run("unmerged index without rebase directory", func(t *testing.T) {
		service, repo := newTestService(t)
		ctx := context.Background()
		seedUnmergedIndex(t, repo)
		if _, err := os.Lstat(filepath.Join(repo, ".git", "MERGE_HEAD")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("test setup left MERGE_HEAD: %v", err)
		}
		_, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
			Path: "MEMORY.md", ExpectedRevision: vault.AbsentRevision, Action: ActionDelete,
		}}})
		requireContractCode(t, err, memerr.OperationActive)
	})
}

func seedUnmergedIndex(t *testing.T, repo string) {
	t.Helper()
	blob := func(content string) string {
		out, err := gitCommand(t, repo, content, "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatalf("hash-object: %v\n%s", err, out)
		}
		return out
	}
	info := fmt.Sprintf("100644 %s 1\tsample.md\n100644 %s 2\tsample.md\n100644 %s 3\tsample.md\n",
		blob("base\n"), blob("ours\n"), blob("theirs\n"))
	if out, err := gitCommand(t, repo, info, "update-index", "--index-info"); err != nil {
		t.Fatalf("update-index: %v\n%s", err, out)
	}
	if out := runGit(t, repo, "ls-files", "--unmerged"); out == "" {
		t.Fatal("failed to seed unmerged index entries")
	}
}

// TestBeginWriteBusyOnIndexLock checks that another Git process holding the
// index lock is reported as BUSY instead of being ignored.
func TestBeginWriteBusyOnIndexLock(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	lockPath := filepath.Join(repo, ".git", "index.lock")
	writeTestFile(t, lockPath, "", 0o644)
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	_, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: vault.AbsentRevision, Action: ActionDelete,
	}}})
	requireContractCode(t, err, memerr.Busy)
}

// TestApplyRejectsLimitsAndBadPaths covers P01 and P03 for the write path.
func TestApplyRejectsLimitsAndBadPaths(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	badPaths := []string{"../escape.md", ".git/config", "notes/../../escape.md", "NUL", `notes\bad.md`}
	for _, path := range badPaths {
		_, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
			Path:             path,
			ExpectedRevision: vault.AbsentRevision,
			Action:           ActionPut,
			RawContent:       "x",
		}}})
		requireContractCode(t, err, memerr.InvalidPath)
	}

	_, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path:             "big.md",
		ExpectedRevision: vault.AbsentRevision,
		Action:           ActionPut,
		RawContent:       strings.Repeat("x", vault.MaxTextFileBytes+1),
	}}})
	requireContractCode(t, err, memerr.LimitExceeded)

	changes := make([]FileChange, 0, MaxMutationFiles+1)
	for index := 0; index <= MaxMutationFiles; index++ {
		changes = append(changes, FileChange{
			Path:             fmt.Sprintf("file-%03d.md", index),
			ExpectedRevision: vault.AbsentRevision,
			Action:           ActionPut,
			RawContent:       "x",
		})
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: changes})
	requireContractCode(t, err, memerr.LimitExceeded)
}

// TestRecoverRollsBackInterruptedWrite covers W04: after an interruption during
// a multi-file write, recovery restores every pre-image.
func TestRecoverRollsBackInterruptedWrite(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	writeTestFile(t, filepath.Join(repo, "notes.md"), "original\n", 0o644)
	runGit(t, repo, "add", "--", "notes.md")
	runGit(t, repo, "commit", "-q", "-m", "notes")
	headBefore := headHash(t, repo)

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := service.ReadFile(ctx, "notes.md")
	if err != nil {
		t.Fatal(err)
	}

	service.testFault = func(stage string) error {
		if stage == "after-write:0" {
			return errSimulatedCrash
		}
		return nil
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{
		{Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- interrupted\n"},
		{Path: "notes.md", ExpectedRevision: notes.FileRevision, Action: ActionPut, RawContent: "changed\n"},
	}})
	service.testFault = nil
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("apply err = %v, want simulated crash", err)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); !strings.Contains(got, "interrupted") {
		t.Fatalf("first file was not written before the interruption: %q", got)
	}
	if got := readTestFile(t, filepath.Join(repo, "notes.md")); got != "original\n" {
		t.Fatalf("second file changed before the interruption: %q", got)
	}

	status, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatalf("recover status: %v", err)
	}
	if len(status.Transactions) != 1 || len(status.Transactions[0].Files) != 2 {
		t.Fatalf("unexpected recover status: %+v", status)
	}

	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("recover rollback: %v", err)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("MEMORY.md = %q, want the pre-image", got)
	}
	if got := readTestFile(t, filepath.Join(repo, "notes.md")); got != "original\n" {
		t.Fatalf("notes.md = %q, want the pre-image", got)
	}
	if headHash(t, repo) != headBefore {
		t.Fatal("recovery changed HEAD")
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after recovery: %q", status)
	}
	after, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Transactions) != 0 {
		t.Fatalf("transaction record survived recovery: %+v", after)
	}
}

// TestRecoverRecognizesCommittedTransaction covers W05: a commit whose message
// carries the transaction marker is completed, not re-applied.
func TestRecoverRecognizesCommittedTransaction(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	service.testFault = func(stage string) error {
		if stage == "after-commit" {
			return errSimulatedCrash
		}
		return nil
	}
	newContent := "# Memory\n\n## Index\n- committed then interrupted\n"
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: newContent,
	}}})
	service.testFault = nil
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("apply err = %v, want simulated crash", err)
	}
	headAfter := headHash(t, repo)
	if count := commitCount(t, repo); count != "2" {
		t.Fatalf("commit count = %s, want 2", count)
	}

	status, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatalf("recover status: %v", err)
	}
	if len(status.Transactions) != 1 || !status.Transactions[0].Resolved || status.Transactions[0].Commit != headAfter {
		t.Fatalf("recover status did not recognize the commit: %+v", status)
	}

	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("recover rollback: %v", err)
	}
	if headHash(t, repo) != headAfter {
		t.Fatal("recovery created a second commit for an already committed transaction")
	}
	if count := commitCount(t, repo); count != "2" {
		t.Fatalf("commit count changed to %s", count)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != newContent {
		t.Fatalf("committed content was rolled back: %q", got)
	}
}

// TestRecoverRefusesUserEditedFile covers W06: a file edited after the
// interruption is reported and never overwritten, while controlled paths are
// still restored.
func TestRecoverRefusesUserEditedFile(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	writeTestFile(t, filepath.Join(repo, "notes.md"), "original\n", 0o644)
	runGit(t, repo, "add", "--", "notes.md")
	runGit(t, repo, "commit", "-q", "-m", "notes")

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := service.ReadFile(ctx, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	service.testFault = func(stage string) error {
		if stage == "after-write:0" {
			return errSimulatedCrash
		}
		return nil
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{
		{Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- interrupted\n"},
		{Path: "notes.md", ExpectedRevision: notes.FileRevision, Action: ActionPut, RawContent: "planned\n"},
	}})
	service.testFault = nil
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("apply err = %v, want simulated crash", err)
	}

	writeTestFile(t, filepath.Join(repo, "notes.md"), "user edit\n", 0o644)
	if _, err := service.RecoverRollback(ctx); err == nil {
		t.Fatal("recovery must refuse to overwrite a user-edited file")
	} else {
		requireContractCode(t, err, memerr.RecoveryRequired)
	}
	if got := readTestFile(t, filepath.Join(repo, "notes.md")); got != "user edit\n" {
		t.Fatalf("user edit was overwritten: %q", got)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("controlled path was not restored: %q", got)
	}
	status, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Transactions) != 1 {
		t.Fatalf("transaction record should remain for manual recovery: %+v", status)
	}
}

// TestApplyRefusesWhileRecoveryPending proves an unfinished transaction blocks
// new writes until it is resolved.
func TestApplyRefusesWhileRecoveryPending(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	service.testFault = func(stage string) error {
		if stage == "after-write:0" {
			return errSimulatedCrash
		}
		return nil
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- pending\n",
	}}})
	service.testFault = nil
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("apply err = %v, want simulated crash", err)
	}

	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "blocked\n",
	}}})
	requireContractCode(t, err, memerr.RecoveryRequired)

	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("recover rollback: %v", err)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("rollback did not restore the entry file: %q", got)
	}
}

// TestApplyHookRejectionRollsBack covers the commit-failure half of W07: a
// rejecting hook makes the write fail clearly and leaves no partial change.
func TestApplyHookRejectionRollsBack(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	writeTestFile(t, hook, "#!/bin/sh\nexit 1\n", 0o755)

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- rejected\n",
	}}})
	requireContractCode(t, err, memerr.GitFailed)
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("rejected write left content behind: %q", got)
	}
	if status := runGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after a rejected hook: %q", status)
	}
	recovered, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Transactions) != 0 {
		t.Fatalf("rejected write left a transaction record: %+v", recovered)
	}
}

// TestApplyHookAddingOutOfPlanFileIsReported covers the out-of-plan half of
// W07: a hook that stages an unrelated file is detected, the real commit is
// reported instead of a fake success, and recovery recognizes it.
func TestApplyHookAddingOutOfPlanFileIsReported(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	writeTestFile(t, hook, "#!/bin/sh\necho hooked > extra.md\ngit add -- extra.md\nexit 0\n", 0o755)

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- hooked\n",
	}}})
	requireContractCode(t, err, memerr.GitFailed)
	if result.Commit == "" {
		t.Fatal("apply must report the commit that actually happened")
	}
	committed := runGit(t, repo, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	if !strings.Contains(committed, "extra.md") {
		t.Fatalf("expected the out-of-plan file in the real commit, got %q", committed)
	}

	status, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Transactions) != 1 || !status.Transactions[0].Resolved || status.Transactions[0].Commit != result.Commit {
		t.Fatalf("recovery did not recognize the real commit: %+v", status)
	}
	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("recover rollback: %v", err)
	}
	if headHash(t, repo) != result.Commit {
		t.Fatal("recovery created a second commit")
	}
}

// TestApplyReportsBusyWhileLockHeld covers the local lock lifetime: a second
// writer cannot run while another process holds the repository lock.
func TestApplyReportsBusyWhileLockHeld(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	gitDir, err := git.GitDir(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(ctx, gitDir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer held.Release()

	shortCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	_, err = service.Apply(shortCtx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: vault.AbsentRevision, Action: ActionDelete,
	}}})
	requireContractCode(t, err, memerr.Busy)
}

// TestApplyRefusesHardLinkedTarget covers the write half of P02: content
// shared with another directory entry is not modified through memctl.
func TestApplyRefusesHardLinkedTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard links are not portable to this test environment")
	}
	service, repo := newTestService(t)
	ctx := context.Background()
	alias := filepath.Join(t.TempDir(), "alias.md")
	if err := os.Link(filepath.Join(repo, "MEMORY.md"), alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- linked\n",
	}}})
	requireContractCode(t, err, memerr.Unsupported)
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("hard-linked content changed: %q", got)
	}
	if got := readTestFile(t, alias); got != testInitialMemory {
		t.Fatalf("alias content changed: %q", got)
	}
}

// TestRecoverRestoresInterruptedDelete verifies that a delete interrupted after
// the file was removed is reported as planned and restored to its pre-image,
// even though the entry file is missing while recovery starts.
func TestRecoverRestoresInterruptedDelete(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()
	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	service.testFault = func(stage string) error {
		if stage == "after-write:0" {
			return errSimulatedCrash
		}
		return nil
	}
	_, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionDelete,
	}}})
	service.testFault = nil
	if !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("apply err = %v, want simulated crash", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "MEMORY.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted delete did not remove the file: %v", err)
	}

	status, err := service.RecoverStatus(ctx)
	if err != nil {
		t.Fatalf("recover status with a missing entry file: %v", err)
	}
	if len(status.Transactions) != 1 || status.Transactions[0].Files[0].State != RecoverFilePlanned {
		t.Fatalf("interrupted delete was not reported as planned: %+v", status)
	}
	if _, err := service.RecoverRollback(ctx); err != nil {
		t.Fatalf("recover rollback: %v", err)
	}
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("deleted entry file was not restored: %q", got)
	}
}

// TestApplyRejectsStaleBaseRevision covers the optional whole-repository
// baseline used by multi-file requests.
func TestApplyRejectsStaleBaseRevision(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	baseline, err := service.Capture(ctx, "", "")
	if err != nil {
		t.Fatalf("capture revision: %v", err)
	}
	writeTestFile(t, filepath.Join(repo, "extra.md"), "external\n", 0o644)
	if _, err := service.Capture(ctx, "", ""); err != nil {
		t.Fatalf("capture external edit: %v", err)
	}

	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(ctx, MutationRequest{
		BaseRevision: baseline.Revision,
		Changes: []FileChange{{
			Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- stale base\n",
		}},
	})
	requireContractCode(t, err, memerr.RevisionMismatch)
	if got := readTestFile(t, filepath.Join(repo, "MEMORY.md")); got != testInitialMemory {
		t.Fatalf("stale-base write changed the file: %q", got)
	}
}

// TestReadFileRefusesProtectedStates covers P2-1: a CLI read must not return
// transaction-intermediate content. It reports busy while the lock is held,
// recovery while an unfinished transaction exists, and the operation state
// while a managed conflict or Git operation is active.
func TestReadFileRefusesProtectedStates(t *testing.T) {
	t.Run("lock busy", func(t *testing.T) {
		service, repo := newTestService(t)
		ctx := context.Background()
		gitDir, err := git.GitDir(ctx, repo)
		if err != nil {
			t.Fatalf("git dir: %v", err)
		}
		held, ok, err := lock.TryAcquire(gitDir)
		if err != nil || !ok {
			t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
		}
		defer held.Release()

		if _, err := service.ReadFile(ctx, "MEMORY.md"); err == nil {
			t.Fatal("ReadFile returned while the repository lock was held")
		} else {
			requireContractCode(t, err, memerr.Busy)
		}
	})

	t.Run("unfinished transaction", func(t *testing.T) {
		service, repo := newTestService(t)
		ctx := context.Background()
		entry, err := service.ReadFile(ctx, "MEMORY.md")
		if err != nil {
			t.Fatal(err)
		}
		service.testFault = func(stage string) error {
			if stage == "after-record" {
				return errSimulatedCrash
			}
			return nil
		}
		if _, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
			Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- pending\n",
		}}}); !errors.Is(err, errSimulatedCrash) {
			t.Fatalf("apply err = %v, want simulated crash", err)
		}
		service.testFault = nil
		if count := commitCount(t, repo); count != "1" {
			t.Fatalf("the interrupted apply committed: %s", count)
		}

		if _, err := service.ReadFile(ctx, "MEMORY.md"); err == nil {
			t.Fatal("ReadFile returned while a transaction needed recovery")
		} else {
			requireContractCode(t, err, memerr.RecoveryRequired)
		}
	})
}

// TestApplyReportsPendingRemoteState covers P2-3: a managed commit in a
// repository with a remote is pending until a push succeeds, and the cached
// sync summary records it. A local-only repository stays local_only.
func TestApplyReportsPendingRemoteState(t *testing.T) {
	service, repo := newTestService(t)
	ctx := context.Background()

	local, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: local.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- local\n",
	}}})
	if err != nil {
		t.Fatalf("local apply: %v", err)
	}
	if result.SyncState != "local_only" {
		t.Fatalf("local-only sync state = %q, want local_only", result.SyncState)
	}

	if err := git.AddRemote(ctx, repo, "origin", "https://example.invalid/memory.git"); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	entry, err := service.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	result, err = service.Apply(ctx, MutationRequest{Changes: []FileChange{{
		Path: "MEMORY.md", ExpectedRevision: entry.FileRevision, Action: ActionPut, RawContent: "# Memory\n\n## Index\n- remote\n",
	}}})
	if err != nil {
		t.Fatalf("remote apply: %v", err)
	}
	if result.SyncState != "pending" {
		t.Fatalf("remote sync state = %q, want pending", result.SyncState)
	}

	repoID := config.RepoID(repo)
	summary, ok, err := state.OpenCache(service.cfg.StateDir, repoID).LoadSync()
	if err != nil {
		t.Fatalf("load sync summary: %v", err)
	}
	if !ok || summary.RepoID != repoID || summary.RemoteState != "pending" || summary.LocalState != "clean" {
		t.Fatalf("cached summary = %+v ok=%v, want a pending record for %s", summary, ok, repoID)
	}
	if summary.Head != result.Commit {
		t.Fatalf("cached head = %q, want the new commit %q", summary.Head, result.Commit)
	}
}
