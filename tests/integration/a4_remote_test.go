package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This file verifies the A-4 remote scenarios G02, G03, G04, G06 and the remote
// approval boundary. Every test uses a temporary HOME, a local bare remote and
// GIT_ALLOW_PROTOCOL=file:ssh, so no real network or credential source is used.

func a4Env(home, repo string, extra ...string) []string {
	env := a2Env(home, repo, true)
	env = append(env, "GIT_ALLOW_PROTOCOL=file:ssh")
	env = append(env, extra...)
	return env
}

func a4Home(t *testing.T, root, name string) string {
	t.Helper()
	home := filepath.Join(root, name)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create home %s: %v", name, err)
	}
	return home
}

func newBareRemote(t *testing.T, dir, name string) string {
	t.Helper()
	bare := filepath.Join(dir, name)
	cmd := exec.Command("git", "init", "-q", "--bare", bare)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, string(out))
	}
	return bare
}

func writeStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return path
}

func a4InitRemote(t *testing.T, bin, home, repo, url string, extraEnv ...string) cliResult {
	t.Helper()
	return runMemctl(t, bin, home, a4Env(home, repo, extraEnv...), "init", url, "--confirm-private")
}

// TestA4PrivateRemoteRoundTripCoversG02 covers the second-device connection:
// an empty remote receives the first commit, a second device clones the same
// history with the correct upstream and creates no independent root commit.
func TestA4PrivateRemoteRoundTripCoversG02(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")

	home1 := a4Home(t, root, "home1")
	home2 := a4Home(t, root, "home2")
	repo1 := filepath.Join(home1, "memory")
	repo2 := filepath.Join(home2, "memory")

	first := a4InitRemote(t, bin, home1, repo1, url)
	if first.code != 0 {
		t.Fatalf("device1 init: code=%d stderr=%q", first.code, first.stderr)
	}
	if !strings.Contains(first.stdout, "state: created") || !strings.Contains(first.stdout, "remote: synced") {
		t.Fatalf("device1 init stdout = %q", first.stdout)
	}

	second := a4InitRemote(t, bin, home2, repo2, url)
	if second.code != 0 {
		t.Fatalf("device2 init: code=%d stderr=%q", second.code, second.stderr)
	}
	if !strings.Contains(second.stdout, "state: cloned") {
		t.Fatalf("device2 init stdout = %q, want a clone", second.stdout)
	}

	head1 := gitOutput(t, a4Env(home1, repo1), repo1, "rev-parse", "HEAD")
	head2 := gitOutput(t, a4Env(home2, repo2), repo2, "rev-parse", "HEAD")
	if head1 != head2 {
		t.Fatalf("devices have different history: %s vs %s", head1, head2)
	}
	if count := gitOutput(t, a4Env(home2, repo2), repo2, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("device2 commit count = %s, want 1 (no independent root)", count)
	}
	if upstream := gitOutput(t, a4Env(home2, repo2), repo2, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); upstream != "origin/main" {
		t.Fatalf("device2 upstream = %q, want origin/main", upstream)
	}

	// A local commit on device2 pushes, and device1 fast-forwards.
	a4AppendEntry(t, bin, home2, repo2, "device2")
	sync := runMemctl(t, bin, home2, a4Env(home2, repo2), "sync", "--wait")
	if sync.code != 0 || !strings.Contains(sync.stdout, "remote: synced") {
		t.Fatalf("device2 sync: code=%d stdout=%q stderr=%q", sync.code, sync.stdout, sync.stderr)
	}
	sync1 := runMemctl(t, bin, home1, a4Env(home1, repo1), "sync", "--wait")
	if sync1.code != 0 || !strings.Contains(sync1.stdout, "remote: synced") {
		t.Fatalf("device1 sync: code=%d stdout=%q stderr=%q", sync1.code, sync1.stdout, sync1.stderr)
	}
	if got := mustReadFile(t, filepath.Join(repo1, "MEMORY.md")); !strings.Contains(got, "device2") {
		t.Fatalf("device1 did not fast-forward: %q", got)
	}
}

// TestA4RemoteBoundaryAndErrors covers the G03 and G04 access branches: URL
// mismatch is refused, an unreachable URL is not treated as an empty remote,
// and a push failure keeps the local commit for the next attempt.
func TestA4RemoteBoundaryAndErrors(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	env := a4Env(home, repo)

	if result := a4InitRemote(t, bin, home, repo, url); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	t.Run("wrong url is not an empty remote", func(t *testing.T) {
		bad := filepath.Join(root, "missing.git")
		other := a4Home(t, root, "home-bad")
		otherRepo := filepath.Join(other, "memory")
		result := a4InitRemote(t, bin, other, otherRepo, bad)
		if result.code == 0 {
			t.Fatalf("init with a missing remote succeeded: %q", result.stdout)
		}
		if _, err := os.Stat(filepath.Join(otherRepo, ".git")); !os.IsNotExist(err) {
			t.Fatalf("failed remote init created a repository, stat err = %v", err)
		}
	})

	t.Run("auth failure is classified", func(t *testing.T) {
		stub := writeStub(t, root, "ssh-auth-fail.sh", `echo "git@example.invalid: Permission denied (publickey)." >&2`+"\nexit 255\n")
		other := a4Home(t, root, "home-auth")
		otherRepo := filepath.Join(other, "memory")
		result := a4InitRemote(t, bin, other, otherRepo, "ssh://git@example.invalid/repo.git", "GIT_SSH_COMMAND="+stub)
		if result.code != 1 || !strings.Contains(result.stderr, "AUTH_FAILED") {
			t.Fatalf("auth failure: code=%d stderr=%q, want AUTH_FAILED", result.code, result.stderr)
		}
	})

	t.Run("non-fast-forward push is retained for the next sync", func(t *testing.T) {
		// A third clone advances the remote from a pre-push hook after this
		// repository already fetched, so the push is rejected.
		thirdHome := a4Home(t, root, "home3")
		thirdRepo := filepath.Join(thirdHome, "memory")
		if result := a4InitRemote(t, bin, thirdHome, thirdRepo, url); result.code != 0 {
			t.Fatalf("third init: code=%d stderr=%q", result.code, result.stderr)
		}
		hook := filepath.Join(repo, ".git", "hooks", "pre-push")
		if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\ngit -C " + thirdRepo + " commit --allow-empty -q -m advance\ngit -C " + thirdRepo + " push -q origin main\nexit 0\n"
		if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(hook)

		a4AppendEntry(t, bin, home, repo, "nff")
		localHead := gitOutput(t, env, repo, "rev-parse", "HEAD")
		result := runMemctl(t, bin, home, env, "sync", "--wait")
		if result.code != 1 {
			t.Fatalf("non-fast-forward sync code = %d, stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stdout, "remote: pending") || !strings.Contains(result.stdout, "reason: NON_FAST_FORWARD") {
			t.Fatalf("non-fast-forward sync stdout = %q, want pending/NON_FAST_FORWARD", result.stdout)
		}
		if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
			t.Fatalf("worktree not clean after rejected push: %q", status)
		}
		if head := gitOutput(t, env, repo, "rev-parse", "HEAD"); head == "" {
			t.Fatalf("local commit was lost after rejected push (was %s)", localHead)
		}
	})

	t.Run("remote without a default branch fails clearly", func(t *testing.T) {
		badBare := newBareRemote(t, root, "ndb.git")
		seed := a4Home(t, root, "ndb-seed")
		seedEnv := []string{
			"HOME=" + seed,
			"PATH=" + os.Getenv("PATH"),
			"GIT_AUTHOR_NAME=Memctl Test",
			"GIT_AUTHOR_EMAIL=memctl-test@example.invalid",
			"GIT_COMMITTER_NAME=Memctl Test",
			"GIT_COMMITTER_EMAIL=memctl-test@example.invalid",
		}
		gitOutput(t, seedEnv, seed, "init", "-q", "-b", "weird")
		if err := os.WriteFile(filepath.Join(seed, "MEMORY.md"), []byte("# Memory\n\n## Index\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOutput(t, seedEnv, seed, "add", "MEMORY.md")
		gitOutput(t, seedEnv, seed, "commit", "-qm", "init")
		gitOutput(t, seedEnv, seed, "push", "-q", badBare, "weird")
		gitOutput(t, seedEnv, badBare, "symbolic-ref", "HEAD", "refs/heads/nonexistent")

		other := a4Home(t, root, "home-ndb")
		otherRepo := filepath.Join(other, "memory")
		result := a4InitRemote(t, bin, other, otherRepo, badBare)
		if result.code != 1 {
			t.Fatalf("init without a default branch: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stderr, "NO_REPO") && !strings.Contains(result.stderr, "NO_UPSTREAM") {
			t.Fatalf("init without a default branch stderr = %q, want a clear preparation error", result.stderr)
		}
		if _, err := os.Stat(filepath.Join(otherRepo, "MEMORY.md")); !os.IsNotExist(err) {
			t.Fatalf("init without a default branch created an entry file, stat err = %v", err)
		}
	})

	t.Run("unset upstream follows the init protocol", func(t *testing.T) {
		gitOutput(t, env, repo, "branch", "--unset-upstream")
		result := runMemctl(t, bin, home, env, "sync", "--wait")
		if result.code != 0 {
			t.Fatalf("sync without upstream: code=%d stderr=%q", result.code, result.stderr)
		}
		if upstream := gitOutput(t, env, repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); upstream != "origin/main" {
			t.Fatalf("upstream = %q, want origin/main", upstream)
		}
	})
}

// TestA4ManualGitStatePausesSyncCoversG06 verifies that a user's unfinished Git
// operation stops synchronization with OPERATION_ACTIVE.
func TestA4ManualGitStatePausesSyncCoversG06(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	env := a4Env(home, repo)

	if result := a4InitRemote(t, bin, home, repo, url); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	// Start a user-owned merge that stops with a conflict.
	gitOutput(t, env, repo, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Memory\n\n## Index\n- side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, env, repo, "commit", "-qam", "side")
	gitOutput(t, env, repo, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Memory\n\n## Index\n- main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, env, repo, "commit", "-qam", "main")
	gitFails(t, env, repo, "merge", "side")

	result := runMemctl(t, bin, home, env, "sync", "--wait")
	if result.code != 1 || !strings.Contains(result.stderr, "OPERATION_ACTIVE") {
		t.Fatalf("sync with a manual merge: code=%d stderr=%q, want OPERATION_ACTIVE", result.code, result.stderr)
	}

	// A managed write is also refused while the user operation is active.
	write := runMemctlStdin(t, bin, home, env, "# Memory\n\n## Index\n- x\n", "write", "MEMORY.md", "--base", "absent", "--stdin")
	if write.code != 1 || !strings.Contains(write.stderr, "OPERATION_ACTIVE") {
		t.Fatalf("write with a manual merge: code=%d stderr=%q, want OPERATION_ACTIVE", write.code, write.stderr)
	}
}

// TestA4RemoteApprovalRequired ensures a remote configured outside memctl is
// not synchronized before the user confirms it.
func TestA4RemoteApprovalRequired(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	env := a4Env(home, repo)

	local := runMemctl(t, bin, home, env, "init")
	if local.code != 0 {
		t.Fatalf("local init: code=%d stderr=%q", local.code, local.stderr)
	}
	gitOutput(t, env, repo, "remote", "add", "origin", url)

	result := runMemctl(t, bin, home, env, "sync", "--wait")
	if result.code != 1 || !strings.Contains(result.stderr, "REMOTE_NOT_APPROVED") {
		t.Fatalf("unapproved sync: code=%d stderr=%q, want REMOTE_NOT_APPROVED", result.code, result.stderr)
	}

	// The explicit confirmation of the same URL enables synchronization.
	confirm := runMemctl(t, bin, home, env, "init", url, "--confirm-private")
	if confirm.code != 0 {
		t.Fatalf("confirm init: code=%d stderr=%q", confirm.code, confirm.stderr)
	}
	sync := runMemctl(t, bin, home, env, "sync", "--wait")
	if sync.code != 0 || !strings.Contains(sync.stdout, "remote: synced") {
		t.Fatalf("confirmed sync: code=%d stdout=%q stderr=%q", sync.code, sync.stdout, sync.stderr)
	}
}

// a4AppendEntry reads the current baseline and writes one extra entry line.
func a4AppendEntry(t *testing.T, bin, home, repo, label string) {
	t.Helper()
	env := a4Env(home, repo)
	current := mustReadFile(t, filepath.Join(repo, "MEMORY.md"))
	entry := "- " + label + "\n"
	if !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	result := runMemctlStdin(t, bin, home, env, current+entry, "write", "MEMORY.md", "--base", readRevision(t, bin, home, repo, env, "MEMORY.md").FileRevision, "--stdin")
	if result.code != 0 {
		t.Fatalf("append entry write: code=%d stderr=%q", result.code, result.stderr)
	}
}

// TestA4LocalCommitReportsPendingAndResetsCountsCoversG02 covers P2-2 and P2-3
// end to end: a managed commit in a repository with a remote reports pending,
// and a completed sync reports zero ahead and behind.
func TestA4LocalCommitReportsPendingAndResetsCountsCoversG02(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	env := a4Env(home, repo)

	if result := a4InitRemote(t, bin, home, repo, url); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	baseline := readRevision(t, bin, home, repo, env, "MEMORY.md")
	write := runMemctlStdin(t, bin, home, env, "# Memory\n\n## Index\n- pending\n", "write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin")
	if write.code != 0 {
		t.Fatalf("write: code=%d stderr=%q", write.code, write.stderr)
	}
	if !strings.Contains(write.stdout, "sync: pending") {
		t.Fatalf("write stdout = %q, want sync: pending", write.stdout)
	}

	synced := runMemctl(t, bin, home, env, "sync", "--wait")
	if synced.code != 0 || !strings.Contains(synced.stdout, "remote: synced") {
		t.Fatalf("sync: code=%d stdout=%q stderr=%q", synced.code, synced.stdout, synced.stderr)
	}
	if !strings.Contains(synced.stdout, "ahead: 0") || !strings.Contains(synced.stdout, "behind: 0") {
		t.Fatalf("sync stdout = %q, want zero ahead and behind after a completed sync", synced.stdout)
	}
}

// TestA4InitRemoteBoundariesCoversG02G04 covers P2-7: an init that fails must
// not delete a directory the caller provided, a remote with unrelated history
// must leave no approved remote behind, and a remote whose push URL differs
// from the requested target is refused.
func TestA4InitRemoteBoundariesCoversG02G04(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	env := a4Env(home, repo)

	t.Run("caller-provided empty directory survives a failed connection", func(t *testing.T) {
		other := a4Home(t, root, "home-empty")
		emptyRepo := filepath.Join(other, "memory")
		if err := os.MkdirAll(emptyRepo, 0o700); err != nil {
			t.Fatal(err)
		}
		result := a4InitRemote(t, bin, other, emptyRepo, filepath.Join(root, "missing.git"))
		if result.code == 0 {
			t.Fatalf("init with a missing remote succeeded: %q", result.stdout)
		}
		info, err := os.Stat(emptyRepo)
		if err != nil || !info.IsDir() {
			t.Fatalf("the caller-provided directory was removed: info=%v err=%v", info, err)
		}
		if _, err := os.Stat(filepath.Join(emptyRepo, ".git")); !os.IsNotExist(err) {
			t.Fatalf("failed init created a repository, stat err = %v", err)
		}
	})

	t.Run("unrelated history keeps no remote or approval", func(t *testing.T) {
		unrelated := newBareRemote(t, root, "unrelated.git")
		seed := a4Home(t, root, "seed")
		seedEnv := []string{
			"HOME=" + seed,
			"PATH=" + os.Getenv("PATH"),
			"GIT_AUTHOR_NAME=Memctl Test",
			"GIT_AUTHOR_EMAIL=memctl-test@example.invalid",
			"GIT_COMMITTER_NAME=Memctl Test",
			"GIT_COMMITTER_EMAIL=memctl-test@example.invalid",
		}
		gitOutput(t, seedEnv, seed, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(seed, "MEMORY.md"), []byte("# Memory\n\n## Index\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOutput(t, seedEnv, seed, "add", "MEMORY.md")
		gitOutput(t, seedEnv, seed, "commit", "-qm", "init")
		gitOutput(t, seedEnv, seed, "push", "-q", unrelated, "main")

		if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
			t.Fatalf("local init: code=%d stderr=%q", result.code, result.stderr)
		}
		result := a4InitRemote(t, bin, home, repo, unrelated)
		if result.code != 1 || !strings.Contains(result.stderr, "NO_UPSTREAM") {
			t.Fatalf("unrelated init: code=%d stderr=%q, want NO_UPSTREAM", result.code, result.stderr)
		}
		if remotes := gitOutput(t, env, repo, "remote"); remotes != "" {
			t.Fatalf("a refused connection left the remote configured: %q", remotes)
		}
	})

	t.Run("extra push url is refused", func(t *testing.T) {
		if result := a4InitRemote(t, bin, home, repo, url); result.code != 0 {
			t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
		}
		extra := newBareRemote(t, root, "extra.git")
		gitOutput(t, env, repo, "remote", "set-url", "--add", "--push", "origin", extra)

		result := a4InitRemote(t, bin, home, repo, url)
		if result.code != 1 || !strings.Contains(result.stderr, "INVALID_ARGUMENT") {
			t.Fatalf("extra push url: code=%d stderr=%q, want INVALID_ARGUMENT", result.code, result.stderr)
		}
		urls := gitOutput(t, env, repo, "remote", "get-url", "--push", "--all", "origin")
		if !strings.Contains(urls, "extra.git") {
			t.Fatalf("the refusal modified the remote URLs: %q", urls)
		}
	})

	t.Run("caller-provided empty directory receives a clone", func(t *testing.T) {
		cloneURL := newBareRemote(t, root, "clone-source.git")
		seedHome := a4Home(t, root, "seed-clone")
		if result := a4InitRemote(t, bin, seedHome, filepath.Join(seedHome, "memory"), cloneURL); result.code != 0 {
			t.Fatalf("seed init: code=%d stderr=%q", result.code, result.stderr)
		}

		targetHome := a4Home(t, root, "home-clone")
		targetRepo := filepath.Join(targetHome, "memory")
		if err := os.MkdirAll(targetRepo, 0o700); err != nil {
			t.Fatal(err)
		}
		result := a4InitRemote(t, bin, targetHome, targetRepo, cloneURL)
		if result.code != 0 || !strings.Contains(result.stdout, "state: cloned") {
			t.Fatalf("clone into a caller-provided directory: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
		}
	})
}
