package compute

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/leases"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/crypto"
)

// Container runtime limits and defaults.
const (

	// Shell used by default.
	defaultShell = "/bin/sh"

	// Maximum UID or GID accepted by runc. Runc requires values to fit in
	// int32 even though the OCI spec field type is uint32.
	runcMaxID = 1<<31 - 1
)

// Suffixes appended to step IDs when naming container records and processes.
const (

	// Suffix appended to step IDs when naming container records.
	containerIDSuffix = "container"

	// Suffix appended to step IDs when naming exec processes.
	execIDSuffix = "exec"
)

// Grace period for SIGTERM before SIGKILL is sent during Stop.
const stopGraceTimeout = 10 * time.Second

// Overlay filesystem layout.
const (

	// Path of the rootfs directory within a containerd snapshot mount.
	rootfsPath = "rootfs"
)

// An isolation boundary with a persistent writable snapshot.
//
// A [Container] is created with [Client.Load] and holds a writable overlayfs
// snapshot over the base image. [Container.Start] starts the container's main
// process, which can be stopped with [Container.Stop]; the snapshot persists
// across multiple Start/Stop cycles. [Container.Exec] runs a command in the
// container, and [Container.Copy] applies a tar archive into the snapshot;
// both accumulate changes that persist in the snapshot. Once the desired state
// is reached, [Container.Commit] captures the snapshot as a new image. The
// container and snapshot are released by calling [Container.Destroy] when they
// are no longer needed.
type Container struct {
	namespace   string                       // Containerd namespace for all operations.
	leaseID     string                       // Lease protecting the snapshot and build content from GC.
	hostNetwork bool                         // Whether the container uses the host network (a relaxation of the restrictive default).
	conn        *containerd.Client           // Containerd connection borrowed from the creating Client.
	img         containerd.Image             // Current base image.
	config      *ocispec.ImageConfig         // OCI config override applied at Commit time; nil means use img's config.
	snapshotID  string                       // Key of the active writable snapshot in the store.
	snapshotter string                       // Snapshotter name ("overlayfs").
	oci         specs.Spec                   // Compiled security spec; used as the runtime spec base.
	mu          sync.Mutex                   // Serialises Start, Stop, Exec, and Copy calls.
	ctr         containerd.Container         // Running container record; nil when stopped.
	task        containerd.Task              // Running task (PID 1); nil when stopped.
	exitCh      <-chan containerd.ExitStatus // Exit status channel from task.Wait; nil when stopped.
}

// Starts the container's main process.
//
// The process command is derived from the image's Entrypoint and Cmd fields,
// which can be overridden via [Container.Configure] before calling Start. If
// the image has neither, the OCI runtime rejects the spec at start time. The
// container must not already be running; a second call returns an error. Use
// [Container.Stop] to terminate the process and [Container.Destroy] to release
// all resources.
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.task != nil {
		return crex.Tag(crex.Newf(ErrContainer, "container already started"), compute.ErrAlreadyStarted)
	}

	ctx = namespaces.WithNamespace(ctx, c.namespace)

	containerID := fmt.Sprintf("%s-%s", crypto.RandHex(idLen), containerIDSuffix)

	// The spec is policy-free by default: a restrictive baseline (isolated
	// network from the default platform spec). Relaxations are applied only
	// when the container was created with them, keeping container management
	// independent of what the container is used for.
	specOpts := []oci.SpecOpts{
		baseSpecOpt(c.oci),
		oci.WithImageConfig(c.img),
		withComputeFields(c.namespace, containerID),
		withKeepAliveArgs(),
	}
	if c.hostNetwork {
		specOpts = append(specOpts, oci.WithHostNamespace(specs.NetworkNamespace), withResolvConf())
	}

	ctn, err := c.conn.NewContainer(ctx, containerID,
		containerd.WithImage(c.img),
		containerd.WithSnapshotter(c.snapshotter),
		containerd.WithSnapshot(c.snapshotID),
		containerd.WithNewSpec(specOpts...),
	)
	if err != nil {
		return crex.Wrapf(ErrContainer, err, "could not create container")
	}

	task, err := ctn.NewTask(ctx, cio.NullIO)
	if err != nil {
		ctn.Delete(ctx)
		return crex.Wrapf(ErrContainer, err, "could not create task")
	}

	exitCh, err := task.Wait(ctx)
	if err != nil {
		task.Delete(ctx)
		ctn.Delete(ctx)
		return crex.Wrapf(ErrContainer, err, "could not wait for task")
	}

	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		ctn.Delete(ctx)
		return crex.Wrapf(ErrContainer, err, "could not start task")
	}

	c.ctr = ctn
	c.task = task
	c.exitCh = exitCh
	return nil
}

// Stops the container's main process.
//
// Sends SIGTERM and waits up to [stopGraceTimeout] seconds for a graceful exit
// before sending SIGKILL. The writable snapshot is not affected; changes from
// [Container.Copy] and [Container.Exec] calls are preserved. Safe to call on
// an already-stopped container.
func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	task, ctr, exitCh := c.task, c.ctr, c.exitCh
	c.task, c.ctr, c.exitCh = nil, nil, nil
	c.mu.Unlock()

	if task == nil {
		return nil
	}

	cleanupCtx := namespaces.WithNamespace(context.Background(), c.namespace)
	_ = task.Kill(cleanupCtx, syscall.SIGTERM)

	stopCtx, cancel := context.WithTimeout(cleanupCtx, stopGraceTimeout)
	defer cancel()
	select {
	case <-exitCh:
	case <-stopCtx.Done():
		_ = task.Kill(cleanupCtx, syscall.SIGKILL)
		<-exitCh
	}

	task.Delete(cleanupCtx)
	ctr.Delete(cleanupCtx)
	return nil
}

// Runs a command in the container and returns its exit code.
//
// Execs the command into the container's running main process. The command is
// invoked via opts.Shell (or the default shell) as a single argument. Changes
// to the filesystem accumulate in the writable snapshot and persist for
// subsequent [Container.Exec] and [Container.Copy] calls. A non-zero exit code
// is a normal result. The error is reserved for a command that could not be
// run or waited on.
func (c *Container) Exec(ctx context.Context, stdout, stderr io.Writer, command string, opts compute.Options) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.task == nil {
		return 0, crex.Tag(crex.Newf(ErrContainer, "container not started"), compute.ErrNotStarted)
	}

	ctx = namespaces.WithNamespace(ctx, c.namespace)

	return c.runExec(ctx, stdout, stderr, command, &opts)
}

// Applies an uncompressed tar archive to the container's writable snapshot.
//
// r must be an uncompressed tar archive. Entries are applied as absolute paths
// within the container filesystem, preserving all permissions, ownership, and
// timestamps. OCI whiteout entries follow standard layer semantics. Changes
// accumulate in the snapshot. The container's main process is not affected.
//
// The tar is written to the content store and applied by the containerd diff
// service, which mounts the snapshot and applies the layer server-side. This
// avoids mounting the snapshot on the crux host, which is impossible when crux
// runs on a different machine or OS than containerd (e.g. macOS driving a Linux
// VM).
func (c *Container) Copy(ctx context.Context, r io.Reader) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx = namespaces.WithNamespace(ctx, c.namespace)

	leaseCtx, done, err := c.conn.WithLease(ctx)
	if err != nil {
		return crex.Wrapf(ErrContainer, err, "could not create lease")
	}
	defer done(ctx)

	mounts, err := c.conn.SnapshotService(c.snapshotter).Mounts(leaseCtx, c.snapshotID)
	if err != nil {
		return crex.Wrapf(ErrContainer, err, "could not get snapshot mounts")
	}

	desc, err := writeLayerBlob(leaseCtx, c.conn.ContentStore(), r)
	if err != nil {
		return err
	}

	if _, err := c.conn.DiffService().Apply(leaseCtx, desc, mounts); err != nil {
		return crex.Wrapf(ErrContainer, err, "could not apply layer tar")
	}

	return nil
}

// Writes an uncompressed tar stream into the content store and returns a layer
// descriptor referencing it.
//
// The digest and size are computed while the stream is written, so the caller
// need not know them in advance. The descriptor uses the uncompressed OCI layer
// media type, suitable for server-side application via the diff service. An
// already-existing blob with the same digest is treated as success.
func writeLayerBlob(ctx context.Context, cs content.Store, r io.Reader) (ocispec.Descriptor, error) {
	ref := fmt.Sprintf("crux-copy-layer-%s", crypto.RandHex(idLen))
	w, err := content.OpenWriter(ctx, cs, content.WithRef(ref))
	if err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "could not open content writer")
	}
	defer w.Close()

	n, err := io.Copy(w, r)
	if err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "could not write layer blob")
	}

	if err := w.Commit(ctx, n, ""); err != nil && !errdefs.IsAlreadyExists(err) {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "could not commit layer blob")
	}

	return ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    w.Digest(),
		Size:      n,
	}, nil
}

// Stores cfg as the pending configuration to be applied at [Container.Commit].
//
// Does not create an image or affect the writable snapshot. Typically called
// after [Container.Inspect] to modify specific fields while preserving the
// rest. The change takes effect when [Container.Commit] is called.
func (c *Container) Configure(cfg ocispec.ImageConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = &cfg
}

// Creates a new image from the container's current state.
//
// If the snapshot has no filesystem changes and no pending configuration,
// returns the current image reference unchanged. If only a configuration
// change is pending, writes a config-only image with an empty-layer history
// entry. Otherwise commits the snapshot diff, incorporating any pending
// configuration. The reference can be passed to [Client.Load], [Client.Export],
// [Client.Extract], or [Client.Remove]. The live container is not affected.
// Records a history entry using by as the creator and the current time.
func (c *Container) Commit(ctx context.Context, by string) (compute.ImageRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx = namespaces.WithNamespace(ctx, c.namespace)

	// Ask containerd whether the snapshot has any content of its own to commit.
	// Usage reports the size of this snapshot's writable layer (the overlay
	// upperdir, or the whole filesystem for a parentless bind-mounted snapshot),
	// so it detects changes regardless of how the snapshot is mounted and
	// without reading any paths from the client host.
	usage, err := c.conn.SnapshotService(c.snapshotter).Usage(ctx, c.snapshotID)
	if err != nil {
		return "", crex.Wrapf(ErrContainer, err, "could not get snapshot usage")
	}
	if usage.Size == 0 {
		if c.config == nil {
			return compute.ImageRef(c.img.Name()), nil
		}
		ref, err := configureImage(ctx, c.conn, c.img, *c.config, by)
		return compute.ImageRef(ref), err
	}
	ref, err := commitSnapshotDiff(ctx, c.conn, c.img, c.snapshotID, c.snapshotter, c.config, by)
	return compute.ImageRef(ref), err
}

// Releases the container's writable snapshot.
//
// After Destroy returns, no other methods may be called on the container.
func (c *Container) Destroy(ctx context.Context) error {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	c.conn.SnapshotService(c.snapshotter).Remove(ctx, c.snapshotID)
	if c.leaseID != "" {
		_ = c.conn.LeasesService().Delete(ctx, leases.Lease{ID: c.leaseID})
	}
	return nil
}

// Returns the container's current OCI image config.
//
// If a pending configuration has been set via [Container.Configure], that is
// returned directly. If not, the config is read from the base image in the
// content store. The caller owns the returned struct and may modify it before
// passing it back to [Container.Configure].
func (c *Container) Inspect(ctx context.Context) (ocispec.ImageConfig, error) {
	c.mu.Lock()
	pending := c.config
	img := c.img
	c.mu.Unlock()
	if pending != nil {
		return *pending, nil
	}
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	_, cfg, err := readImageConfig(ctx, c.conn.ContentStore(), img.Target())
	if err != nil {
		return ocispec.ImageConfig{}, err
	}
	return cfg.Config, nil
}

// Execs command into the running task and returns its exit code, routing
// output through an [execIO].
func (c *Container) runExec(ctx context.Context, stdout, stderr io.Writer, command string, opts *compute.Options) (int, error) {
	execID := fmt.Sprintf("%s-%s", crypto.RandHex(idLen), execIDSuffix)
	eio := newExecIO(stdout, stderr)
	defer eio.close()

	creator, err := eio.creator()
	if err != nil {
		return 0, crex.Wrapf(ErrContainer, err, "could not create log IO")
	}

	spec, err := c.ctr.Spec(ctx)
	if err != nil {
		return 0, crex.Wrapf(ErrContainer, err, "could not get container spec")
	}

	process, err := c.task.Exec(ctx, execID, buildExecProcess(spec, command, opts), creator)
	if err != nil {
		return 0, crex.Wrapf(ErrContainer, err, "could not create exec process")
	}

	exitCh, err := process.Wait(ctx)
	if err != nil {
		process.Delete(ctx)
		return 0, crex.Wrapf(ErrContainer, err, "could not wait for exec process")
	}

	if err := process.Start(ctx); err != nil {
		process.Delete(ctx)
		return 0, crex.Wrapf(ErrContainer, err, "could not start exec process")
	}

	var code int
	var execErr error
	select {
	case status := <-exitCh:
		if err := status.Error(); err != nil {
			execErr = crex.Wrapf(ErrContainer, err, "exec process failed")
		} else {
			code = int(status.ExitCode())
		}
	case <-ctx.Done():
		process.Kill(ctx, syscall.SIGTERM)
		execErr = ctx.Err()
	}

	eio.flush(process.IO())
	process.Delete(ctx)

	return code, execErr
}

// Builds a process spec for exec from the container's base process.
//
// Overrides args, env, workdir, and user from command and opts.
func buildExecProcess(base *specs.Spec, command string, opts *compute.Options) *specs.Process {
	proc := *base.Process
	proc.Terminal = false

	shell := defaultShell
	if opts.Shell != "" {
		shell = opts.Shell
	}
	proc.Args = []string{shell, "-c", command}

	if len(opts.Env) > 0 {
		env := make([]string, 0, len(opts.Env))
		for k, v := range opts.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		proc.Env = MergeEnv(proc.Env, env)
	}

	if opts.Workdir != "" {
		proc.Cwd = opts.Workdir
	}

	if opts.User != "" {
		uid, gid, err := parseUserSpec(opts.User)
		if err == nil {
			proc.User.UID = uid
			if gid != nil {
				proc.User.GID = *gid
			}
		}
	}

	return &proc
}

// Parses a user spec in "uid" or "uid:gid" format.
//
// The uid is required. The gid is optional; a nil gid indicates no colon was
// present. Both components must satisfy the [parseID] constraint.
func parseUserSpec(user string) (uid uint32, gid *uint32, err error) {
	parts := strings.SplitN(user, ":", 2)
	uid, err = parseID(parts[0])
	if err != nil {
		return 0, nil, crex.Wrapf(ErrContainer, err, "invalid uid %q", user)
	}

	if len(parts) == 2 {
		g, err := parseID(parts[1])
		if err != nil {
			return 0, nil, crex.Wrapf(ErrContainer, err, "invalid gid %q", user)
		}
		gid = &g
	}
	return uid, gid, nil
}

// Parses a decimal integer as a process ID.
//
// Values above MaxInt32 are rejected to match runc's requirement, even though
// they are valid uint32 values.
func parseID(s string) (uint32, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	if v > runcMaxID {
		return 0, crex.New("exceeds runc maximum")
	}
	return uint32(v), nil
}

// Choose the base spec option.
//
// Returns an OCI spec option that either applies the given compiled security
// spec if it is compiled, or falls back to the default platform spec. The
// distinction is made based on whether the compiled security spec has a Linux
// or Windows section.
func baseSpecOpt(src specs.Spec) oci.SpecOpts {
	if src.Linux == nil && src.Windows == nil {
		return oci.WithDefaultSpecForPlatform(fmt.Sprintf("linux/%s", runtime.GOARCH))
	}
	return applyOCISpec(src)
}

// Main process used for build containers whose image defines no entrypoint or
// command. Build steps run via exec, so the main process only needs to stay
// alive; this blocks indefinitely.
var defaultKeepAlive = []string{"/bin/sh", "-c", "exec sleep infinity"}

// Sets a blocking main process when the spec has no process args.
//
// An image being built up from scratch has no entrypoint or command, which the
// OCI runtime rejects ("args must not be empty"). Since build steps run through
// exec rather than the main process, a keep-alive command is sufficient. When
// the image already defines args, they are left untouched.
func withKeepAliveArgs() oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
		if s.Process == nil {
			s.Process = &specs.Process{}
		}
		if len(s.Process.Args) == 0 {
			s.Process.Args = append([]string{}, defaultKeepAlive...)
		}
		return nil
	}
}

// Bind-mounts the host's resolv.conf into the container so build steps can
// resolve package mirrors and other hosts.
//
// Build commands such as "apt-get update" need working DNS. A base image's
// /etc/resolv.conf is typically empty or points at a resolver that is not
// present in the build container, so the host's file is mounted over it. The
// container shares the host network namespace (see [oci.WithHostNamespace]),
// so the host's resolver is reachable.
func withResolvConf() oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
		s.Mounts = append(s.Mounts, specs.Mount{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      "/etc/resolv.conf",
			Options:     []string{"rbind", "ro"},
		})
		return nil
	}
}

// Copies src into the blank spec produced by [containerd.WithNewSpec] so the
// compiled security spec becomes the starting point for the full container spec.
//
// Top-level pointer fields (Root, Process, Linux) are shallow-copied into new
// structs so that subsequent SpecOpts can modify them without mutating the
// original security spec.
func applyOCISpec(src specs.Spec) oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
		*s = src
		if src.Root != nil {
			r := *src.Root
			s.Root = &r
		}
		if src.Process != nil {
			p := *src.Process
			s.Process = &p
		}
		if src.Linux != nil {
			l := *src.Linux
			s.Linux = &l
		}
		return nil
	}
}

// Sets the two fields the affordance package leaves empty because only the
// container runtime knows them: the rootfs path (always "rootfs" under
// containerd's overlayfs snapshotter) and the cgroup path.
func withComputeFields(ns, id string) oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
		s.Root.Path = rootfsPath
		s.Linux.CgroupsPath = filepath.Join("/", ns, id)
		return nil
	}
}
