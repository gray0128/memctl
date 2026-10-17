package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file implements the managed write routes of stage C-1. Every route calls
// the same application service as the CLI, so the UI and the CLI share one lock
// and one version protocol. The HTTP layer only validates the request shape,
// converts preconditions into baselines and maps contract errors to responses.

// mutationResultDTO is the JSON shape of app.MutationResult.
type mutationResultDTO struct {
	TransactionID string   `json:"transactionId,omitempty"`
	Revision      string   `json:"revision,omitempty"`
	Commit        string   `json:"commit,omitempty"`
	ChangedFiles  []string `json:"changedFiles"`
	SyncState     string   `json:"syncState"`
}

func toMutationResultDTO(result app.MutationResult) mutationResultDTO {
	changed := result.ChangedFiles
	if changed == nil {
		changed = []string{}
	}
	return mutationResultDTO{
		TransactionID: result.TransactionID,
		Revision:      result.Revision,
		Commit:        result.Commit,
		ChangedFiles:  changed,
		SyncState:     result.SyncState,
	}
}

// entryResultDTO is the response of the entry create and patch routes.
type entryResultDTO struct {
	Entry  app.MemoryEntryDTO `json:"entry"`
	Result mutationResultDTO  `json:"result"`
}

// syncSummaryDTO is the SyncSummary of docs/02-domain-models-and-contracts.md
// section 2.2 as returned by POST /api/v1/sync. Unknown ahead/behind counts
// stay null instead of pretending to be zero, matching SyncStateDTO.
type syncSummaryDTO struct {
	RepoID        string `json:"repoId"`
	Branch        string `json:"branch,omitempty"`
	Head          string `json:"head,omitempty"`
	HasRemote     bool   `json:"hasRemote"`
	HasUpstream   bool   `json:"hasUpstream"`
	RemoteState   string `json:"remoteState"`
	LocalState    string `json:"localState"`
	AheadCount    *int   `json:"aheadCount"`
	BehindCount   *int   `json:"behindCount"`
	LastAttemptAt string `json:"lastAttemptAt,omitempty"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	ReasonCode    string `json:"reasonCode,omitempty"`
	ConflictID    string `json:"conflictId,omitempty"`
	OperationID   string `json:"operationId,omitempty"`
}

// putFileBody is the PUT /api/v1/files/{path...} request. Undeclared fields are
// rejected so a typo never becomes a silent no-op.
type putFileBody struct {
	RawContent string `json:"rawContent"`
}

// moveFileBody is the POST /api/v1/files:move request.
type moveFileBody struct {
	FromPath     string `json:"fromPath"`
	ToPath       string `json:"toPath"`
	BaseRevision string `json:"baseRevision"`
}

// createEntryBody is the POST /api/v1/entries request.
type createEntryBody struct {
	FilePath         string   `json:"filePath"`
	Heading          string   `json:"heading"`
	FactText         string   `json:"factText"`
	Sources          []string `json:"sources"`
	ExpectedRevision string   `json:"expectedRevision"`
}

// patchEntryBody is the PATCH /api/v1/entries/{id} request.
type patchEntryBody struct {
	FilePath       string `json:"filePath"`
	FactText       string `json:"factText"`
	MoveToFilePath string `json:"moveToFilePath"`
	BaseRevision   string `json:"baseRevision"`
}

// doctorFixBody is the POST /api/v1/doctor:fix request.
type doctorFixBody struct {
	IssueCodes   []string `json:"issueCodes"`
	BaseRevision string   `json:"baseRevision"`
}

// resolvedFileBody is one entry of the conflict:resolve request.
type resolvedFileBody struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	RawContent string `json:"rawContent"`
}

// conflictResolveBody is the POST /api/v1/conflict:resolve request.
type conflictResolveBody struct {
	ConflictID    string             `json:"conflictId"`
	BaseRevision  string             `json:"baseRevision"`
	ResolvedFiles []resolvedFileBody `json:"resolvedFiles"`
}

// conflictAbortBody is the POST /api/v1/conflict:abort request.
type conflictAbortBody struct {
	ConflictID string `json:"conflictId"`
}

// revertBody is the POST /api/v1/audit/{hash}/revert request.
type revertBody struct {
	BaseRevision string `json:"baseRevision"`
}

// resolveResultDTO is the shared response of the two conflict routes. It
// mirrors the CLI JSON shape: the completed step, the next stage when the replay
// stopped again, and the remote state. A nil Conflict means the replay finished.
type resolveResultDTO struct {
	ConflictID  string            `json:"conflictId,omitempty"`
	Head        string            `json:"head,omitempty"`
	RemoteState string            `json:"remoteState"`
	Conflict    *app.ConflictView `json:"conflict,omitempty"`
}

func toResolveResultDTO(result app.ResolveResult) resolveResultDTO {
	return resolveResultDTO{
		ConflictID:  result.ConflictID,
		Head:        result.Head,
		RemoteState: result.RemoteState,
		Conflict:    result.Conflict,
	}
}

// deviceLabel reads the normalized device label for the commit source trailer.
func deviceLabel() string { return hookio.ResolveDevice(os.Getenv) }

// handleConflictResolve applies one stage's resolved files through the same
// multi-stage state machine the CLI uses. A further conflict is a 200 response
// carrying the next snapshot; a stale stage is 409 CONFLICT_STALE.
func (s *Server) handleConflictResolve(w http.ResponseWriter, r *http.Request) {
	var body conflictResolveBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	resolved := make([]app.ResolvedFile, 0, len(body.ResolvedFiles))
	for _, file := range body.ResolvedFiles {
		resolved = append(resolved, app.ResolvedFile{
			Path:       file.Path,
			Action:     app.ChangeAction(file.Action),
			RawContent: file.RawContent,
		})
	}
	result, err := s.app.ResolveApply(r.Context(), false, body.ConflictID, body.BaseRevision, resolved)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toResolveResultDTO(result))
}

// handleConflictAbort aborts the owned replay and returns the restored state.
func (s *Server) handleConflictAbort(w http.ResponseWriter, r *http.Request) {
	var body conflictAbortBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	result, err := s.app.ResolveAbort(r.Context(), body.ConflictID)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toResolveResultDTO(result))
}

// handleAuditRevert reverts one single-parent commit as a new managed commit.
// A conflicting revert is aborted and verified by the application service
// before the caller sees CONTENT_CONFLICT.
func (s *Server) handleAuditRevert(w http.ResponseWriter, r *http.Request) {
	var body revertBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	result, err := s.app.Revert(r.Context(), r.PathValue("hash"), body.BaseRevision, "", "", deviceLabel())
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toMutationResultDTO(result))
}

// handlePutFile replaces or creates one managed file. If-Match binds an existing
// file's fileRevision; If-None-Match: * creates a new file. A request without
// either precondition is PRECONDITION_REQUIRED, and never changes a target.
func (s *Server) handlePutFile(w http.ResponseWriter, r *http.Request) {
	var body putFileBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	expected := ""
	switch match, ok := ifMatch(r); {
	case ok:
		expected = match
	case ifNoneMatchStar(r):
		expected = vault.AbsentRevision
	}
	result, err := s.app.PutFile(r.Context(), r.PathValue("path"), expected, body.RawContent, "", "", deviceLabel())
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toMutationResultDTO(result))
}

// handleMoveFile moves one managed file and rewrites the parsed links that
// resolved to it in the same transaction.
func (s *Server) handleMoveFile(w http.ResponseWriter, r *http.Request) {
	var body moveFileBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	result, err := s.app.MoveFile(r.Context(), app.MoveFileRequest{
		FromPath:     body.FromPath,
		ToPath:       body.ToPath,
		BaseRevision: body.BaseRevision,
		Device:       deviceLabel(),
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toMutationResultDTO(result))
}

// handleCreateEntry appends one entry, or creates the target file with it.
func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	var body createEntryBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	result, err := s.app.CreateEntry(r.Context(), app.CreateEntryRequest{
		FilePath:         body.FilePath,
		Heading:          body.Heading,
		FactText:         body.FactText,
		Sources:          body.Sources,
		ExpectedRevision: body.ExpectedRevision,
		Device:           deviceLabel(),
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, entryResultDTO{Entry: result.Entry, Result: toMutationResultDTO(result.Result)})
}

// handlePatchEntry rewrites one entry's fact text, optionally moving it to
// another file. If-Match binds the source file; a move also needs baseRevision.
func (s *Server) handlePatchEntry(w http.ResponseWriter, r *http.Request) {
	var body patchEntryBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	fileRevision, _ := ifMatch(r)
	result, err := s.app.PatchEntry(r.Context(), app.PatchEntryRequest{
		ID:             r.PathValue("id"),
		FilePath:       body.FilePath,
		FactText:       body.FactText,
		MoveToFilePath: body.MoveToFilePath,
		BaseRevision:   body.BaseRevision,
		FileRevision:   fileRevision,
		Device:         deviceLabel(),
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, entryResultDTO{Entry: result.Entry, Result: toMutationResultDTO(result.Result)})
}

// handleDeleteEntry removes one entry's line from the named file.
func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	fileRevision, _ := ifMatch(r)
	result, err := s.app.DeleteEntry(r.Context(), app.DeleteEntryRequest{
		ID:           r.PathValue("id"),
		FilePath:     r.URL.Query().Get("filePath"),
		FileRevision: fileRevision,
		Device:       deviceLabel(),
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSON(w, toMutationResultDTO(result))
}

// handleDoctorFix applies the deterministic diagnostic repairs.
func (s *Server) handleDoctorFix(w http.ResponseWriter, r *http.Request) {
	var body doctorFixBody
	if err := decodeBody(r, &body); err != nil {
		s.writeAppError(w, err)
		return
	}
	result, err := s.app.DoctorFix(r.Context(), app.DoctorFixRequest{
		IssueCodes:   body.IssueCodes,
		BaseRevision: body.BaseRevision,
		Device:       deviceLabel(),
	})
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	payload := struct {
		Report any                `json:"report"`
		Result *mutationResultDTO `json:"result,omitempty"`
	}{Report: result.Report}
	if result.Result != nil {
		dto := toMutationResultDTO(*result.Result)
		payload.Result = &dto
	}
	writeJSON(w, payload)
}

// handleSync performs one complete foreground synchronization and returns the
// documented SyncSummary. The remote lock and baseline checks are the same as
// the CLI path; the client polls GET /api/v1/snapshot for later state.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	// The route takes no input, so any body must be an empty JSON object.
	if err := decodeOptionalBody(r, &struct{}{}); err != nil {
		s.writeAppError(w, err)
		return
	}
	result, err := s.app.SyncWait(r.Context(), false)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	summary := syncSummaryDTO{
		RepoID:        result.RepoID,
		Branch:        result.Branch,
		Head:          result.Head,
		HasRemote:     result.HasRemote,
		HasUpstream:   result.HasUpstream,
		RemoteState:   result.RemoteState,
		LocalState:    result.LocalState,
		LastAttemptAt: result.LastAttemptAt,
		LastSuccessAt: result.LastSuccessAt,
		ReasonCode:    result.ReasonCode,
		ConflictID:    result.ConflictID,
		OperationID:   result.OperationID,
	}
	// A repository without an upstream has no meaningful ahead/behind counts;
	// zero would read as "in sync" instead of "unknown".
	if result.HasUpstream {
		ahead, behind := result.Ahead, result.Behind
		summary.AheadCount = &ahead
		summary.BehindCount = &behind
	}
	writeJSONStatus(w, http.StatusAccepted, summary)
}

// decodeBody reads one bounded JSON body and rejects undeclared fields. The
// body must contain exactly one JSON value: trailing bytes would bypass the
// documented request limit because MaxBytesReader only fails on a read.
func decodeBody(r *http.Request, target any) error {
	if r.Body == nil {
		return memerr.New(memerr.InvalidArgument, "a JSON request body is required")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return decodeBodyError(err)
	}
	return requireBodyEOF(decoder)
}

// requireBodyEOF rejects a second JSON value or trailing garbage after the
// first. The second read is what makes an oversized tail hit MaxBytesReader.
func requireBodyEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return memerr.New(memerr.LimitExceeded, "the request body exceeds the %d byte limit", tooLarge.Limit)
		}
	}
	return memerr.New(memerr.InvalidArgument, "the request body must contain exactly one JSON value")
}

// decodeBodyError maps one decoding failure to the contract error.
func decodeBodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return memerr.New(memerr.LimitExceeded, "the request body exceeds the %d byte limit", tooLarge.Limit)
	}
	return memerr.Wrap(memerr.InvalidArgument, err, "the request body is not valid JSON for this route")
}

// ifMatch returns the unquoted If-Match baseline.
func ifMatch(r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return "", false
	}
	if len(raw) >= 2 && strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		raw = raw[1 : len(raw)-1]
	}
	if raw == "" {
		return "", false
	}
	return raw, true
}

// ifNoneMatchStar reports the create-only precondition.
func ifNoneMatchStar(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("If-None-Match")) == "*"
}
