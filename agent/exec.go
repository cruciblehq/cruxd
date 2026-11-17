package agent

import (
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/httpx"
)

// Runs a command in a container and streams its output.
//
// Output chunks are ExecOutput messages tagged with their channel. The stream
// ends with ExecExit (with the command's exit code), or an ExecError when the
// command could not be run. Messages are framed by [compute.ExecWriter] and
// flushed as they are produced.
func (a *Agent) exec(w http.ResponseWriter, r *http.Request) {
	var req compute.ExecRequest
	if err := httpx.DecodeRequest(r, compute.MediaTypeExecRequest, compute.BodyFormat, &req); err != nil {
		http.Error(w, "invalid exec request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}

	w.Header().Set(httpx.HeaderContentType, compute.MediaTypeExecResponse.String())
	w.WriteHeader(http.StatusOK)

	stream := compute.NewExecWriter(w)
	stdout := &execWriter{stream: stream, channel: compute.ChannelStdout}
	stderr := &execWriter{stream: stream, channel: compute.ChannelStderr}

	code, err := c.Exec(r.Context(), stdout, stderr, req.Command, req.Options)
	_ = stream.Send(terminal(code, err))
}

// Builds the terminal exec message for the outcome of a command.
//
// When the command runs, it returns an exit code. A command that could not
// run returns an error, classified when its condition is recognizable and
// an internal error otherwise.
func terminal(code int, err error) compute.ExecResponse {
	if err == nil {
		return compute.ExecResponse{Type: compute.ExecExit, Code: code}
	}
	errCode, ok := classify(err)
	if !ok {
		errCode = compute.ErrorCodeInternalError
	}
	return compute.ExecResponse{
		Type:  compute.ExecError,
		Error: &compute.Error{Code: errCode, Message: err.Error()},
	}
}
