package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/cruciblehq/crex"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Sequence counter for generating unique exec process identifiers.
var execSeq uint64

// Returns a unique exec process identifier.
func nextExecID() string {
	return fmt.Sprintf("exec-%d", atomic.AddUint64(&execSeq, 1))
}

// Output of a command execution inside a container.
type ExecResult struct {
	ExitCode int    // Exit code of the process.
	Stdout   string // Captured standard output.
	Stderr   string // Captured standard error.
}

// Runs a command inside the container.
//
// The command is passed to the shell as a single argument via "shell -c
// command". Environment variables and working directory override the
// container's OCI spec for this execution only.
func (c *Container) Exec(ctx context.Context, shell, command string, env []string, workdir string) (*ExecResult, error) {
	var stdout bytes.Buffer
	exitCode, stderr, err := c.execCommand(ctx, nil, &stdout, env, workdir, shell, "-c", command)
	if err != nil {
		return nil, err
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr,
	}, nil
}

// Runs a command and arguments directly inside the container.
//
// Unlike [Exec], which passes a command string to a shell, ExecArgs runs the
// command directly without shell wrapping. This is suitable for CLI-invoked
// exec where the user provides the full command line.
func (c *Container) ExecArgs(ctx context.Context, args []string) (*ExecResult, error) {
	pspec, err := c.buildProcessSpec(ctx, nil, "", args...)
	if err != nil {
		return nil, err
	}

	var stdout, stderr bytes.Buffer
	exitCode, err := c.execProcess(ctx, pspec, nil, &stdout, &stderr)
	if err != nil {
		return nil, err
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// Builds an OCI process spec for running a command inside the container.
//
// A process spec defines everything needed to start a process: the command
// and arguments, environment variables, working directory, and terminal mode.
// The base values are copied from the container's own OCI spec, then env and
// workdir are overridden if provided.
func (c *Container) buildProcessSpec(ctx context.Context, env []string, workdir string, args ...string) (*specs.Process, error) {
	ctr, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		return nil, err
	}

	spec, err := ctr.Spec(ctx)
	if err != nil {
		return nil, err
	}

	pspec := *spec.Process
	pspec.Terminal = false
	pspec.Args = args

	if len(env) > 0 {
		pspec.Env = mergeEnv(pspec.Env, env)
	}
	if workdir != "" {
		pspec.Cwd = workdir
	}

	return &pspec, nil
}

// Merges override env vars on top of a base env slice.
func mergeEnv(base, overrides []string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		if k, v, ok := strings.Cut(entry, "="); ok {
			merged[k] = v
		}
	}
	for _, entry := range overrides {
		if k, v, ok := strings.Cut(entry, "="); ok {
			merged[k] = v
		}
	}

	result := make([]string, 0, len(merged))
	for k, v := range merged {
		result = append(result, k+"="+v)
	}
	return result
}

// Runs a command inside the container, returning the exit code and captured
// stderr. Builds the process spec from args, then delegates to execProcess.
// A non-zero exit code is not treated as an error; the caller decides.
func (c *Container) execCommand(ctx context.Context, stdin io.Reader, stdout io.Writer, env []string, workdir string, args ...string) (int, string, error) {
	pspec, err := c.buildProcessSpec(ctx, env, workdir, args...)
	if err != nil {
		return 0, "", crex.Wrap(ErrRuntime, err)
	}

	var stderr bytes.Buffer
	exitCode, err := c.execProcess(ctx, pspec, stdin, stdout, &stderr)
	if err != nil {
		return 0, "", err
	}
	return exitCode, stderr.String(), nil
}

// Starts a process inside the container's running task, waits for it to exit,
// and returns the exit code.
//
// The process is attached to the task as an additional exec, not as the
// primary process. This requires the task to already be running (started by
// [Container.startTask] during container creation). stdin, stdout, and stderr
// are connected to the process. Nil streams are replaced with io.Discard
// (stdout/stderr) or left disconnected (stdin). A non-zero exit code is not
// treated as an error; the caller decides how to handle it.
//
// When stdin is provided, the container's stdin is explicitly closed after the
// reader returns EOF so the exec process receives the EOF signal. This is
// required because the containerd shim holds both ends of the stdin FIFO open
// and will not propagate EOF on its own.
func (c *Container) execProcess(ctx context.Context, pspec *specs.Process, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	task, err := c.loadTask(ctx)
	if err != nil {
		return 0, err
	}

	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	// Wrap stdin to detect when the reader returns EOF.
	var stdinDone <-chan struct{}
	if stdin != nil {
		dr := newDoneReader(stdin)
		stdin = dr
		stdinDone = dr.done
	}

	process, err := task.Exec(ctx, nextExecID(), pspec, cio.NewCreator(
		cio.WithStreams(stdin, stdout, stderr),
	))
	if err != nil {
		return 0, crex.Wrap(ErrRuntime, err)
	}

	return awaitProcess(ctx, process, stdinDone)
}

// Loads the container's running task.
func (c *Container) loadTask(ctx context.Context) (containerd.Task, error) {
	ctr, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	task, err := ctr.Task(ctx, nil)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	return task, nil
}

// Waits for an exec process to exit and returns the exit code.
//
// The process is started, then the function blocks until it exits. If
// stdinDone is non-nil, the process stdin is closed when the channel fires
// so the exec process receives EOF. The process is always deleted before
// returning.
func awaitProcess(ctx context.Context, process containerd.Process, stdinDone <-chan struct{}) (int, error) {
	statusC, err := process.Wait(ctx)
	if err != nil {
		process.Delete(ctx)
		return 0, crex.Wrap(ErrRuntime, err)
	}

	if err := process.Start(ctx); err != nil {
		process.Delete(ctx)
		return 0, crex.Wrap(ErrRuntime, err)
	}

	// Close the container's stdin after the reader is exhausted. Without this
	// the shim keeps its write end of the stdin FIFO open and the exec process
	// never receives EOF.
	if stdinDone != nil {
		go func() {
			<-stdinDone
			process.CloseIO(ctx, containerd.WithStdinCloser)
		}()
	}

	exitStatus := <-statusC
	process.Delete(ctx)

	code, _, err := exitStatus.Result()
	if err != nil {
		return 0, crex.Wrap(ErrRuntime, err)
	}

	return int(code), nil
}
