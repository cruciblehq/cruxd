//go:build linux

package compute

import (
	"io"

	"github.com/containerd/containerd/v2/pkg/cio"
)

// Pipe-based exec I/O for Linux.
//
// Uses the containerd shim's native FIFO support to stream stdout and stderr
// live to the caller's writers via goroutines managed by [cio.NewCreator].
// FIFOs are created on the host filesystem, so there is no VM boundary issue.
type pipeExecIO struct {
	cioCreator cio.Creator // Creator used by task.Exec to connect shim output to the caller's writers.
}

// Creates a pipe-based [execIO] that streams output live to stdout and stderr.
//
// Nil writers are replaced with [io.Discard] so the shim always has a valid sink.
func newExecIO(stdout, stderr io.Writer) execIO {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &pipeExecIO{
		cioCreator: cio.NewCreator(cio.WithStreams(nil, stdout, stderr)),
	}
}

// Returns the [cio.Creator] that streams exec output live to the caller's writers.
func (p *pipeExecIO) creator() (cio.Creator, error) {
	return p.cioCreator, nil
}

// Waits for the in-flight forwarding goroutines to drain, then closes the I/O.
//
// Must be called before deleting the process to guarantee all output has been
// delivered to the writers.
func (p *pipeExecIO) flush(pio cio.IO) {
	if pio == nil {
		return
	}
	pio.Wait()
	pio.Close()
}

// No-op; the containerd cio layer manages FIFO lifecycle.
func (p *pipeExecIO) close() {
	/* No-op */
}
