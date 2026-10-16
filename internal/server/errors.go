package server

import (
	"encoding/json"
	"net/http"

	"github.com/gray0128/memctl/internal/memerr"
)

// errorBody is the documented error envelope of
// docs/02-domain-models-and-contracts.md section 4.3. Optional fields are only
// present when the application service has them.
type errorBody struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	Retryable     bool   `json:"retryable"`
	TransactionID string `json:"transactionId,omitempty"`
	Recovery      string `json:"recovery,omitempty"`
	CommittedHash string `json:"committedHash,omitempty"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

// writeAppError converts a contract error into the documented HTTP status and
// code. Unknown errors are reported as IO_FAILED/500 rather than leaking an
// implementation detail.
func (s *Server) writeAppError(w http.ResponseWriter, err error) {
	code := memerr.IOFailed
	message := "the request could not be completed"
	if contractErr, ok := memerr.As(err); ok {
		code = contractErr.Code
		message = contractErr.Message
	} else if err != nil {
		message = err.Error()
	}
	writeError(w, statusFor(code), string(code), message, retryable(code))
}

func writeError(w http.ResponseWriter, status int, code, message string, retryable bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(errorResponse{Error: errorBody{
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}})
}

func statusFor(code memerr.Code) int {
	switch code {
	case memerr.InvalidArgument, memerr.InvalidPath:
		return http.StatusBadRequest
	case memerr.NotFound, memerr.NoRepo:
		return http.StatusNotFound
	case memerr.RemoteNotApproved:
		return http.StatusForbidden
	case memerr.ExternalChanges, memerr.OperationActive, memerr.ConflictStale,
		memerr.RecoveryRequired, memerr.ContentConflict:
		return http.StatusConflict
	case memerr.RevisionMismatch:
		return http.StatusPreconditionFailed
	case memerr.LimitExceeded:
		return http.StatusRequestEntityTooLarge
	case memerr.Unsupported:
		return http.StatusUnprocessableEntity
	case memerr.PreconditionRequired:
		return http.StatusPreconditionRequired
	case memerr.Busy, memerr.NetworkUnavailable, memerr.Timeout:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func retryable(code memerr.Code) bool {
	switch code {
	case memerr.Busy, memerr.NetworkUnavailable, memerr.Timeout:
		return true
	default:
		return false
	}
}
