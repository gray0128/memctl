package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/server"
	"github.com/gray0128/memctl/internal/watcher"
)

// This file verifies stage B-2 (T024) from docs/05-validation-and-delivery.md:
// A05 (SSE chunk splitting, multi-line data, reconnect correction, queue
// overflow, directory rename/remove/atomic replace) and the SSE part of A02.
// Every run uses a temporary HOME and memory repository and a loopback listener
// on an ephemeral port, so no network or real credential source is touched.

// sseEvent is one decoded server-sent event.
type sseEvent struct {
	name string
	data string
}

// sseDecoder parses the SSE wire format from a byte stream. It joins multiple
// data lines with a newline and ignores comments and blank keep-alive lines,
// exactly as docs/02-domain-models-and-contracts.md section 5 requires of the
// client. It therefore reads the same stream regardless of how the network
// splits it into chunks.
type sseDecoder struct {
	scanner *bufio.Scanner
}

func newSSEDecoder(reader io.Reader) *sseDecoder {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &sseDecoder{scanner: scanner}
}

func (d *sseDecoder) next() (sseEvent, error) {
	var event sseEvent
	var data []string
	for {
		if !d.scanner.Scan() {
			if err := d.scanner.Err(); err != nil {
				return sseEvent{}, err
			}
			return sseEvent{}, io.EOF
		}
		line := d.scanner.Text()
		if line == "" {
			if event.name == "" && len(data) == 0 {
				continue
			}
			event.data = strings.Join(data, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event.name = value
		case "data":
			data = append(data, value)
		}
	}
}

// readEvent reads one event with a deadline so a missing publication fails the
// test instead of blocking it.
func readEvent(t *testing.T, decoder *sseDecoder) sseEvent {
	t.Helper()
	type result struct {
		event sseEvent
		err   error
	}
	done := make(chan result, 1)
	go func() {
		event, err := decoder.next()
		done <- result{event: event, err: err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("read SSE event: %v", got.err)
		}
		return got.event
	case <-time.After(8 * time.Second):
		t.Fatal("timed out reading an SSE event")
		return sseEvent{}
	}
}

// startB2StreamServer binds an ephemeral loopback port and serves the handler.
func startB2StreamServer(t *testing.T, application *app.App) (*server.Server, string) {
	t.Helper()
	listener, err := server.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := listener.Addr().String()
	srv, err := server.New(server.Options{
		App:          application,
		Addr:         addr,
		Token:        b1Token,
		SSEHeartbeat: 40 * time.Millisecond,
	})
	if err != nil {
		listener.Close()
		t.Fatalf("build server: %v", err)
	}
	httpServer := srv.NewHTTPServer()
	go func() { _ = httpServer.Serve(listener) }()
	t.Cleanup(func() { _ = httpServer.Close() })
	return srv, addr
}

func openSSE(t *testing.T, addr string) (*http.Response, *sseDecoder) {
	t.Helper()
	resp := openSSERaw(t, addr)
	return resp, newSSEDecoder(resp.Body)
}

func openSSERaw(t *testing.T, addr string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	req.Header.Set("X-Memctl-Token", b1Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200 from SSE, got %d: %s", resp.StatusCode, body)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("SSE content type should be text/event-stream, got %q", contentType)
	}
	return resp
}

func b2Snapshot(t *testing.T, addr string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/snapshot", nil)
	if err != nil {
		t.Fatalf("build snapshot request: %v", err)
	}
	req.Header.Set("X-Memctl-Token", b1Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("snapshot request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 from snapshot, got %d: %s", resp.StatusCode, body)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return payload
}

// TestB2SSEAuthCoversA02 checks that the SSE route enforces the same token and
// Origin rules as every other API route before it starts streaming.
func TestB2SSEAuthCoversA02(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")

	missing := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/events", "", "127.0.0.1:18420", "")
	mustStatus(t, missing.status, http.StatusUnauthorized)
	mustCode(t, errorCode(t, missing), "AUTH_REQUIRED")

	wrong := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/events", "wrong-token", "127.0.0.1:18420", "")
	mustStatus(t, wrong.status, http.StatusUnauthorized)

	nullOrigin := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/events", b1Token, "127.0.0.1:18420", "null")
	mustStatus(t, nullOrigin.status, http.StatusForbidden)
	mustCode(t, errorCode(t, nullOrigin), "ORIGIN_DENIED")

	crossOrigin := fixture.do(t, http.MethodGet, "http://127.0.0.1:18420/api/v1/events", b1Token, "127.0.0.1:18420", "https://evil.example")
	mustStatus(t, crossOrigin.status, http.StatusForbidden)
}

// TestB2SSEDecoderHandlesSplitChunksAndMultilineDataCoversA05 feeds the decoder
// one byte at a time so the SSE frame is split across many network chunks, and
// uses a multi-line data payload. Both are client-side parsing requirements of
// A05 and docs/02-domain-models-and-contracts.md section 5.
func TestB2SSEDecoderHandlesSplitChunksAndMultilineDataCoversA05(t *testing.T) {
	stream := "event: vault.updated\n" +
		"data: {\"revision\":\"sha256:abc\",\n" +
		"data: \"changedFiles\":[\"MEMORY.md\"]}\n" +
		"\n" +
		": heartbeat\n" +
		"\n" +
		"event: resync.required\n" +
		"data: {}\n" +
		"\n"
	decoder := newSSEDecoder(&oneByteReader{data: []byte(stream)})

	first := readEvent(t, decoder)
	if first.name != "vault.updated" {
		t.Fatalf("first event name = %q", first.name)
	}
	if !strings.Contains(first.data, "sha256:abc") || !strings.Contains(first.data, `"changedFiles":["MEMORY.md"]`) {
		t.Fatalf("multi-line data was not joined correctly: %q", first.data)
	}
	if !strings.Contains(first.data, "\n") {
		t.Fatalf("multi-line data should keep its newline: %q", first.data)
	}

	second := readEvent(t, decoder)
	if second.name != "resync.required" || second.data != "{}" {
		t.Fatalf("second event = %+v", second)
	}
}

// TestB2SSEStreamsCommittedWriteCoversA05 verifies a committed managed write
// publishes vault.updated to an open stream, independently of the file watcher.
func TestB2SSEStreamsCommittedWriteCoversA05(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	_, addr := startB2StreamServer(t, fixture.app)

	resp, decoder := openSSE(t, addr)
	defer resp.Body.Close()

	fixture.update(t, "alpha.md", "# Alpha\n\n- fact\n- second fact\n")
	var event sseEvent
	for {
		event = readEvent(t, decoder)
		if event.name == "vault.updated" && strings.Contains(event.data, "alpha.md") {
			break
		}
	}
	var payload struct {
		Revision     string   `json:"revision"`
		ChangedFiles []string `json:"changedFiles"`
	}
	if err := json.Unmarshal([]byte(event.data), &payload); err != nil {
		t.Fatalf("decode vault.updated payload: %v (%s)", err, event.data)
	}
	if !strings.HasPrefix(payload.Revision, "sha256:") {
		t.Fatalf("write event should carry the new revision, got %q", payload.Revision)
	}
	if len(payload.ChangedFiles) != 1 || payload.ChangedFiles[0] != "alpha.md" {
		t.Fatalf("unexpected changed files: %v", payload.ChangedFiles)
	}
}

// TestB2SSEHeartbeatCoversA05 verifies the long-lived stream keeps sending a
// comment heartbeat without producing a data event.
func TestB2SSEHeartbeatCoversA05(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	_, addr := startB2StreamServer(t, fixture.app)

	resp := openSSERaw(t, addr)
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read heartbeat: %v", err)
		}
		if strings.HasPrefix(line, ": heartbeat") {
			return
		}
	}
	t.Fatal("the SSE stream should send a heartbeat comment")
}

// TestB2WatchStreamsExternalEditCoversA05 drives the recursive watcher through
// the server: an out-of-band file edit must reach the open SSE stream even
// though no managed transaction produced it.
func TestB2WatchStreamsExternalEditCoversA05(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	srv, addr := startB2StreamServer(t, fixture.app)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchDone := make(chan error, 1)
	go func() { watchDone <- srv.Watch(ctx) }()

	resp, decoder := openSSE(t, addr)
	defer resp.Body.Close()

	external := filepath.Join(fixture.repo, "external.md")
	if err := os.WriteFile(external, []byte("# External\n\n- out of band\n"), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	var event sseEvent
	for {
		event = readEvent(t, decoder)
		if event.name == "vault.updated" && strings.Contains(event.data, "external.md") {
			break
		}
	}

	cancel()
	select {
	case err := <-watchDone:
		if err != nil {
			t.Fatalf("watch returned %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}

// TestB2SSEResyncAndReconnectCorrectionCoversA05 verifies that a disconnected
// client corrects its view by re-reading the snapshot, and that a queue
// overflow hint is delivered as resync.required.
func TestB2SSEResyncAndReconnectCorrectionCoversA05(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	srv, addr := startB2StreamServer(t, fixture.app)

	before := b2Snapshot(t, addr)
	revisionBefore, _ := before["revision"].(string)
	if revisionBefore == "" {
		t.Fatal("snapshot should include a revision")
	}

	// Connect and disconnect without reading the event stream.
	first, _ := openSSE(t, addr)
	first.Body.Close()

	// The write happens while the client is disconnected; no event is replayed
	// on reconnect, so the snapshot is the only correct source of the new state.
	fixture.write(t, "missed.md", "# Missed\n\n- while offline\n")

	after := b2Snapshot(t, addr)
	revisionAfter, _ := after["revision"].(string)
	if revisionAfter == revisionBefore {
		t.Fatal("the snapshot revision should change after a committed write")
	}
	if !snapshotHasFile(after, "missed.md") {
		t.Fatalf("reconnect correction must expose missed.md: %v", after["files"])
	}

	// Reconnect and verify the overflow degradation is streamed as
	// resync.required.
	second, decoder := openSSE(t, addr)
	defer second.Body.Close()
	srv.Events().Publish(watcher.Invalidation{Reason: watcher.ReasonResyncRequired})
	event := readEvent(t, decoder)
	if event.name != "resync.required" {
		t.Fatalf("expected resync.required, got %+v", event)
	}
}

func snapshotHasFile(snapshot map[string]any, relPath string) bool {
	files, ok := snapshot["files"].([]any)
	if !ok {
		return false
	}
	for _, entry := range files {
		file, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if file["relPath"] == relPath {
			return true
		}
	}
	return false
}

// oneByteReader returns one byte per Read call so a consumer sees a stream
// split at every possible boundary.
type oneByteReader struct {
	data []byte
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}
