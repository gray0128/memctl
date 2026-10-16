package server

import (
	"encoding/json"
	"errors"
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
// section 2.2 as returned by POST /api/v1/sync.
type syncSummaryDTO struct {
	RepoID        string `json:"repoId"`
	Branch        string `json:"branch,omitempty"`
	Head          string `json:"head,omitempty"`
	HasRemote     bool   `json:"hasRemote"`
	HasUpstream   bool   `json:"hasUpstream"`
	RemoteState   string `json:"remoteState"`
	LocalState    string `json:"localState"`
	AheadCount    int    `json:"aheadCount"`
	BehindCount   int    `json:"behindCount"`
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

// deviceLabel reads the normalized device label for the commit source trailer.
func deviceLabel() string { return hookio.ResolveDevice(os.Getenv) }

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
	default:
		s.writeAppError(w, memerr.New(memerr.PreconditionRequired,
			"writing a file requires If-Match with a fileRevision, or If-None-Match: * to create it"))
		return
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
	fileRevision, ok := ifMatch(r)
	if !ok {
		s.writeAppError(w, memerr.New(memerr.PreconditionRequired,
			"patching an entry requires If-Match with the fileRevision it was read from"))
		return
	}
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
	fileRevision, ok := ifMatch(r)
	if !ok {
		s.writeAppError(w, memerr.New(memerr.PreconditionRequired,
			"deleting an entry requires If-Match with the fileRevision it was read from"))
		return
	}
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
	result, err := s.app.SyncWait(r.Context(), false)
	if err != nil {
		s.writeAppError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, syncSummaryDTO{
		RepoID:        result.RepoID,
		Branch:        result.Branch,
		Head:          result.Head,
		HasRemote:     result.HasRemote,
		HasUpstream:   result.HasUpstream,
		RemoteState:   result.RemoteState,
		LocalState:    result.LocalState,
		AheadCount:    result.Ahead,
		BehindCount:   result.Behind,
		LastAttemptAt: result.LastAttemptAt,
		LastSuccessAt: result.LastSuccessAt,
		ReasonCode:    result.ReasonCode,
		ConflictID:    result.ConflictID,
		OperationID:   result.OperationID,
	})
}

// decodeBody reads one bounded JSON body and rejects undeclared fields.
func decodeBody(r *http.Request, target any) error {
	if r.Body == nil {
		return memerr.New(memerr.InvalidArgument, "a JSON request body is required")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return memerr.New(memerr.LimitExceeded, "the request body exceeds the %d byte limit", tooLarge.Limit)
		}
		return memerr.Wrap(memerr.InvalidArgument, err, "the request body is not valid JSON for this route")
	}
	return nil
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
