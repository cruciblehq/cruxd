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
	"github.com/containerd/containerd/v2/core/transfer/archive"
	timage "github.com/containerd/containerd/v2/core/transfer/image"
	tregistry "github.com/containerd/containerd/v2/core/transfer/registry"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/cruciblehq/crex"
	dref "github.com/distribution/reference"
)

const (

	// Snapshotter used for container filesystems. containerd runs as root
	// inside the VM, so the native overlayfs kernel module is available.
	snapshotter = "overlayfs"

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
// The archive is transferred server-side into containerd's content store,
// tagged with a deterministic name derived from the path, and the layers
// for the target platform are unpacked into the snapshotter. A container
// is created with a fresh snapshot and a long-running task (sleep infinity)
// is started so that subsequent Exec calls have a running process to attach
// to. Any existing container with the same ID is removed before the new one
// is created. Building for a platform other than the host requires
// QEMU / binfmt_misc support in the kernel.
func (rt *Runtime) StartContainer(ctx context.Context, path string, id string, platform string) (*Container, error) {
	tag := imageTag(path)

	if err := rt.transferImage(ctx, path, tag, platform); err != nil {
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

	return c, nil
}

// Pulls a remote OCI image and starts a container from it.
//
// The reference is a single-token OCI image name such as "alpine:3.21" or
// "docker.io/library/alpine:3.21", normalized to include the default
// registry and tag when omitted. The image is pulled into containerd's
// content store, unpacked for the target platform, and a container with a
// long-running task is started. Any existing container with the same ID is
// removed before the new one is created.
func (rt *Runtime) StartContainerFromOCI(ctx context.Context, ref string, id string, platform string) (*Container, error) {
	image, err := rt.pullImage(ctx, ref, platform)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	c := &Container{
		client:   rt.client,
		id:       id,
		platform: platform,
	}

	c.remove(ctx)

	ctr, err := c.create(ctx, image)
	if err != nil {
		return nil, crex.Wrap(ErrRuntime, err)
	}

	if err := c.startTask(ctx, ctr); err != nil {
		ctr.Delete(ctx, containerd.WithSnapshotCleanup)
		return nil, crex.Wrap(ErrRuntime, err)
	}

	return c, nil
}

// Pulls a remote OCI image from a container registry.
//
// The reference is a single-token image name. Bare names like "alpine:3.21"
// are normalized to "docker.io/library/alpine:3.21", and untagged names
// receive the "latest" tag. This differs from Crucible references, which
// are space-separated name and version strings resolved by the CLI before
// reaching the daemon. The image is stored in containerd's content store
// and unpacked into the snapshotter for the specified platform.
//
// Uses the containerd transfer service rather than the lower-level Pull or
// Fetch APIs. The transfer service handles multi-platform index resolution
// correctly, including index entries whose descriptors lack explicit platform
// metadata (as seen in some Docker Official Images).
//
// If the image is already present and unpacked for the target platform the
// pull is skipped, avoiding unnecessary registry requests (e.g. when
// Docker Hub rate limits are in effect).
func (rt *Runtime) pullImage(ctx context.Context, ref string, platform string) (containerd.Image, error) {
	named, err := dref.ParseNormalizedNamed(ref)
	if err != nil {
		return nil, err
	}
	fullRef := dref.TagNameOnly(named).String()

	p, err := platforms.Parse(platform)
	if err != nil {
		return nil, err
	}

	// Fast path: reuse an image that is already unpacked locally.
	if img, err := rt.resolveImage(ctx, fullRef, platform); err == nil {
		unpacked, err := img.IsUnpacked(ctx, snapshotter)
		if err == nil && unpacked {
			slog.Info("image already unpacked, skipping pull", "ref", fullRef, "platform", platform)
			return img, nil
		}
	}

	slog.Info("pulling image", "ref", fullRef, "platform", platform)

	src, err := tregistry.NewOCIRegistry(ctx, fullRef)
	if err != nil {
		return nil, err
	}

	dest := timage.NewStore(fullRef,
		timage.WithPlatforms(p),
		timage.WithUnpack(p, snapshotter),
	)

	if err := rt.client.Transfer(ctx, src, dest); err != nil {
		return nil, err
	}

	return rt.resolveImage(ctx, fullRef, platform)
}

// Transfers an OCI archive into containerd's content store server-side.
//
// The archive is streamed to containerd which imports it, stores it under
// the given tag, and unpacks the layers for the target platform into the
// snapshotter. The entire operation runs inside the containerd process,
// so cruxd does not need mount privileges.
func (rt *Runtime) transferImage(ctx context.Context, path, tag, platform string) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()

	p, err := platforms.Parse(platform)
	if err != nil {
		return err
	}

	src := archive.NewImageImportStream(fh, "")
	dest := timage.NewStore(tag, timage.WithUnpack(p, snapshotter))

	return rt.client.Transfer(ctx, src, dest)
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
// The archive is transferred server-side into containerd's content store,
// tagged with the provided name, and the layers are unpacked into the
// snapshotter.
func (rt *Runtime) ImportImage(ctx context.Context, path, tag string) error {
	platform := defaultPlatform()
	if err := rt.transferImage(ctx, path, tag, platform); err != nil {
		return crex.Wrap(ErrRuntime, err)
	}
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
