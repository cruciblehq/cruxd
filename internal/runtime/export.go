package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/images/archive"
	"github.com/containerd/containerd/v2/pkg/rootfs"
	"github.com/cruciblehq/go-utils/crex"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Filename of the OCI archive produced by Export.
const exportFilename = "image.tar"

// Commits the container's filesystem changes and exports the result as an
// OCI archive.
//
// The diff between the container's snapshot and its parent is stored as a
// new layer. If entrypoint is non-empty it is set on the image config.
// The resulting image is written to output/image.tar.
func (c *Container) Export(ctx context.Context, output string, entrypoint []string) error {
	loaded, err := c.client.LoadContainer(ctx, c.id)
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	info, err := loaded.Info(ctx)
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	layer, diffID, err := c.snapshotDiff(ctx, info)
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	if err := c.updateImage(ctx, info.Image, func(manifest *ocispec.Manifest, config *ocispec.Image) {
		manifest.Layers = append(manifest.Layers, layer)
		config.RootFS.DiffIDs = append(config.RootFS.DiffIDs, diffID)
		if len(entrypoint) > 0 {
			config.Config.Entrypoint = entrypoint
			config.Config.Cmd = nil
		}
	}); err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	exportPath := filepath.Join(output, exportFilename)
	if err := c.exportImage(ctx, info.Image, exportPath); err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	slog.Info("image exported", "path", exportPath)
	return nil
}

// Computes the diff between the container's snapshot and its parent, returning
// the layer descriptor and its diff ID without modifying the image.
func (c *Container) snapshotDiff(ctx context.Context, info containers.Container) (ocispec.Descriptor, digest.Digest, error) {
	layer, err := rootfs.CreateDiff(ctx,
		info.SnapshotKey,
		c.client.SnapshotService(info.Snapshotter),
		c.client.DiffService(),
	)
	if err != nil {
		return ocispec.Descriptor{}, "", err
	}

	diffID, err := images.GetDiffID(ctx, c.client.ContentStore(), layer)
	if err != nil {
		return ocispec.Descriptor{}, "", err
	}

	return layer, diffID, nil
}

// Writes the named image to an OCI tar archive at the given path.
func (c *Container) exportImage(ctx context.Context, imageName, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return c.client.Export(ctx, f, archive.WithImage(c.client.ImageService(), imageName))
}

// Loads the image's manifest and config, applies the mutation, and writes
// the updated blobs back to the content store.
func (c *Container) updateImage(ctx context.Context, imageName string, mutate func(*ocispec.Manifest, *ocispec.Image)) error {
	is := c.client.ImageService()

	img, err := is.Get(ctx, imageName)
	if err != nil {
		return err
	}

	manifest, err := c.readManifest(ctx, img.Target)
	if err != nil {
		return err
	}

	config, err := c.readConfig(ctx, manifest.Config)
	if err != nil {
		return err
	}

	mutate(&manifest, &config)

	newConfigDesc, err := c.writeBlob(ctx, manifest.Config.MediaType, config, imageName+"-config")
	if err != nil {
		return err
	}
	manifest.Config = newConfigDesc

	manifestLabels := manifestGCLabels(manifest)
	newManifestDesc, err := c.writeBlob(ctx, img.Target.MediaType, manifest, imageName+"-manifest", content.WithLabels(manifestLabels))
	if err != nil {
		return err
	}

	img.Target = newManifestDesc
	_, err = is.Update(ctx, img, "target")
	return err
}

// Loads an OCI manifest from the content store.
func (c *Container) readManifest(ctx context.Context, desc ocispec.Descriptor) (ocispec.Manifest, error) {
	b, err := content.ReadBlob(ctx, c.client.ContentStore(), desc)
	if err != nil {
		return ocispec.Manifest{}, err
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return ocispec.Manifest{}, err
	}
	return m, nil
}

// Loads an OCI image config from the content store.
func (c *Container) readConfig(ctx context.Context, desc ocispec.Descriptor) (ocispec.Image, error) {
	b, err := content.ReadBlob(ctx, c.client.ContentStore(), desc)
	if err != nil {
		return ocispec.Image{}, err
	}
	var img ocispec.Image
	if err := json.Unmarshal(b, &img); err != nil {
		return ocispec.Image{}, err
	}
	return img, nil
}

// Serializes a value and writes it to the content store, returning the
// descriptor that references the stored blob.
func (c *Container) writeBlob(ctx context.Context, mediaType string, v any, ref string, opts ...content.Opt) (ocispec.Descriptor, error) {
	cs := c.client.ContentStore()
	b, err := json.Marshal(v)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(b),
		Size:      int64(len(b)),
	}
	if err := content.WriteBlob(ctx, cs, ref, bytes.NewReader(b), desc, opts...); err != nil {
		return ocispec.Descriptor{}, err
	}
	return desc, nil
}

// Computes containerd GC reference labels for a manifest's children.
//
// These labels allow containerd's garbage collector to trace reachability
// from the manifest blob to its config and layer blobs.
func manifestGCLabels(m ocispec.Manifest) map[string]string {
	labels := map[string]string{
		"containerd.io/gc.ref.content.config": m.Config.Digest.String(),
	}
	for i, layer := range m.Layers {
		key := fmt.Sprintf("containerd.io/gc.ref.content.l.%d", i)
		labels[key] = layer.Digest.String()
	}
	return labels
}
