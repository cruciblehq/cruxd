package agent

import "github.com/cruciblehq/spec/compute"

// Sends each write as output on a fixed channel of an exec stream.
type execWriter struct {
	stream  *compute.ExecWriter // Stream the exec messages are written to.
	channel compute.Channel     // Channel the output messages are tagged with.
}

// Sends p as output on the writer's channel.
//
// Large writes are split across several messages (see [compute.ExecWriter.Output]).
func (e *execWriter) Write(p []byte) (int, error) {
	if err := e.stream.Output(e.channel, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
