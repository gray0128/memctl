// Package memerr defines the stable error codes shared by the CLI, the
// application service and the repository accessors. The codes mirror
// docs/02-domain-models-and-contracts.md; they are part of the CLI and HTTP
// contract and must not be reworded per call site.
package memerr

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error class.
type Code string

const (
	InvalidArgument Code = "INVALID_ARGUMENT"
	InvalidPath     Code = "INVALID_PATH"
	NotFound        Code = "NOT_FOUND"
	NoRepo          Code = "NO_REPO"
	Unsupported     Code = "UNSUPPORTED"
	LimitExceeded   Code = "LIMIT_EXCEEDED"
	GitFailed       Code = "GIT_FAILED"
	IOFailed        Code = "IO_FAILED"
)

// Error carries a contract code together with a human-readable message.
type Error struct {
	Code    Code
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// New builds an error with a formatted message.
func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap builds an error that keeps the cause for errors.Is/errors.As while
// printing only the formatted message.
func Wrap(code Code, err error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Err: err}
}

// As reports whether err is or wraps a contract error.
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
