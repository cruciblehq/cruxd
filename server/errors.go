package server

import (
	"errors"
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/httpx"
)

var (
	errMissingContainerID = crex.New("missing container id")
	errContainerNotFound  = crex.New("container not found")
)

// Writes an operation failure as an [agent.Error] payload.
//
// The status follows the code (see [agent.ErrorCode.HTTPStatus]). Returns
// the error when the payload cannot be written.
func (s *Server) failOp(w http.ResponseWriter, code agent.ErrorCode, message string) error {
	return httpx.Write(w, agent.MediaTypeError, agent.BodyFormat, code.HTTPStatus(), agent.Error{
		Code:    code,
		Message: message,
	})
}

// Classifies err and writes it as an operation Error payload.
//
// An error that cannot be classified is reported as an internal error.
// Returns the error when the payload cannot be written.
func (s *Server) failErr(w http.ResponseWriter, err error) error {
	code, ok := classify(err)
	if !ok {
		code = agent.ErrorCodeInternalError
	}
	return s.failOp(w, code, err.Error())
}

// Maps an agent error to a spec error code when its condition is recognizable.
//
// The agent tags recoverable conditions with [agent] sentinels. An
// unrecognized error is treated as an unexpected fault by the caller.
func classify(err error) (agent.ErrorCode, bool) {
	switch {
	case errors.Is(err, agent.ErrNotFound):
		return agent.ErrorCodeNotFound, true
	case errors.Is(err, agent.ErrAlreadyStarted):
		return agent.ErrorCodeConflict, true
	case errors.Is(err, agent.ErrNotStarted):
		return agent.ErrorCodePreconditionFailed, true
	}
	return "", false
}
