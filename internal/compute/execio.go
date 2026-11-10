package compute

import "github.com/containerd/containerd/v2/pkg/cio"

// Manages I/O routing for a single exec invocation.
//
// Abstracts the platform-specific strategy for connecting the containerd shim's
// output streams to the caller's writers. FIFOs do not work across the macOS VM
// kernel boundary over virtiofs, so on darwin output is collected in files on
// the host and forwarded after the process exits. On Linux, FIFOs work natively
// and output is streamed live.
type execIO interface {

	// Returns the [cio.Creator] to pass to task.Exec.
	//
	// Allocates any platform resources needed to collect output: FIFOs on
	// Linux, log files on darwin and other platforms. Called once per exec
	// invocation before the process starts.
	creator() (cio.Creator, error)

	// Drains output to the caller's writers after the process exits.
	//
	// pio is the value returned by process.IO() and may be nil. Must be called
	// after the process exits and before process.Delete so that all output is
	// forwarded before resources are released. On pipe-based implementations
	// this blocks until the shim-to-writer forwarding goroutines have drained.
	// On file-based implementations this reads the accumulated log files and
	// forwards their contents to the writers.
	flush(pio cio.IO)

	// Releases resources allocated by construction and by [execIO.creator].
	//
	// Must be called after flush to clean up any resources allocated for a
	// given exec invocation. It's safe to call even if creator returned an
	// error. On file-based implementations, removes the log files; on
	// pipe-based implementations this is a no-op.
	close()
}
