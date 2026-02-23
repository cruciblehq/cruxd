package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	goruntime "runtime"
	"syscall"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/cruciblehq/crex"
)

const (

	// Snapshotter used for container filesystems. fuse-overlayfs provides
	// overlay semantics without requiring root privileges (no mount(2)),
	// allowing cruxd to run as a regular user.
	snapshotter = "fuse-overlayfs"

	// OCI runtime shim for running containers.
	ociRuntime = "io.containerd.runc.v2"
)

// Manages the containerd client and provides image and container operations.
type Runtime struct {
	client *containerd.Client // Containerd client for managing containers and images.
}

// Creates a runtime connected to the containerd socket at the given address.
//
// The namespace scopes all containerd operations to a single tenant. The
// runtime must be closed when no longer needed.
func New(address, namespace string) (*Runtime, error) {
	client, err := containerd.New(address, containerd.WithDefaultNamespace(namespace))
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}
	return &Runtime{client: client}, nil
}

// Closes the containerd client connection.
func (rt *Runtime) Close() error {
	return rt.client.Close()
}

// Imports an OCI archive, unpacks it for the target platform, and starts
// a container.
//
// The archive is imported into containerd's content store and tagged with
// a deterministic name derived from the path. The layers for the target
// platform are unpacked into the snapshotter, a container is created with
// a fresh snapshot, and a long-running task (sleep infinity) is started so
// that subsequent Exec calls have a running process to attach to. Any existing
// container with the same ID is removed before the new one is created.
// Building for a platform other than the host requires QEMU / binfmt_misc
// support in the kernel.
func (rt *Runtime) StartContainer(ctx context.Context, path string, id string, platform string) (*Container, error) {
	tag := imageTag(path)

	source, err := rt.importArchive(ctx, path)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	if err := rt.tagImage(ctx, source, tag); err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	if err := rt.unpackImage(ctx, tag, platform); err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	c := &Container{
		client:   rt.client,
		id:       id,
		platform: platform,
	}

	// Remove any stale container from a previous build with the same ID.
	c.remove(ctx)

	image, err := rt.resolveImage(ctx, tag, platform)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	ctr, err := c.create(ctx, image)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	if err := c.startTask(ctx, ctr); err != nil {
		ctr.Delete(ctx, containerd.WithSnapshotCleanup)
		return nil, crex.Wrap(ErrRuntime, err)
	}

	slog.Debug("container started", "id", id, "image", tag)

	return c, nil
}

// Imports an OCI archive into the content store.
//
// The archive must contain exactly one image. Multi-platform archives
// are supported (single OCI index with per-platform manifests).
func (rt *Runtime) importArchive(ctx context.Context, path string) (images.Image, error) {
	fh, err := os.Open(path)
	if err != nil {
		return images.Image{}, err
	}
	defer fh.Close()

	imported, err := rt.client.Import(ctx, fh)
	if err != nil {
		return images.Image{}, err
	}

	// Import returns one record per image in the archive's index.json.
	// A multi-platform archive has a single entry (an OCI index that
	// internally references per-platform manifests); platform selection
	// happens later via platformImage. Multiple records would mean
	// multiple unrelated images, which we don't support.
	if len(imported) == 0 {
		return images.Image{}, ErrEmptyArchive
	} else if len(imported) > 1 {
		return images.Image{}, ErrMultipleImages
	}

	return imported[0], nil
}

// Tags an imported image under a deterministic name.
//
// Updates the tag if it already exists. Removes the source record when
// its name differs from the tag to avoid duplicates.
func (rt *Runtime) tagImage(ctx context.Context, source images.Image, tag string) error {
	is := rt.client.ImageService()

	img := images.Image{
		Name:   tag,
		Target: source.Target,
	}

	if _, err := is.Create(ctx, img); err != nil {
		if !errdefs.IsAlreadyExists(err) {
			return err
		}
		if _, err := is.Update(ctx, img, "target"); err != nil {
			return err
		}
	}

	if source.Name != tag {
		_ = is.Delete(ctx, source.Name)
	}

	return nil
}

// Unpacks the image layers for the target platform into the snapshotter.
func (rt *Runtime) unpackImage(ctx context.Context, tag, platform string) error {
	image, err := rt.resolveImage(ctx, tag, platform)
	if err != nil {
		return err
	}

	return image.Unpack(ctx, snapshotter)
}

// Looks up a tagged image and selects the manifest for the given platform.
//
// Multi-platform images contain manifests for multiple architectures. This
// method selects one, so that subsequent operations target the correct
// architecture.
func (rt *Runtime) resolveImage(ctx context.Context, tag, platform string) (containerd.Image, error) {
	p, err := platforms.Parse(platform)
	if err != nil {
		return nil, err
	}

	img, err := rt.client.ImageService().Get(ctx, tag)
	if err != nil {
		return nil, err
	}

	return containerd.NewImageWithPlatform(rt.client, img, platforms.Only(p)), nil
}

// Produces a containerd image tag from an archive path.
//
// The path is hashed to produce a tag that is always valid for OCI references
// regardless of which characters the path contains.
func imageTag(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("import/%s:latest", hex.EncodeToString(h[:]))
}

// Returns the default OCI platform for the host architecture.
func defaultPlatform() string {
	return "linux/" + goruntime.GOARCH
}

// Imports an OCI archive, tags it under the given name, and unpacks it for
// the host platform.
//
// The archive is imported into the content store, tagged with the provided
// name, and the layers are unpacked into the snapshotter.
func (rt *Runtime) ImportImage(ctx context.Context, path, tag string) error {
	source, err := rt.importArchive(ctx, path)
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	if err := rt.tagImage(ctx, source, tag); err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	platform := defaultPlatform()
	if err := rt.unpackImage(ctx, tag, platform); err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	slog.Debug("image imported", "tag", tag)
	return nil
}

// Starts a container from a previously imported image tag.
//
// Any stale container with the same ID is cleaned up first. The container
// runs detached with a long-running task.
func (rt *Runtime) StartFromTag(ctx context.Context, tag, id string) (*Container, error) {
	platform := defaultPlatform()

	c := &Container{
		client:   rt.client,
		id:       id,
		platform: platform,
	}

	c.remove(ctx)

	image, err := rt.resolveImage(ctx, tag, platform)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	ctr, err := c.create(ctx, image)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	if err := c.startTask(ctx, ctr); err != nil {
		ctr.Delete(ctx, containerd.WithSnapshotCleanup)
		return nil, crex.Wrap(ErrRuntime, err)
	}

	slog.Debug("container started", "id", id, "image", tag)
	return c, nil
}

// Removes an image and all containers created from it.
//
// Containers are discovered by querying containerd for records whose image
// field matches the tag. Each container's task is killed before the container
// and its snapshot are deleted.
func (rt *Runtime) DestroyImage(ctx context.Context, tag string) error {
	ctrs, err := rt.client.Containers(ctx, fmt.Sprintf("image==%s", tag))
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	for _, ctr := range ctrs {
		if task, taskErr := ctr.Task(ctx, nil); taskErr == nil {
			task.Kill(ctx, syscall.SIGKILL)
			task.Delete(ctx, containerd.WithProcessKill)
		}
		if err := ctr.Delete(ctx, containerd.WithSnapshotCleanup); err != nil && !errdefs.IsNotFound(err) {
			return crex.Wrap(ErrRuntime, err)
		}
	}

	if err := rt.client.ImageService().Delete(ctx, tag); err != nil && !errdefs.IsNotFound(err) {
		return crex.Wrap(ErrRuntime, err)
	}

	slog.Debug("image destroyed", "tag", tag)
	return nil
}

// Returns a handle for an existing container.
//
// The container is not loaded or verified; the handle is a lightweight
// reference that resolves the container lazily on subsequent calls.
func (rt *Runtime) Container(id string) *Container {
	return &Container{
		client:   rt.client,
		id:       id,
		platform: defaultPlatform(),
	}
}
