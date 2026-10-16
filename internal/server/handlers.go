package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/memerr"
)

// handleSnapshot returns the VaultSnapshot. A repository in a protected state
// has no complete snapshot yet, which the contract reports as BUSY rather than
// as a transaction-intermediate tree.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.app.Snapshot(r.Context())
	if err != nil {
		if contractErr, ok := memerr.As(err); ok && busyCode(contractErr.Code) {
			writeError(w, http.StatusServiceUnavailable, string(memerr.Busy),
				"a managed transaction, conflict or Git operation is active; no complete snapshot is available yet", true)
			return
		}
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, snapshot)
}

// handleFile returns one file's metadata and raw text with its ETag.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	file, err := s.app.ReadFile(r.Context(), relPath)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+file.FileRevision+`"`)
	writeJSON(w, map[string]any{
		"file": app.MemoryFileDTO{
			RelPath:      file.RelPath,
			FileRevision: file.FileRevision,
			Kind:         string(file.Kind),
			SizeBytes:    file.SizeBytes,
			UpdatedAt:    file.ModTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Editable:     file.Editable,
		},
		"rawContent": file.RawContent,
	})
}

// handleEntries returns one page of parsed entries.
func (s *Server) handleEntries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := intParam(query.Get("limit"))
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	offset, err := intParam(query.Get("offset"))
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	page, err := s.app.ListEntries(r.Context(), app.EntryQuery{
		Q:      query.Get("q"),
		Agent:  query.Get("agent"),
		Device: query.Get("device"),
		File:   query.Get("file"),
		From:   query.Get("from"),
		To:     query.Get("to"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, page)
}

// handleAudit returns one page of commits.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := intParam(query.Get("limit"))
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	page, err := s.app.Audit(r.Context(), app.AuditQuery{
		Limit:  limit,
		Agent:  query.Get("agent"),
		Before: query.Get("before"),
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, page)
}

// handleDoctor returns the read-only diagnosis report.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	report, err := s.app.Doctor(r.Context())
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, report)
}

// handleConflict returns the active conflict snapshot, or 204 when none exists.
func (s *Server) handleConflict(w http.ResponseWriter, r *http.Request) {
	conflict, ok, err := s.app.ConflictState(r.Context())
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, conflict)
}

func intParam(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, memerr.New(memerr.InvalidArgument, "query parameter is not an integer: %q", value)
	}
	return parsed, nil
}

func busyCode(code memerr.Code) bool {
	switch code {
	case memerr.Busy, memerr.OperationActive, memerr.RecoveryRequired:
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
