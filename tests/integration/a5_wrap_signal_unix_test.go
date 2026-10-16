//go:build unix

package integration_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestA5WrapForwardsTerminationSignalsCoversH05 covers P2-6: a termination
// signal sent to `memctl wrap` reaches the wrapped process, which keeps its own
// shutdown behavior and exit status.
func TestA5WrapForwardsTerminationSignalsCoversH05(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")

	ready := filepath.Join(root, "child-ready")
	terminated := filepath.Join(root, "child-terminated")
	child := writeStub(t, root, "child.sh",
		"trap 'echo term > \""+terminated+"\"; exit 7' TERM\n"+
			"echo ready > \""+ready+"\"\n"+
			"while true; do sleep 0.2; done\n")

	cmd := exec.Command(bin, "wrap", "--", child)
	cmd.Dir = home
	cmd.Env = env
	cmd.Stdout = new(bytes.Buffer)
	cmd.Stderr = new(bytes.Buffer)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start wrap: %v", err)
	}
	waitForFileExists(t, ready, 10*time.Second)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal wrap: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
			t.Fatalf("wrap exit = %v, want the wrapped process exit code 7", err)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("wrap did not finish after the termination signal was forwarded")
	}
	if _, err := os.Stat(terminated); err != nil {
		t.Fatalf("the wrapped process did not receive the termination signal: %v", err)
	}
}

func waitForFileExists(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s was not created within %s", path, timeout)
}
