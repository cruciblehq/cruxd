package agent

import (
	"net/http"

	"github.com/cruciblehq/spec/compute"
)

// Wraps each write into an ExecOutput message on a fixed channel.
type execWriter struct {
	a       *Agent              // reference to the agent to use for writing exec messages
	w       http.ResponseWriter // response writer to send exec messages to
	flusher http.Flusher        // flusher to flush the response writer after each message
	channel compute.Channel     // channel to tag the exec output messages with
}

// Emits p as a single ExecOutput message.
func (e *execWriter) Write(p []byte) (int, error) {
	msg := compute.ExecResponse{
		Type:    compute.ExecOutput,
		Channel: e.channel,
		Data:    p,
	}
	if err := e.a.writeExecMessage(e.w, e.flusher, msg); err != nil {
		return 0, err
	}
	return len(p), nil
}
