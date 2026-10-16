package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
)

// This file verifies P1-2 through L01: every Git child started inside a locked
// transaction inherits the repository lock descriptor, so a killed parent
// cannot leave Git writing to the repository after the lock is released.

// writeFakeGit writes a PATH shim that records whether the inherited lock
// descriptor (fd 3) was present and, when blockCommand is set, blocks on that
// subcommand. Paths are embedded in the script because the Git child
// environment is restricted to the documented credential whitelist.
func writeFakeGit(t *testing.T, dir, realGit, fdLog, blockPid, blockCommand string) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ -e /dev/fd/3 ]; then\n" +
		"  echo \"fd3=yes $*\" >> \"" + fdLog + "\"\n" +
		"else\n" +
		"  echo \"fd3=no $*\" >> \"" + fdLog + "\"\n" +
		"fi\n" +
		"if [ -n \"" + blockPid + "\" ] && [ \"$1\" = \"" + blockCommand + "\" ]; then\n" +
		"  echo $$ > \"" + blockPid + "\"\n" +
		"  sleep 3\n" +
		"fi\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return path
}

func fakeGitEnv(t *testing.T, env []string, fakeBin string) []string {
	t.Helper()
	return envWith(env, "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestLocalGitChildrenInheritLockCoversL01 covers the inheritance rule: the
// commands that modify the repository inside the transaction see the lock
// descriptor, while commands started before the lock do not need it.
func TestLocalGitChildrenInheritLockCoversL01(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	fakeBin := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	fdLog := filepath.Join(root, "fd.log")
	writeFakeGit(t, fakeBin, realGit, fdLog, "", "")
	env = fakeGitEnv(t, env, fakeBin)

	baseline := readRevision(t, bin, home, repo, env, "MEMORY.md")
	write := runMemctlStdin(t, bin, home, env, "# Memory\n\n## Index\n- inherited\n", "write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin")
	if write.code != 0 {
		t.Fatalf("write: code=%d stderr=%q", write.code, write.stderr)
	}

	log := mustReadFile(t, fdLog)
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		invocation := strings.Join(fields[1:], " ")
		for _, command := range []string{"add", "commit"} {
			if invocation == command || strings.HasPrefix(invocation, command+" ") {
				seen[command+":"+fields[0]] = true
			}
		}
	}
	for _, want := range []string{"add:fd3=yes", "commit:fd3=yes"} {
		if !seen[want] {
			t.Fatalf("git %s did not inherit the lock descriptor; log:\n%s", want, log)
		}
	}
	for _, unwanted := range []string{"add:fd3=no", "commit:fd3=no"} {
		if seen[unwanted] {
			t.Fatalf("git %s ran without the lock descriptor; log:\n%s", unwanted, log)
		}
	}
}

// TestKilledWriteParentKeepsLockWithGitCoversL01 kills the memctl parent while
// its Git commit child is still running: the inherited descriptor must keep the
// lock held until that child exits.
func TestKilledWriteParentKeepsLockWithGitCoversL01(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	fakeBin := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	commitPid := filepath.Join(root, "commit.pid")
	writeFakeGit(t, fakeBin, realGit, filepath.Join(root, "fd.log"), commitPid, "commit")
	env = fakeGitEnv(t, env, fakeBin)

	baseline := readRevision(t, bin, home, repo, env, "MEMORY.md")
	cmd := exec.Command(bin, "write", "MEMORY.md", "--base", baseline.FileRevision, "--stdin")
	cmd.Dir = home
	cmd.Env = env
	cmd.Stdin = strings.NewReader("# Memory\n\n## Index\n- killed\n")
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start write: %v", err)
	}

	waitForFilePid(t, commitPid, 10*time.Second)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill parent: %v", err)
	}
	_ = cmd.Wait()

	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	if held, ok, err := lock.TryAcquire(gitDir); err != nil {
		t.Fatalf("TryAcquire after the parent was killed: %v", err)
	} else {
		if ok {
			_ = held.Release()
			t.Fatal("the repository lock was released when the parent was killed; the Git child ran without the lock")
		}
	}

	// The Git child exits after its fixed delay and releases the last descriptor.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		acquired, ok, err := lock.TryAcquire(gitDir)
		if err != nil {
			t.Fatalf("TryAcquire: %v", err)
		}
		if ok {
			_ = acquired.Release()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the repository lock was not released after the Git child exited")
}

func waitForFilePid(t *testing.T, path string, timeout time.Duration) int {
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
	t.Fatalf("the wrapped Git process never recorded its pid at %s", path)
	return 0
}
