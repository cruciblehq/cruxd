package runtime

import (
	"context"
	"log/slog"
	"syscall"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/spec/protocol"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// A running build container backed by containerd.
type Container struct {
	client   *containerd.Client // Containerd client for managing the container.
	id       string             // Unique identifier for the container, used as the containerd container ID.
	platform string             // OCI platform (e.g., "linux/amd64").
}

// Queries the current state of the container.
//
// Returns [protocol.ContainerRunning] if the task is active,
// [protocol.ContainerStopped] if the container exists but has no running
// task, or [protocol.ContainerNotCreated] if the container does not exist.
func (c *Container) Status(ctx context.Context) (protocol.ContainerState, error) {
	ctr, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return protocol.ContainerNotCreated, nil
		}
		return "", crex.Wrap(ErrRuntime, err)
	}

	task, err := ctr.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return protocol.ContainerStopped, nil
		}
		return "", crex.Wrap(ErrRuntime, err)
	}

	status, err := task.Status(ctx)
	if err != nil {
		return "", crex.Wrap(ErrRuntime, err)
	}

	switch status.Status {
	case containerd.Running:
		return protocol.ContainerRunning, nil
	default:
		return protocol.ContainerStopped, nil
	}
}

// Stops the container's task.
//
// The running task is killed and deleted. The container metadata is preserved.
// Calling Stop on an already-stopped container is not an error.
func (c *Container) Stop(ctx context.Context) error {
	ctr, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return crex.Wrap(ErrRuntime, err)
	}

	task, err := ctr.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return crex.Wrap(ErrRuntime, err)
	}

	task.Kill(ctx, syscall.SIGKILL)
	if _, err := task.Delete(ctx, containerd.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
		return crex.Wrap(ErrRuntime, err)
	}

	return nil
}

// Removes the container and its resources.
//
// The task is killed and the container is removed from containerd along
// with its snapshot. After destruction the handle is invalid.
func (c *Container) Destroy(ctx context.Context) {
	ctr, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			slog.Warn("failed to load container for destruction", "id", c.id, "error", err)
		}
		return
	}

	if task, err := ctr.Task(ctx, nil); err == nil {
		task.Kill(ctx, syscall.SIGKILL)
		task.Delete(ctx, containerd.WithProcessKill)
	}

	if err := ctr.Delete(ctx, containerd.WithSnapshotCleanup); err != nil && !errdefs.IsNotFound(err) {
		slog.Warn("failed to delete container during destruction", "id", c.id, "error", err)
	}
}

// Creates the containerd container with the standard build configuration.
func (c *Container) create(ctx context.Context, image containerd.Image) (containerd.Container, error) {
	return c.client.NewContainer(ctx, c.id,
		containerd.WithImage(image),
		containerd.WithSnapshotter(snapshotter),
		containerd.WithNewSnapshot(c.id, image),
		containerd.WithRuntime(ociRuntime, nil),
		containerd.WithNewSpec(
			oci.WithDefaultSpecForPlatform(c.platform),
			oci.WithImageConfig(image),
			oci.WithHostNamespace(specs.NetworkNamespace),
			oci.WithHostResolvconf,
			oci.WithProcessArgs("sleep", "infinity"),
		),
	)
}

// Starts the container's long-running task with no attached IO.
func (c *Container) startTask(ctx context.Context, ctr containerd.Container) error {
	task, err := ctr.NewTask(ctx, cio.NullIO)
	if err != nil {
		return err
	}
	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		return err
	}
	return nil
}

// Removes an existing container with this ID, if one exists.
//
// Any running task is killed and the container is deleted along with its
// snapshot. This is a no-op when no container with the ID is found.
func (c *Container) remove(ctx context.Context) {
	existing, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		return
	}
	if task, err := existing.Task(ctx, nil); err == nil {
		task.Kill(ctx, syscall.SIGKILL)
		task.Delete(ctx, containerd.WithProcessKill)
	}
	existing.Delete(ctx, containerd.WithSnapshotCleanup)
}
