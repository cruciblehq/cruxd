package server

import (
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/httpx"
)

// Runs a command in a container and streams its output.
//
// Output chunks are ExecOutput messages tagged with their channel. The stream
// ends with ExecExit (with the command's exit code), or an ExecError when the
// command could not be run. Messages are framed by [agent.ExecWriter] and
// flushed as they are produced.
func (s *Server) exec(w http.ResponseWriter, r *http.Request) {
	var req agent.ExecRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeExecRequest, agent.BodyFormat, &req); err != nil {
		http.Error(w, "invalid exec request", http.StatusBadRequest)
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}

	w.Header().Set(httpx.HeaderContentType, agent.MediaTypeExecResponse.String())
	w.WriteHeader(http.StatusOK)

	stream := agent.NewExecWriter(w)
	stdout := &execWriter{stream: stream, channel: agent.ChannelStdout}
	stderr := &execWriter{stream: stream, channel: agent.ChannelStderr}

	code, err := c.Exec(r.Context(), stdout, stderr, req.Command, req.Options)
	_ = stream.Send(terminal(code, err))
}

// Builds the terminal exec message for the outcome of a command.
//
// When the command runs, it returns an exit code. A command that could not
// run returns an error, classified when its condition is recognizable and
// an internal error otherwise.
func terminal(code int, err error) agent.ExecResponse {
	if err == nil {
		return agent.ExecResponse{Type: agent.ExecExit, Code: code}
	}
	errCode, ok := classify(err)
	if !ok {
		errCode = agent.ErrorCodeInternalError
	}
	return agent.ExecResponse{
		Type:  agent.ExecError,
		Error: &agent.Error{Code: errCode, Message: err.Error()},
	}
}
