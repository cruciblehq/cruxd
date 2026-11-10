package agent

import (
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

// Runs a command in a container and streams its output.
//
// Output chunks are ExecOutput messages tagged with their channel. The stream
// ends with ExecExit (with the command's exit code), or an ExecError when the
// command could not be run. Each message is a newline-terminated JSON object,
// flushed as it is produced.
func (a *Agent) exec(w http.ResponseWriter, r *http.Request) {
	var req compute.ExecRequest
	if err := a.decode(r, compute.MediaTypeExecRequest, &req); err != nil {
		http.Error(w, "invalid exec request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set(compute.HeaderContentType, compute.MediaTypeExecResponse.String())
	w.WriteHeader(http.StatusOK)

	stdout := &execWriter{a: a, w: w, flusher: flusher, channel: compute.ChannelStdout}
	stderr := &execWriter{a: a, w: w, flusher: flusher, channel: compute.ChannelStderr}

	code, err := c.Exec(r.Context(), stdout, stderr, req.Command, req.Options)
	_ = a.writeExecMessage(w, flusher, terminal(code, err))
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

// Encodes msg as one newline-terminated JSON object and flushes it.
//
// Returns the error when msg cannot be encoded or written.
func (a *Agent) writeExecMessage(w http.ResponseWriter, flusher http.Flusher, msg compute.ExecResponse) error {
	data, err := codec.Encode(msg, codec.JSON)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
