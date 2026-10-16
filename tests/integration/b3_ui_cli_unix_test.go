//go:build unix

package integration_test

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This file verifies the `memctl ui` command end to end for stage B-3 subtask 1
// (T025): the command binds the loopback address, serves the embedded assets
// from a binary copied into an empty HOME, injects the session token, enforces
// read-only mode and shuts down on SIGTERM without an open SSE stream blocking
// it. It uses a temporary HOME and memory repository.

var uiURLPattern = regexp.MustCompile(`http://127\.0\.0\.1:\d+/`)

func readUIStartupLine(t *testing.T, stdout io.Reader) string {
	t.Helper()
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read ui startup line: %v", err)
	}
	return strings.TrimSpace(line)
}

func waitForUI(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK {
				return string(body)
			}
			lastErr = readErr
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("ui did not become ready at %s: %v", url, lastErr)
	return ""
}

func drain(t *testing.T, response *http.Response) {
	t.Helper()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("drain response body: %v", err)
	}
	response.Body.Close()
}

func TestB3UICommandEndToEndCoversR01A02A03(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(home, "memory")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create home: %v", err)
	}
	// R01: only the binary is copied into the isolated HOME; the embedded UI
	// assets must still load.
	copied := filepath.Join(home, "memctl")
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read built binary: %v", err)
	}
	if err := os.WriteFile(copied, data, 0o755); err != nil {
		t.Fatalf("copy binary into the empty home: %v", err)
	}
	env := a2Env(home, repo, true)
	if result := runMemctl(t, copied, home, env, "init"); result.code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.code, result.stderr)
	}

	cmd := exec.Command(copied, "ui", "--no-open", "--readonly", "--port", "0")
	cmd.Dir = home
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ui: %v", err)
	}
	exited := false
	defer func() {
		if !exited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
		}
	}()

	line := readUIStartupLine(t, stdout)
	url := uiURLPattern.FindString(line)
	if url == "" {
		t.Fatalf("ui startup line has no loopback URL: %q", line)
	}
	if !strings.Contains(line, "read-only: true") {
		t.Fatalf("ui startup line must report read-only mode: %q", line)
	}

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
	body := waitForUI(t, client, url)
	match := uiTokenPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("served page has no injected session token:\n%s", body)
	}
	token := match[1]

	// A02: the injected token authenticates the API; a missing token is rejected.
	authorized, err := http.NewRequest(http.MethodGet, url+"api/v1/snapshot", nil)
	if err != nil {
		t.Fatalf("build snapshot request: %v", err)
	}
	authorized.Header.Set("X-Memctl-Token", token)
	response, err := client.Do(authorized)
	if err != nil {
		t.Fatalf("authorized snapshot: %v", err)
	}
	drain(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized snapshot status = %d, want 200", response.StatusCode)
	}

	anonymous, err := client.Get(url + "api/v1/snapshot")
	if err != nil {
		t.Fatalf("anonymous snapshot: %v", err)
	}
	drain(t, anonymous)
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous snapshot status = %d, want 401", anonymous.StatusCode)
	}

	// A03: --readonly rejects a write even with a valid token.
	write, err := http.NewRequest(http.MethodPost, url+"api/v1/snapshot", nil)
	if err != nil {
		t.Fatalf("build write request: %v", err)
	}
	write.Header.Set("X-Memctl-Token", token)
	response, err = client.Do(write)
	if err != nil {
		t.Fatalf("read-only write: %v", err)
	}
	drain(t, response)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only write status = %d, want 403", response.StatusCode)
	}

	// A long-lived SSE stream must not block shutdown: the base context is
	// cancelled before waiting, so the server exits promptly.
	sseRequest, err := http.NewRequest(http.MethodGet, url+"api/v1/events", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	sseRequest.Header.Set("X-Memctl-Token", token)
	sseClient := &http.Client{}
	sseResponse, err := sseClient.Do(sseRequest)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	defer sseResponse.Body.Close()
	if sseResponse.StatusCode != http.StatusOK {
		t.Fatalf("SSE stream status = %d, want 200", sseResponse.StatusCode)
	}

	// Shut down cleanly on SIGTERM.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal ui: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		exited = true
		if err != nil {
			t.Fatalf("ui exited with %v (stderr=%q)", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("ui did not exit after SIGTERM (stderr=%q)", stderr.String())
	}
}
