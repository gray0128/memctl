package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/watcher"
)

const (
	// DefaultSSEQueue is the per-client bound of the SSE event queue.
	DefaultSSEQueue = watcher.DefaultQueue
	// DefaultSSEHeartbeat is the comment interval that keeps a long-lived SSE
	// connection alive across local proxies and client idle timeouts.
	DefaultSSEHeartbeat = 15 * time.Second
)

// vaultUpdatedData is the documented payload of one vault.updated event. It is
// only a hint; the client re-reads the snapshot after receiving it.
type vaultUpdatedData struct {
	Revision     string   `json:"revision,omitempty"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
}

// dreamUpdatedData is the documented payload of one dream.updated event.
type dreamUpdatedData struct {
	ProposalID string `json:"proposalId"`
	Status     string `json:"status"`
}

// Events returns the invalidation bus behind GET /api/v1/events. A caller that
// runs the recursive file watcher publishes external-edit hints into it.
func (s *Server) Events() *watcher.Bus { return s.events }

// Watch recursively watches the configured repository until ctx is cancelled,
// publishing external-edit invalidations into the SSE bus. It returns the
// watcher's construction or runtime error; a cancelled context returns nil.
func (s *Server) Watch(ctx context.Context) error {
	repoWatcher, err := watcher.New(s.app.RepoPath(), s.events)
	if err != nil {
		return err
	}
	defer repoWatcher.Close()
	return repoWatcher.Watch(ctx)
}

// handleEvents streams server-sent invalidation hints. The security middleware
// requires X-Memctl-Token for this route exactly like every other API route.
// The stream never carries authoritative state: a reconnect or a
// resync.required notification makes the client re-read the snapshot.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, string(memerr.IOFailed),
			"this response writer cannot stream server-sent events", false)
		return
	}

	// Subscribe before the first flush so an invalidation published after the
	// client sees the response headers is not lost by the subscribe race.
	sub := s.events.Subscribe()
	defer sub.Close()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	heartbeat := time.NewTicker(s.sseHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case hint := <-sub.Hints():
			if err := writeSSEEvent(w, hint.Reason, invalidationPayload(hint)); err != nil {
				return
			}
			flusher.Flush()
		case <-sub.Resync():
			if err := writeSSEEvent(w, watcher.ReasonResyncRequired, nil); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes one SSE frame. A payload containing newlines is split
// into several data lines as the SSE wire format requires.
func writeSSEEvent(w io.Writer, name string, payload []byte) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
		return err
	}
	if len(payload) > 0 {
		for _, line := range strings.Split(string(payload), "\n") {
			if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func vaultUpdatedPayload(hint watcher.Invalidation) []byte {
	payload, err := json.Marshal(vaultUpdatedData{
		Revision:     hint.Revision,
		ChangedFiles: hint.ChangedFiles,
	})
	if err != nil {
		return nil
	}
	return payload
}

// invalidationPayload selects the documented payload for one event reason.
func invalidationPayload(hint watcher.Invalidation) []byte {
	if hint.Reason != watcher.ReasonDreamUpdated {
		return vaultUpdatedPayload(hint)
	}
	payload, err := json.Marshal(dreamUpdatedData{ProposalID: hint.ProposalID, Status: hint.Status})
	if err != nil {
		return nil
	}
	return payload
}
