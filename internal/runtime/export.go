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
	"github.com/containerd/platforms"
	"github.com/cruciblehq/crex"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Filename of the OCI archive produced by Export.
const exportFilename = "image.tar"

// Commits the container's filesystem changes and exports the result as an
// OCI archive.
//
// The diff between the container's snapshot and its parent is stored as a
// new layer. If entrypoint is non-empty it is set on the image config. The
// resulting image is written to output/image.tar. The stored image record
// in containerd is never modified. The mutated manifest, config, and index
// are written to the content store as ephemeral blobs and referenced only
// during the export. A content lease protects these blobs from garbage
// collection until the export completes.
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

	// Acquire a content lease so the ephemeral blobs written by
	// buildExportTarget survive until the archive export finishes.
	// Without a lease, containerd's GC scheduler may collect them
	// between the write and the export.
	ctx, done, err := c.client.WithLease(ctx)
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}
	defer done(context.Background())

	target, err := c.buildExportTarget(ctx, info.Image, func(manifest *ocispec.Manifest, config *ocispec.Image) {
		manifest.Layers = append(manifest.Layers, layer)
		config.RootFS.DiffIDs = append(config.RootFS.DiffIDs, diffID)
		if len(entrypoint) > 0 {
			config.Config.Entrypoint = entrypoint
			config.Config.Cmd = nil
		}
	})
	if err != nil {
		return crex.Wrap(ErrRuntime, err)
	}

	exportPath := filepath.Join(output, exportFilename)
	if err := c.exportImage(ctx, target, info.Image, exportPath); err != nil {
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

// Writes the image to an OCI tar archive at the given path.
//
// The target descriptor is exported directly via [archive.WithManifest]
// rather than looking up the image by name. This allows the caller to
// export ephemeral content (e.g., a mutated manifest with an extra layer)
// without modifying the stored image record. The image name is attached
// as the OCI reference annotation on the archive entry. When the target
// is a multi-platform index, only the manifest matching the container's
// platform is included.
func (c *Container) exportImage(ctx context.Context, target ocispec.Descriptor, imageName, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	p, err := platforms.Parse(c.platform)
	if err != nil {
		return err
	}

	return c.client.Export(ctx, f,
		archive.WithManifest(target, imageName),
		archive.WithPlatform(platforms.Only(p)),
	)
}

// Builds the export target descriptor by applying a mutation to the image's
// manifest and config.
//
// The mutated manifest, config, and (when the root is an index) a new
// single-entry index are written to the content store as ephemeral blobs.
// The stored image record is never modified, so subsequent builds always
// see the original, clean image pulled from the registry.
func (c *Container) buildExportTarget(ctx context.Context, imageName string, mutate func(*ocispec.Manifest, *ocispec.Image)) (ocispec.Descriptor, error) {
	is := c.client.ImageService()

	img, err := is.Get(ctx, imageName)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	target, index, manifestIdx, err := c.resolveManifestDescriptor(ctx, img.Target, imageName)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	newManifestDesc, err := c.mutateManifest(ctx, target, imageName, mutate)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return c.buildImageTarget(ctx, img.Target, index, manifestIdx, newManifestDesc, imageName)
}

// Resolves the image root descriptor to a platform-specific manifest.
//
// If the root is an OCI Image Index, the index is read and walked to find
// the manifest matching the container's platform. Returns the manifest
// descriptor, the index (nil when the root is al+,,ready a manifest), and the
// position of the manifest within the index.
//
// Some registries (notably Docker Hub) serve index entries without explicit
// platform metadata. When a descriptor lacks a platform field, the manifest
// and its config are read to extract the platform from the image config, the
// same fallback that containerd's images.Manifest uses internally.
func (c *Container) resolveManifestDescriptor(ctx context.Context, root ocispec.Descriptor, imageName string) (ocispec.Descriptor, *ocispec.Index, int, error) {
	if !images.IsIndexType(root.MediaType) {
		return root, nil, 0, nil
	}

	idx, err := c.readIndex(ctx, root)
	if err != nil {
		return ocispec.Descriptor{}, nil, 0, err
	}

	p, err := platforms.Parse(c.platform)
	if err != nil {
		return ocispec.Descriptor{}, nil, 0, err
	}

	i, ok := c.matchManifest(ctx, idx, platforms.OnlyStrict(p))
	if ok {
		return idx.Manifests[i], &idx, i, nil
	}

	if len(idx.Manifests) == 0 {
		return ocispec.Descriptor{}, nil, 0, crex.Wrapf(ErrEmptyIndex, "%s", imageName)
	}
	return idx.Manifests[0], &idx, 0, nil
}

// Searches the index for a manifest matching the given platform.
//
// Descriptors with an explicit platform field are checked first. If none
// match, descriptors without a platform field are probed by reading the
// image config to discover the platform (the "ConfigPlatform" fallback).
// Returns the index position and true when a match is found.
func (c *Container) matchManifest(ctx context.Context, idx ocispec.Index, matcher platforms.MatchComparer) (int, bool) {
	for i, m := range idx.Manifests {
		if m.Platform != nil && matcher.Match(*m.Platform) {
			return i, true
		}
	}
	for i, m := range idx.Manifests {
		if m.Platform != nil || !images.IsManifestType(m.MediaType) {
			continue
		}
		if p, ok := c.configPlatform(ctx, m); ok && matcher.Match(p) {
			return i, true
		}
	}
	return 0, false
}

// Reads the image config referenced by a manifest descriptor and returns the
// platform declared in the config.
//
// Returns false when the config cannot be read.
func (c *Container) configPlatform(ctx context.Context, desc ocispec.Descriptor) (ocispec.Platform, bool) {
	manifest, err := c.readManifest(ctx, desc)
	if err != nil {
		return ocispec.Platform{}, false
	}
	config, err := c.readConfig(ctx, manifest.Config)
	if err != nil {
		return ocispec.Platform{}, false
	}
	return ocispec.Platform{
		OS:           config.OS,
		Architecture: config.Architecture,
		Variant:      config.Variant,
	}, true
}

// Reads the manifest and config, applies the mutation, and writes the
// updated blobs back to the content store.
func (c *Container) mutateManifest(ctx context.Context, target ocispec.Descriptor, imageName string, mutate func(*ocispec.Manifest, *ocispec.Image)) (ocispec.Descriptor, error) {
	manifest, err := c.readManifest(ctx, target)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	config, err := c.readConfig(ctx, manifest.Config)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	mutate(&manifest, &config)

	newConfigDesc, err := c.writeBlob(ctx, manifest.Config.MediaType, config, imageName+"-config")
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	manifest.Config = newConfigDesc

	return c.writeBlob(ctx, target.MediaType, manifest, imageName+"-manifest", content.WithLabels(manifestGCLabels(manifest)))
}

// Produces the final image target descriptor after a manifest update.
//
// When the image was resolved through an index, a new single-entry index is
// written containing only the updated manifest. Entries for other platforms
// are dropped because their layer blobs are typically not present in the
// content store (only the target platform's layers are fetched).
func (c *Container) buildImageTarget(ctx context.Context, root ocispec.Descriptor, index *ocispec.Index, manifestIdx int, newManifest ocispec.Descriptor, imageName string) (ocispec.Descriptor, error) {
	if index == nil {
		return newManifest, nil
	}

	index.Manifests = []ocispec.Descriptor{newManifest}
	return c.writeBlob(ctx, root.MediaType, index, imageName+"-index", content.WithLabels(indexGCLabels(*index)))
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

// Loads an OCI image index from the content store.
func (c *Container) readIndex(ctx context.Context, desc ocispec.Descriptor) (ocispec.Index, error) {
	b, err := content.ReadBlob(ctx, c.client.ContentStore(), desc)
	if err != nil {
		return ocispec.Index{}, err
	}
	var idx ocispec.Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return ocispec.Index{}, err
	}
	return idx, nil
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

// Computes containerd GC reference labels for an index's children.
func indexGCLabels(idx ocispec.Index) map[string]string {
	labels := make(map[string]string, len(idx.Manifests))
	for i, m := range idx.Manifests {
		key := fmt.Sprintf("containerd.io/gc.ref.content.m.%d", i)
		labels[key] = m.Digest.String()
	}
	return labels
}
