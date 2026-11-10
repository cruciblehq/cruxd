package agent

import (
	"errors"
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/crex"
)

var (
	errUnexpectedContentType = crex.New("unexpected content type")
	errMissingContainerID    = crex.New("missing container id")
	errContainerNotFound     = crex.New("container not found")
)

// Writes an empty 200 response for an operation that returns no body.
//
// Always returns nil. The return value keeps the signature in line with the
// other response helpers.
func (a *Agent) ok(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusOK)
	return nil
}

// Writes an operation failure as a [compute.Error] payload at 200.
//
// Returns the error when the payload cannot be written.
func (a *Agent) failOp(w http.ResponseWriter, code compute.ErrorCode, message string) error {
	return a.encode(w, compute.MediaTypeError, http.StatusOK, compute.Error{
		Code:    code,
		Message: message,
	})
}

// Classifies err and writes either an operation Error payload or a transport 500.
//
// A recognizable operation failure is reported as an Error payload; an
// unexpected fault is a transport 500. Returns the error when the payload
// cannot be written.
func (a *Agent) failErr(w http.ResponseWriter, err error) error {
	if code, ok := classify(err); ok {
		return a.failOp(w, code, err.Error())
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
	return nil
}

// Maps a runtime error to a spec error code when its condition is recognizable.
//
// The runtime tags recoverable conditions with [compute] sentinels. An
// unrecognized error is treated as an unexpected fault by the caller.
func classify(err error) (compute.ErrorCode, bool) {
	switch {
	case errors.Is(err, compute.ErrNotFound):
		return compute.ErrorCodeNotFound, true
	case errors.Is(err, compute.ErrAlreadyStarted):
		return compute.ErrorCodeConflict, true
	case errors.Is(err, compute.ErrNotStarted):
		return compute.ErrorCodePreconditionFailed, true
	}
	return "", false
}
