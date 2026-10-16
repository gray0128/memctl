package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
)

// This file verifies A-5 sub-task 1 (T018): background synchronization
// derivation, credential environment isolation and process lifecycle. It covers
// G08.2, L01 (background/kill scenario) and L02.

// TestA5BackgroundCredentialDifferenceCoversG08_2 starts a background worker
// whose credential environment differs from the foreground shell. The worker
// must fail as AUTH_FAILED, keep the local commit and leave the repository
// lock free; the next foreground sync pushes the retained commit.
func TestA5BackgroundCredentialDifferenceCoversG08_2(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")

	stub := writeStub(t, root, "ssh-conditional.sh", `
if [ "$MEMCTL_TEST_FOREGROUND" = "1" ]; then
  host=$1
  shift
  exec sh -c "$*"
fi
echo "git@example.invalid: Permission denied (publickey)." >&2
exit 255
`)
	sshURL := "ssh://git@example.invalid" + url
	baseEnv := a4Env(home, repo, "GIT_SSH_COMMAND="+stub)
	foregroundEnv := envWith(baseEnv, "MEMCTL_TEST_FOREGROUND=1", "AGENT_MEMORY_BACKGROUND=1")

	if result := runMemctl(t, bin, home, foregroundEnv, "init", sshURL, "--confirm-private"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	a4WriteFile(t, bin, home, repo, foregroundEnv, "MEMORY.md", "# Memory\n\n## Index\n- one\n")
	if result := runMemctl(t, bin, home, foregroundEnv, "sync", "--wait"); result.code != 0 {
		t.Fatalf("first foreground sync: code=%d stderr=%q", result.code, result.stderr)
	}

	// A second local commit that only the background worker tries to push.
	a4WriteFile(t, bin, home, repo, baseEnv, "MEMORY.md", "# Memory\n\n## Index\n- one\n- two\n")
	localHead := gitOutput(t, baseEnv, repo, "rev-parse", "HEAD")

	// The parent has the foreground marker; the whitelist snapshot does not.
	if result := runMemctl(t, bin, home, foregroundEnv, "end", "--agent", "test", "--sid", "s1"); result.code != 0 {
		t.Fatalf("end: code=%d stderr=%q", result.code, result.stderr)
	}
	waitForSyncReason(t, home, "AUTH_FAILED", "failed", 15*time.Second)
	waitForNoProcesses(t, bin, 10*time.Second)

	if remoteHead := gitOutput(t, baseEnv, url, "rev-parse", "main"); remoteHead == localHead {
		t.Fatalf("background worker pushed despite an unavailable credential environment")
	}
	if head := gitOutput(t, baseEnv, repo, "rev-parse", "HEAD"); head != localHead {
		t.Fatalf("local commit was lost: %s -> %s", localHead, head)
	}
	if status := gitOutput(t, baseEnv, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after a failed background sync: %q", status)
	}
	// The lock must be free, so a foreground operation can run.
	if result := runMemctl(t, bin, home, foregroundEnv, "sync", "--wait"); result.code != 0 {
		t.Fatalf("foreground recovery sync: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if remoteHead := gitOutput(t, baseEnv, url, "rev-parse", "main"); remoteHead != localHead {
		t.Fatalf("foreground sync did not push the retained commit: remote=%s local=%s", remoteHead, localHead)
	}
}

// TestA5KilledParentKeepsLockWithGitCoversL01 kills the memctl parent while Git
// still runs. The inherited lock must keep the repository locked until the Git
// child exits, so no writer can run without the lock.
func TestA5KilledParentKeepsLockWithGitCoversL01(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")

	proxy := writeStub(t, root, "ssh-proxy.sh", sshProxyBody)
	sshURL := "ssh://git@example.invalid" + url
	env := a4Env(home, repo, "GIT_SSH_COMMAND="+proxy)
	if result := runMemctl(t, bin, home, env, "init", sshURL, "--confirm-private"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	pidFile := filepath.Join(root, "hang.pid")
	hang := writeStub(t, root, "ssh-hang.sh", "echo $PPID > \""+pidFile+"\"\nexec sleep 300\n")
	runEnv := envWith(env, "GIT_SSH_COMMAND="+hang)

	cmd := exec.Command(bin, "sync", "--wait")
	cmd.Dir = home
	cmd.Env = runEnv
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sync: %v", err)
	}
	gitPid := waitForPid(t, pidFile, 10*time.Second)

	// Kill the parent without any cleanup.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill parent: %v", err)
	}
	_ = cmd.Wait()
	if !processAlive(gitPid) {
		t.Fatalf("Git child %d exited together with the killed parent", gitPid)
	}

	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	if _, ok, err := lock.TryAcquire(gitDir); err == nil && ok {
		t.Fatal("repository lock was released while the Git child was still running")
	}

	// Kill the whole Git process group: Git and the SSH child both inherited
	// the lock descriptor, so the lock is released only when both exit.
	killProcessGroup(t, gitPid)
	waitForProcessGone(t, gitPid, 10*time.Second)

	deadline := time.Now().Add(5 * time.Second)
	for {
		held, ok, err := lock.TryAcquire(gitDir)
		if err != nil {
			t.Fatalf("try acquire: %v", err)
		}
		if ok {
			_ = held.Release()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("repository lock was not released after the Git child exited")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestA5BackgroundStormKeepsStateCoversL02 triggers many `end` calls while the
// network is unavailable. Background workers must serialize on the lock instead
// of piling up, and the local commit must stay intact.
func TestA5BackgroundStormKeepsStateCoversL02(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")

	proxy := writeStub(t, root, "ssh-proxy.sh", sshProxyBody)
	sshURL := "ssh://git@example.invalid" + url
	env := a4Env(home, repo, "GIT_SSH_COMMAND="+proxy)
	if result := runMemctl(t, bin, home, env, "init", sshURL, "--confirm-private"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}
	a4WriteFile(t, bin, home, repo, env, "MEMORY.md", "# Memory\n\n## Index\n- storm\n")
	localHead := gitOutput(t, env, repo, "rev-parse", "HEAD")

	// A network-failing stub that records concurrent invocations. The paths live
	// under HOME so the background whitelist forwards them.
	down := writeStub(t, root, "ssh-down.sh", `
guard="$HOME/.memctl-guard"
if ! mkdir "$guard" 2>/dev/null; then
  echo concurrent > "$HOME/.memctl-concurrent"
  exit 255
fi
sleep 0.2
rmdir "$guard"
echo "ssh: Could not resolve hostname example.invalid: Name or service not known" >&2
exit 255
`)
	stormEnv := envWith(env, "GIT_SSH_COMMAND="+down, "AGENT_MEMORY_BACKGROUND=1")

	for i := 0; i < 15; i++ {
		if result := runMemctl(t, bin, home, stormEnv, "end", "--agent", "test", "--sid", "s1"); result.code != 0 {
			t.Fatalf("end %d: code=%d stderr=%q", i, result.code, result.stderr)
		}
	}
	waitForNoProcesses(t, bin, 20*time.Second)

	if _, err := os.Stat(filepath.Join(home, ".memctl-concurrent")); !os.IsNotExist(err) {
		t.Fatalf("two background fetches ran concurrently, stat err = %v", err)
	}
	if head := gitOutput(t, env, repo, "rev-parse", "HEAD"); head != localHead {
		t.Fatalf("local commit changed during the storm: %s -> %s", localHead, head)
	}
	if status := gitOutput(t, env, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after the storm: %q", status)
	}
}

func waitForPid(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %s never recorded a pid", path)
	return 0
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// killProcessGroup kills a Git process group: the Git process is its own group
// leader (Setpgid), so -pid reaches Git and every transport child that
// inherited the lock descriptor.
func killProcessGroup(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func waitForProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", pid)
}

func waitForSyncReason(t *testing.T, home, reason, remoteState string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(home, ".agent-memory", "repos", "*", "cache", "sync.json"))
		for _, match := range matches {
			data, err := os.ReadFile(match)
			if err != nil {
				continue
			}
			var summary struct {
				ReasonCode  string `json:"reasonCode"`
				RemoteState string `json:"remoteState"`
			}
			if json.Unmarshal(data, &summary) == nil && summary.ReasonCode == reason && summary.RemoteState == remoteState {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no background sync state recorded reason %s with state %s", reason, remoteState)
}

func waitForNoProcesses(t *testing.T, bin string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if countProcesses(bin) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("background workers are still running: %d", countProcesses(bin))
}

func countProcesses(bin string) int {
	cmd := exec.Command("pgrep", "-f", bin)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
