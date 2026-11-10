//go:build !linux

package compute

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/pkg/cio"

	"github.com/cruciblehq/utils-go/crypto"
)

const (

	// Subdirectory under os.UserCacheDir where task logs are written.
	logSubdir = "crux/logs"

	// Permission bits for the task log directory.
	logDirPerm = 0o700

	// Permission bits for task log files.
	logFilePerm = 0o600

	// Log file suffix for exec stdout.
	stdoutLogSuffix = "stdout.log"

	// Log file suffix for exec stderr.
	stderrLogSuffix = "stderr.log"

	// URI scheme used to pass log file paths to the containerd shim.
	logURIScheme = "file"
)

// File-based exec I/O for non-Linux platforms.
//
// FIFOs do not work across the macOS VM kernel boundary over virtiofs, so
// output is collected in regular files on the host and forwarded after the
// process exits. The same strategy is used on all other non-Linux platforms
// since os.UserCacheDir and file:// log URIs are universally available.
type fileExecIO struct {
	stdoutPath string    // Path to the log file for stdout.
	stderrPath string    // Path to the log file for stderr.
	stdout     io.Writer // Writer for stdout.
	stderr     io.Writer // Writer for stderr.
}

// Creates a file-based [execIO], writing output to stdout and stderr.
//
// A random name is generated internally to avoid collisions between concurrent
// exec invocations. Placeholder log files are created in the host cache directory.
// The caller must call [fileExecIO.close] when done.
func newExecIO(stdout, stderr io.Writer) execIO {
	id := crypto.RandHex(idLen)
	logDir, _ := os.UserCacheDir()
	logDir = filepath.Join(logDir, logSubdir)
	os.MkdirAll(logDir, logDirPerm)
	stdoutPath := filepath.Join(logDir, fmt.Sprintf("%s.%s", id, stdoutLogSuffix))
	stderrPath := filepath.Join(logDir, fmt.Sprintf("%s.%s", id, stderrLogSuffix))
	for _, p := range []string{stdoutPath, stderrPath} {
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, logFilePerm); err == nil {
			f.Close()
		}
	}
	return &fileExecIO{
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
		stdout:     stdout,
		stderr:     stderr,
	}
}

// Returns a [cio.Creator] that routes stdout and stderr to separate log files.
func (f *fileExecIO) creator() (cio.Creator, error) {
	stdoutURI, err := cio.LogURIGenerator(logURIScheme, f.stdoutPath, nil)
	if err != nil {
		return nil, err
	}
	stderrURI, err := cio.LogURIGenerator(logURIScheme, f.stderrPath, nil)
	if err != nil {
		return nil, err
	}
	sio := &splitLogIO{stdout: stdoutURI.String(), stderr: stderrURI.String()}
	return func(_ string) (cio.IO, error) { return sio, nil }, nil
}

// Reads the log files and writes their contents to the caller's writers.
//
// pio is unused; output forwarding happens from the log files.
func (f *fileExecIO) flush(_ cio.IO) {
	forwardFromFile(f.stdout, f.stdoutPath)
	forwardFromFile(f.stderr, f.stderrPath)
}

// Removes the log files created during construction.
func (f *fileExecIO) close() {
	os.Remove(f.stdoutPath)
	os.Remove(f.stderrPath)
}

// Writes the contents of path to w when w is non-nil and the file is non-empty.
func forwardFromFile(w io.Writer, path string) {
	if w == nil {
		return
	}
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		w.Write(data) //nolint:errcheck
	}
}

// Implements [cio.IO] using separate file URIs for stdout and stderr.
//
// Used instead of [cio.LogFile] so that the two streams can be forwarded
// independently after the process exits.
type splitLogIO struct {
	stdout string // File URI for the stdout log.
	stderr string // File URI for the stderr log.
}

// Returns the CIO configuration with separate stdout and stderr file URIs.
func (s *splitLogIO) Config() cio.Config {
	return cio.Config{Stdout: s.stdout, Stderr: s.stderr}
}

// Cancel is a no-op for file-backed I/O.
func (s *splitLogIO) Cancel() {
	/* no-op */
}

// Wait is a no-op for file-backed I/O.
func (s *splitLogIO) Wait() {
	/* no-op */
}

// Close is a no-op for file-backed I/O; the caller manages file lifecycle.
func (s *splitLogIO) Close() error {
	return nil
}
