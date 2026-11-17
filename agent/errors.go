package agent

import (
	"errors"
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/httpx"
)

var (
	errMissingContainerID = crex.New("missing container id")
	errContainerNotFound  = crex.New("container not found")
)

// Writes an empty 200 response for an operation that returns no body.
//
// Always returns nil. The return value keeps the signature in line with the
// other response helpers.
func (a *Agent) ok(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusOK)
	return nil
}

// Writes an operation failure as a [compute.Error] payload.
//
// The status follows the code (see [compute.ErrorCode.HTTPStatus]). Returns
// the error when the payload cannot be written.
func (a *Agent) failOp(w http.ResponseWriter, code compute.ErrorCode, message string) error {
	return httpx.Write(w, compute.MediaTypeError, compute.BodyFormat, code.HTTPStatus(), compute.Error{
		Code:    code,
		Message: message,
	})
}

// Classifies err and writes it as an operation Error payload.
//
// An error that cannot be classified is reported as an internal error.
// Returns the error when the payload cannot be written.
func (a *Agent) failErr(w http.ResponseWriter, err error) error {
	code, ok := classify(err)
	if !ok {
		code = compute.ErrorCodeInternalError
	}
	return a.failOp(w, code, err.Error())
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
