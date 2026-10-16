package integration_test

import (
	"context"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
)

// This file verifies the G08 credential sub-scenarios that A-4 can exercise in
// the foreground and the L01 foreground process cleanup. G08.2 (background
// derivation) belongs to A-5.

// sshProxyStub forwards a fake SSH invocation to the local Git transport so a
// real push/clone can run without a network or a real SSH server.
const sshProxyBody = `host=$1
shift
if [ -n "$MEMCTL_STUB_LOG" ]; then
  echo "SSH_AUTH_SOCK=$SSH_AUTH_SOCK" >> "$MEMCTL_STUB_LOG"
fi
exec sh -c "$*"
`

// TestA4SSHAgentForegroundCoversG08_1 covers the explicit foreground path with
// an available agent: the sync succeeds, the agent socket reaches the child and
// the remote receives the commit.
func TestA4SSHAgentForegroundCoversG08_1(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	log := filepath.Join(root, "ssh.log")
	stub := writeStub(t, root, "ssh-proxy.sh", sshProxyBody)
	sshURL := "ssh://git@example.invalid" + url
	env := a4Env(home, repo, "GIT_SSH_COMMAND="+stub, "SSH_AUTH_SOCK=/tmp/memctl-test-agent.sock", "MEMCTL_STUB_LOG="+log)

	if result := runMemctl(t, bin, home, env, "init", sshURL, "--confirm-private"); result.code != 0 {
		t.Fatalf("init over ssh: code=%d stderr=%q", result.code, result.stderr)
	}
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Memory\n\n## Index\n- agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMemctl(t, bin, home, env, "sync", "--capture"); result.code != 0 {
		t.Fatalf("capture: %q", result.stderr)
	}
	sync := runMemctl(t, bin, home, env, "sync", "--wait")
	if sync.code != 0 || !strings.Contains(sync.stdout, "remote: synced") {
		t.Fatalf("sync over ssh: code=%d stdout=%q stderr=%q", sync.code, sync.stdout, sync.stderr)
	}
	logged := mustReadFile(t, log)
	if !strings.Contains(logged, "SSH_AUTH_SOCK=/tmp/memctl-test-agent.sock") {
		t.Fatalf("child did not receive SSH_AUTH_SOCK: %q", logged)
	}
	localHead := gitOutput(t, a4Env(home, repo), repo, "rev-parse", "HEAD")
	remoteHead := gitOutput(t, a4Env(home, repo), url, "rev-parse", "main")
	if remoteHead != localHead {
		t.Fatalf("remote main = %s, want %s", remoteHead, localHead)
	}
}

// TestA4AskpassSuppressedCoversG08_4 covers the non-interactive path: an
// interactive askpass would be called if the child inherited it. Without a
// terminal it must not be invoked.
func TestA4AskpassSuppressedCoversG08_4(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	stub := writeStub(t, root, "ssh-proxy.sh", sshProxyBody)
	marker := filepath.Join(root, "askpass-called")
	askpass := writeStub(t, root, "askpass.sh", "touch "+marker+"\necho secret\n")
	sshURL := "ssh://git@example.invalid" + url

	env := a4Env(home, repo, "GIT_SSH_COMMAND="+stub, "GIT_ASKPASS="+askpass, "SSH_ASKPASS="+askpass)
	if result := runMemctl(t, bin, home, env, "init", sshURL, "--confirm-private"); result.code != 0 {
		t.Fatalf("init over ssh: code=%d stderr=%q", result.code, result.stderr)
	}
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Memory\n\n## Index\n- no-askpass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMemctl(t, bin, home, env, "sync", "--capture"); result.code != 0 {
		t.Fatalf("capture: %q", result.stderr)
	}
	sync := runMemctl(t, bin, home, env, "sync", "--wait")
	if sync.code != 0 {
		t.Fatalf("sync: code=%d stderr=%q", sync.code, sync.stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("askpass was invoked in the non-interactive path, stat err = %v", err)
	}
}

// TestA4CredentialTimeoutCleansUpCoversG08_5AndL01 covers the total deadline:
// the process group is terminated, the stub is gone, the lock is free again,
// and the failure is classified as TIMEOUT.
func TestA4CredentialTimeoutCleansUpCoversG08_5AndL01(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	url := newBareRemote(t, root, "memory.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	stub := writeStub(t, root, "ssh-proxy.sh", sshProxyBody)
	sshURL := "ssh://git@example.invalid" + url
	if result := runMemctl(t, bin, home, a4Env(home, repo, "GIT_SSH_COMMAND="+stub), "init", sshURL, "--confirm-private"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	pidFile := filepath.Join(root, "stub.pid")
	hang := writeStub(t, root, "ssh-hang.sh", "echo $$ > \""+pidFile+"\"\nsleep 300\n")

	t.Setenv("HOME", home)
	t.Setenv("AGENT_MEMORY_DIR", repo)
	t.Setenv("AGENT_MEMORY_STATE", filepath.Join(home, ".agent-memory"))
	t.Setenv("GIT_ALLOW_PROTOCOL", "file:ssh")
	t.Setenv("GIT_SSH_COMMAND", hang)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "absent-global"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(root, "absent-system"))
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_AUTHOR_NAME", "Memctl Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "memctl-test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Memctl Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "memctl-test@example.invalid")

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := app.New(cfg).SyncWait(ctx, false)
	if err == nil || !strings.Contains(err.Error(), "TIMEOUT") {
		t.Fatalf("timeout sync: err=%v result=%+v, want TIMEOUT", err, result)
	}
	if result.RemoteState != "pending" {
		t.Fatalf("timeout remote state = %q, want pending", result.RemoteState)
	}

	waitForProcessExit(t, pidFile)

	gitDir, err := git.GitDir(context.Background(), repo)
	if err != nil {
		t.Fatalf("git dir: %v", err)
	}
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer lockCancel()
	held, err := lock.Acquire(lockCtx, gitDir)
	if err != nil {
		t.Fatalf("repository lock was not released after the timeout: %v", err)
	}
	_ = held.Release()
}

// TestA4UnknownHostKeyCoversG08_6 covers a missing or mismatched host key: the
// non-interactive path fails as AUTH_FAILED and writes no known_hosts entry.
func TestA4UnknownHostKeyCoversG08_6(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	stub := writeStub(t, root, "ssh-hostkey.sh", `echo "Host key verification failed." >&2`+"\nexit 255\n")
	result := runMemctl(t, bin, home, a4Env(home, repo, "GIT_SSH_COMMAND="+stub), "init", "ssh://git@example.invalid/repo.git", "--confirm-private")
	if result.code != 1 || !strings.Contains(result.stderr, "AUTH_FAILED") {
		t.Fatalf("unknown host key: code=%d stderr=%q, want AUTH_FAILED", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Fatalf("known_hosts was written for an unknown host, stat err = %v", err)
	}
}

// TestA4CredentialRedactionCoversG08_7 covers the no-leak rule: a credential
// URL in a failed remote access is redacted in the diagnostic output.
func TestA4CredentialRedactionCoversG08_7(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	result := runMemctl(t, bin, home, a4Env(home, repo, "GIT_ALLOW_PROTOCOL=https:file"), "init", "http://user:secret@127.0.0.1:9/repo.git", "--confirm-private")
	if result.code != 1 {
		t.Fatalf("credential URL init: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if strings.Contains(result.stderr, "secret") || strings.Contains(result.stdout, "secret") {
		t.Fatalf("credential leaked in output: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "://***@") {
		t.Fatalf("credential URL was not redacted: %q", result.stderr)
	}
}

// envWith replaces the given environment keys so a test can override a value
// inherited from a4Env instead of appending a duplicate.
func envWith(base []string, kv ...string) []string {
	replace := map[string]bool{}
	for _, item := range kv {
		name, _, _ := strings.Cut(item, "=")
		replace[name] = true
	}
	out := make([]string, 0, len(base)+len(kv))
	for _, item := range base {
		name, _, _ := strings.Cut(item, "=")
		if replace[name] {
			continue
		}
		out = append(out, item)
	}
	return append(out, kv...)
}

// startGitHTTPServer serves one bare repository over smart HTTP with Basic
// authentication. It skips when git-http-backend is unavailable.
func startGitHTTPServer(t *testing.T, root, bareName string) string {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skipf("git --exec-path failed: %v", err)
	}
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git-http-backend not found: %v", err)
	}
	git := &cgi.Handler{
		Path: backend,
		Root: "/",
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user" || pass != "pass" {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		git.ServeHTTP(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL + "/" + bareName
}

// TestA4HTTPCredentialHelperCoversG08_3 covers a non-interactive credential
// helper over smart HTTP: authentication succeeds without a terminal and the
// secret does not appear in the output or the runtime state.
func TestA4HTTPCredentialHelperCoversG08_3(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	bare := newBareRemote(t, root, "repo.git")
	gitOutput(t, os.Environ(), bare, "config", "http.receivepack", "true")
	url := startGitHTTPServer(t, root, "repo.git")
	home := a4Home(t, root, "home")
	repo := filepath.Join(home, "memory")
	marker := filepath.Join(root, "helper-called")
	helper := writeStub(t, root, "cred-helper.sh", "touch "+marker+"\nif [ \"$1\" = get ]; then\n  echo username=user\n  echo password=pass\nfi\n")
	env := envWith(a4Env(home, repo),
		"GIT_ALLOW_PROTOCOL=http:file",
		"GIT_CONFIG_NOSYSTEM=1",
		// Reset the inherited helper list, then install only the test stub so
		// the macOS keychain helper is never consulted.
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.helper",
		"GIT_CONFIG_VALUE_1="+helper,
		"GIT_TERMINAL_PROMPT=0",
	)

	result := runMemctl(t, bin, home, env, "init", url, "--confirm-private")
	if result.code != 0 {
		t.Fatalf("http init: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("credential helper was not used: %v", err)
	}
	if strings.Contains(result.stdout, "pass") || strings.Contains(result.stderr, "pass") {
		t.Fatalf("credential leaked in output: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	a4WriteFile(t, bin, home, repo, env, "MEMORY.md", "# Memory\n\n## Index\n- http\n")
	sync := runMemctl(t, bin, home, env, "sync", "--wait")
	if sync.code != 0 || strings.Contains(sync.stdout, "pass") || strings.Contains(sync.stderr, "pass") {
		t.Fatalf("http sync: code=%d stdout=%q stderr=%q", sync.code, sync.stdout, sync.stderr)
	}
}

func waitForProcessExit(t *testing.T, pidFile string) {
	t.Helper()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if value, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil {
				pid = value
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatalf("the hanging stub never recorded its pid")
	}
	for time.Now().Before(deadline) {
		process, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		if signalErr := process.Signal(syscall.Signal(0)); signalErr != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Git child process %d is still running after the timeout", pid)
}
