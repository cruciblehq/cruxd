package compute

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	imgarchive "github.com/containerd/containerd/v2/core/images/archive"
	"github.com/containerd/containerd/v2/core/leases"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/rootfs"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	imgspecs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/crypto"
)

// Number of random bytes in generated step, snapshot, and exec identifiers.
const idLen = 8

// Containerd runtime configuration.
const (

	// OCI runtime used for containers.
	runcV2Runtime = "io.containerd.runc.v2"

	// Timeout for establishing the initial containerd gRPC connection.
	connectTimeout = 10 * time.Second

	// Timeout for one containerd health check.
	healthTimeout = 5 * time.Second

	// Snapshotter used for all container and image operations.
	overlayfsSnapshotter = "overlayfs"

	// Lifetime of the lease that protects a container's writable snapshot and
	// build-time content from garbage collection. It is renewed implicitly by
	// use and deleted when the container is destroyed; the expiration is a
	// safety net so abandoned leases do not leak indefinitely.
	leaseDuration = 24 * time.Hour
)

// Suffix appended to step IDs when naming writable snapshots.
const snapshotIDSuffix = "snapshot"

// Content store and image reference keys.
const (

	// Content store ref key prefix for image config blobs.
	configBlobRefPrefix = "config"

	// Content store ref key prefix for image manifest blobs.
	manifestBlobRefPrefix = "manifest"

	// Image service ref prefix for images built by crux.
	buildImageRefPrefix = "crucible/build"

	// Containerd GC label key that links a manifest to its config blob.
	gcLabelConfig = "containerd.io/gc.ref.content.config"
)

// OCI whiteout markers.
const (

	// Prefix used by OCI whiteout tar entries to mark deleted paths.
	whiteoutPrefix = ".wh."

	// Name of the opaque whiteout entry that deletes all siblings in a directory.
	whiteoutOpaque = ".wh..wh..opq"
)

// A live connection to containerd on a host.
//
// A Client is a handle to containerd on a specific host, providing methods for
// interacting with the host's containerd instance and image store. A client is
// opened by calling [Backend.Connect] and must be closed when no longer needed.
// The client owns the underlying containerd connection, so calling any method
// after [Client.Close] results in undefined behaviour. Containers are created
// via [Client.Load] and own all operations on their writable snapshot.
type Client struct {
	conn      *containerd.Client // Underlying containerd client connection.
	namespace string             // Containerd namespace for all operations on this client.
}

// Connects to the containerd socket at path and returns a Client.
//
// The path should be the same as the one configured for the compute backend in
// use. For the local backend, the default is "/run/containerd/containerd.sock".
// This method sets default namespace and runtime options to match crux's usual
// configurations. Methods using the client may override these defaults by using
// namespaces.WithNamespace or oci.WithRuntime in their contexts or spec options,
// but doing so is not recommended.
func newClient(path, namespace string) (*Client, error) {
	c, err := containerd.New(path,
		containerd.WithDefaultNamespace(namespace),
		containerd.WithDefaultRuntime(runcV2Runtime),
		containerd.WithTimeout(connectTimeout),
	)
	if err != nil {
		return nil, crex.Wrap(ErrConnect, err)
	}
	return &Client{conn: c, namespace: namespace}, nil
}

// Imports an OCI image archive into the host's image store.
//
// The returned reference is a unique identifier within crux's namespace
// and can be passed to [Client.Load], [Client.Export], [Client.Extract],
// or [Client.Remove]. The archive must be in OCI tar format as produced
// by [Client.Export]. The image is unpacked so all layer snapshots exist.
func (c *Client) Import(ctx context.Context, r io.Reader) (compute.ImageRef, error) {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	imgs, err := c.conn.Import(ctx, r)
	if err != nil {
		return "", crex.Wrap(ErrImport, err)
	}
	if len(imgs) == 0 {
		return "", crex.Wrap(ErrImport, ErrNoImages)
	}

	img, err := c.conn.GetImage(ctx, imgs[0].Name)
	if err != nil {
		return "", crex.Wrap(ErrImport, err)
	}

	// Unpack is idempotent; it ensures all layer snapshots exist in the store.
	if err := img.Unpack(ctx, overlayfsSnapshotter); err != nil {
		return "", crex.Wrap(ErrImport, err)
	}

	return compute.ImageRef(img.Name()), nil
}

// Exports the named image as an OCI tar archive.
//
// ref must be a reference returned by [Client.Import] or [Container.Commit].
// The archive includes all layer blobs, manifests, and config descriptors and
// can be re-imported via [Client.Import].
func (c *Client) Export(ctx context.Context, ref compute.ImageRef, w io.Writer) error {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	return c.conn.Export(ctx, w, imgarchive.WithImage(c.conn.ImageService(), string(ref)))
}

// Extracts a directory tree from the named image as a tar stream.
//
// ref must be a reference returned by [Client.Import] or [Container.Commit].
// path is matched against the merged view of all image layers; it is passed
// through [path.Clean] before matching. Callers are responsible for resolving
// path to its final form before calling. The returned [io.ReadCloser] must be
// closed by the caller.
func (c *Client) Extract(ctx context.Context, ref compute.ImageRef, path string) (io.ReadCloser, error) {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	img, err := c.conn.GetImage(ctx, string(ref))
	if err != nil {
		return nil, wrapContainerd(err)
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(c.streamExtract(ctx, img, path, pw))
	}()
	return pr, nil
}

// Returns the OCI image config for the named image.
//
// The returned [ocispec.ImageConfig] contains the runtime defaults baked into
// the image, which are the values in effect before [compute.Options] overrides
// them at container start. ref must be a reference returned by [Client.Import]
// or [Client.Commit].
func (c *Client) Inspect(ctx context.Context, ref compute.ImageRef) (ocispec.ImageConfig, error) {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	img, err := c.conn.GetImage(ctx, string(ref))
	if err != nil {
		return ocispec.ImageConfig{}, wrapContainerd(err)
	}
	_, cfg, err := readImageConfig(ctx, c.conn.ContentStore(), img.Target())
	if err != nil {
		return ocispec.ImageConfig{}, err
	}
	return cfg.Config, nil
}

// Removes the named image from the store.
//
// ref must be a reference returned by [Client.Import] or [Container.Commit].
// The caller is responsible for ensuring no containers are using the image
// before removing it.
func (c *Client) Remove(ctx context.Context, ref compute.ImageRef) error {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	return c.conn.ImageService().Delete(ctx, string(ref))
}

// Creates a new container from a previously imported image.
//
// ref must be a reference returned by [Client.Import] or [Container.Commit].
// Allocates a persistent writable snapshot with the image as parent. The
// container is not started until [Container.Start] is called. The container
// must be released by calling [Container.Destroy] when no longer needed.
func (c *Client) Load(ctx context.Context, ref compute.ImageRef, opts compute.Options) (*Container, error) {
	ctx = namespaces.WithNamespace(ctx, c.namespace)
	img, err := c.conn.GetImage(ctx, string(ref))
	if err != nil {
		return nil, wrapContainerd(err)
	}

	// Unpack is idempotent; safe to call even when layers are already present.
	if err := img.Unpack(ctx, overlayfsSnapshotter); err != nil {
		return nil, crex.Wrap(ErrContainer, err)
	}

	// A lease keeps the writable snapshot and build-time content alive for the
	// container's lifetime. The active snapshot prepared below is not referenced
	// by any container record until Start, so without a lease the garbage
	// collector can remove it mid-build (and with it its on-disk fs directory),
	// which later breaks layer application.
	lease, err := c.conn.LeasesService().Create(ctx, leases.WithRandomID(), leases.WithExpiration(leaseDuration))
	if err != nil {
		return nil, crex.Wrapf(ErrContainer, err, "could not create lease")
	}
	leaseCtx := leases.WithLease(ctx, lease.ID)

	snapshotID, err := c.prepareSnapshot(leaseCtx, img)
	if err != nil {
		_ = c.conn.LeasesService().Delete(ctx, lease)
		return nil, err
	}

	ctr := &Container{
		conn:        c.conn,
		namespace:   c.namespace,
		leaseID:     lease.ID,
		hostNetwork: opts.HostNetwork,
		img:         img,
		snapshotID:  snapshotID,
		snapshotter: overlayfsSnapshotter,
		oci:         opts.OCI,
	}
	return ctr, nil
}

// Checks that containerd is serving requests.
//
// Gives up after [healthTimeout], since the containerd client otherwise waits
// for the connection to recover.
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	serving, err := c.conn.IsServing(ctx)
	if err != nil {
		return crex.Wrap(ErrConnect, err)
	}
	if !serving {
		return crex.Newf(ErrConnect, "containerd is not serving")
	}
	return nil
}

// Closes the client and releases the underlying containerd connection.
//
// Does not affect images or running containers. After calling Close, the
// Client must not be used again.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Prepares a new writable snapshot for img and returns its key.
//
// Computes the chain ID from img's diff IDs, then calls Prepare on the overlayfs
// snapshotter. The returned key identifies the snapshot and must be passed to
// [Container.Destroy] when the container is no longer needed.
func (c *Client) prepareSnapshot(ctx context.Context, img containerd.Image) (string, error) {
	diffIDs, err := img.RootFS(ctx)
	if err != nil {
		return "", crex.Wrapf(ErrContainer, err, "could not read image rootfs")
	}
	parent := identity.ChainID(diffIDs).String()

	snapshotID := fmt.Sprintf("%s-%s", crypto.RandHex(idLen), snapshotIDSuffix)
	if _, err := c.conn.SnapshotService(overlayfsSnapshotter).Prepare(ctx, snapshotID, parent); err != nil {
		return "", crex.Wrap(ErrContainer, err)
	}
	return snapshotID, nil
}

// Extracts src from img as an uncompressed tar stream written to w.
//
// Layers are scanned in order with last-writer-wins semantics; whiteout entries
// are applied before writing. src is passed through [path.Clean] before matching.
// Returns an error when src is not found in any layer.
func (c *Client) streamExtract(ctx context.Context, img containerd.Image, src string, w io.Writer) error {
	cs := c.conn.ContentStore()

	mfst, err := images.Manifest(ctx, cs, img.Target(), nil)
	if err != nil {
		return crex.Wrap(ErrContainer, err)
	}

	entries, err := collectLayerEntries(ctx, cs, mfst.Layers, src)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return crex.Tag(crex.Newf(ErrContainer, "path %q not found in image", src), compute.ErrNotFound)
	}

	return streamEntries(ctx, cs, mfst.Layers, entries, w)
}

// Returns a new image identical to img but with cfg as its runtime configuration.
//
// cfg replaces the Config section of the OCI image. All remaining image fields
// are inherited from img. A history entry is appended with by as the creator,
// the current time, and EmptyLayer set since no filesystem layer is added. The
// new image is registered under a reference, which is returned. The original
// img is not modified.
func configureImage(ctx context.Context, conn *containerd.Client, img containerd.Image, cfg ocispec.ImageConfig, by string) (string, error) {
	mfst, imgCfg, err := readImageConfig(ctx, conn.ContentStore(), img.Target())
	if err != nil {
		return "", err
	}
	imgCfg.Config = cfg
	now := time.Now()
	imgCfg.History = append(imgCfg.History, ocispec.History{
		Created:    &now,
		CreatedBy:  by,
		EmptyLayer: true,
	})
	return writeNewImage(ctx, conn, mfst, imgCfg, nil)
}

// Reads and decodes the OCI manifest and image config for target from cs.
func readImageConfig(ctx context.Context, cs content.Store, target ocispec.Descriptor) (ocispec.Manifest, ocispec.Image, error) {
	mfst, err := images.Manifest(ctx, cs, target, nil)
	if err != nil {
		return ocispec.Manifest{}, ocispec.Image{}, crex.Wrap(ErrContainer, err)
	}
	configData, err := content.ReadBlob(ctx, cs, mfst.Config)
	if err != nil {
		return ocispec.Manifest{}, ocispec.Image{}, crex.Wrapf(ErrContainer, err, "could not read image config")
	}
	var cfg ocispec.Image
	if err := json.Unmarshal(configData, &cfg); err != nil {
		return ocispec.Manifest{}, ocispec.Image{}, crex.Wrapf(ErrContainer, err, "could not unmarshal image config")
	}
	return mfst, cfg, nil
}

// Iterates layers in order and records the winning layer for each path under src.
//
// Uses last-writer-wins semantics; whiteout entries are applied as encountered.
// No file data is read. Returns an error if any layer cannot be read, but not
// if src is not found.
func collectLayerEntries(ctx context.Context, cs content.Store, layers []ocispec.Descriptor, src string) (map[string]ocispec.Descriptor, error) {
	srcClean := path.Clean(src)
	entries := make(map[string]ocispec.Descriptor)
	for _, layer := range layers {
		if err := scanLayerRefs(ctx, cs, layer, srcClean, entries); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// Scans a single layer's tar headers and records the winning layer for each
// matching path under srcClean.
//
// Whiteout entries are applied. File data is always discarded without buffering.
func scanLayerRefs(ctx context.Context, cs content.Store, layer ocispec.Descriptor, srcClean string, entries map[string]ocispec.Descriptor) error {
	lr, err := openLayerReader(ctx, cs, layer)
	if err != nil {
		return err
	}
	defer lr.Close()

	tr := tar.NewReader(lr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return crex.Wrapf(ErrContainer, err, "could not read tar entry")
		}
		if err := recordTarRef(hdr, tr, layer, srcClean, entries); err != nil {
			return err
		}
	}
	return nil
}

// Records a single tar header in entries or applies a whiteout, then discards
// the entry data.
//
// Entries matching srcClean are stored with their source layer. Whiteout entries
// are applied to entries. Data is always discarded without buffering.
func recordTarRef(hdr *tar.Header, tr *tar.Reader, layer ocispec.Descriptor, srcClean string, entries map[string]ocispec.Descriptor) error {
	name := path.Join("/", hdr.Name)
	base := path.Base(name)
	if strings.HasPrefix(base, whiteoutPrefix) {
		applyWhiteout(entries, name, base)
	} else if pathUnder(name, srcClean) {
		entries[name] = layer
	}
	if _, err := io.Copy(io.Discard, tr); err != nil {
		return err
	}
	return nil
}

// Whether name equals dir or is directly or indirectly contained within it.
func pathUnder(name, dir string) bool {
	for {
		if name == dir {
			return true
		}
		parent := path.Dir(name)
		if parent == name { // reached filesystem root
			return false
		}
		name = parent
	}
}

// Applies a whiteout tar entry to entries.
//
// An opaque whiteout (.wh..wh..opq) deletes all entries under the same
// directory. A regular whiteout (.wh.<name>) deletes the named entry.
func applyWhiteout(entries map[string]ocispec.Descriptor, name, base string) {
	if base == whiteoutOpaque {
		dir := path.Dir(name)
		for k := range entries {
			if pathUnder(k, dir) {
				delete(entries, k)
			}
		}
	} else {
		whTarget := path.Join(path.Dir(name), strings.TrimPrefix(base, whiteoutPrefix))
		delete(entries, whTarget)
	}
}

// Streams winning entries from their source layers to w as an uncompressed tar
// stream.
//
// Layers are visited in their original order. Only the winning entry per path
// is streamed; no file data is buffered.
func streamEntries(ctx context.Context, cs content.Store, layers []ocispec.Descriptor, entries map[string]ocispec.Descriptor, w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, layer := range layers {
		names := make(map[string]struct{})
		for name, winningLayer := range entries {
			if winningLayer.Digest == layer.Digest {
				names[name] = struct{}{}
			}
		}
		if len(names) == 0 {
			continue
		}
		if err := streamLayerMatches(ctx, cs, layer, names, tw); err != nil {
			return err
		}
	}
	return tw.Close()
}

// Re-reads layer and streams the entries named in names to tw.
//
// Entries not in names are discarded. Data flows directly from the layer
// decompressor to the tar writer without buffering.
func streamLayerMatches(ctx context.Context, cs content.Store, layer ocispec.Descriptor, names map[string]struct{}, tw *tar.Writer) error {
	lr, err := openLayerReader(ctx, cs, layer)
	if err != nil {
		return err
	}
	defer lr.Close()

	tr := tar.NewReader(lr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return crex.Wrapf(ErrContainer, err, "could not read tar entry")
		}
		if err := streamTarEntry(hdr, tr, names, tw); err != nil {
			return err
		}
	}
	return nil
}

// Streams a single tar entry to tw if its normalised path is in names,
// otherwise discards the data. Always advances tr past the entry data.
func streamTarEntry(hdr *tar.Header, tr *tar.Reader, names map[string]struct{}, tw *tar.Writer) error {
	name := path.Join("/", hdr.Name)
	if _, ok := names[name]; !ok {
		_, err := io.Copy(io.Discard, tr)
		return err
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return crex.Wrapf(ErrContainer, err, "could not write tar header for %q", name)
	}
	if _, err := io.Copy(tw, tr); err != nil {
		return crex.Wrapf(ErrContainer, err, "could not write tar data for %q", name)
	}
	return nil
}

// Commits the diff of snapshotID as a new image layer appended to img.
//
// Creates a temporary lease to protect the new blob from GC until the
// manifest's GC labels are written. Returns the new image reference.
func commitSnapshotDiff(ctx context.Context, conn *containerd.Client, img containerd.Image, snapshotID, snapshotterName string, cfgOverride *ocispec.ImageConfig, by string) (string, error) {
	leaseCtx, doneLease, err := conn.WithLease(ctx)
	if err != nil {
		return "", crex.Wrapf(ErrContainer, err, "create lease")
	}
	defer doneLease(ctx)

	snapshotter := conn.SnapshotService(snapshotterName)
	layerDesc, err := rootfs.CreateDiff(leaseCtx, snapshotID, snapshotter, conn.DiffService())
	if err != nil {
		return "", crex.Wrapf(ErrContainer, err, "create diff")
	}

	diffID, err := uncompressedDigest(leaseCtx, conn, layerDesc)
	if err != nil {
		return "", crex.Wrapf(ErrContainer, err, "compute diff ID")
	}

	return appendLayerToImage(leaseCtx, conn, img, layerDesc, diffID, cfgOverride, by)
}

// Computes the digest of the uncompressed content behind desc.
//
// Reads the compressed blob from the content store, decompresses it, and
// returns the sha256 digest of the resulting stream.
func uncompressedDigest(ctx context.Context, conn *containerd.Client, desc ocispec.Descriptor) (digest.Digest, error) {
	lr, err := openLayerReader(ctx, conn.ContentStore(), desc)
	if err != nil {
		return "", err
	}
	defer lr.Close()

	digester := digest.SHA256.Digester()
	if _, err := io.Copy(digester.Hash(), lr); err != nil {
		return "", err
	}
	return digester.Digest(), nil
}

// Creates a new image by appending a layer to img's manifest.
//
// The new config includes diffID in its DiffIDs and a history entry stamped
// with the current time and by as the creator. When cfgOverride is non-nil
// it replaces the image config from img. Returns the new image reference.
func appendLayerToImage(ctx context.Context, conn *containerd.Client, img containerd.Image, layerDesc ocispec.Descriptor, diffID digest.Digest, cfgOverride *ocispec.ImageConfig, by string) (string, error) {
	mfst, cfg, err := readImageConfig(ctx, conn.ContentStore(), img.Target())
	if err != nil {
		return "", err
	}
	if cfgOverride != nil {
		cfg.Config = *cfgOverride
	}
	now := time.Now()
	cfg.RootFS.DiffIDs = append(cfg.RootFS.DiffIDs, diffID)
	cfg.History = append(cfg.History, ocispec.History{
		Created:   &now,
		CreatedBy: by,
	})
	return writeNewImage(ctx, conn, mfst, cfg, &layerDesc)
}

// Writes a new image to the content store and returns its reference.
//
// Uses parentMfst and cfg to build the manifest. When extraLayer is non-nil
// it is appended to the manifest's layer list.
func writeNewImage(ctx context.Context, conn *containerd.Client, parentMfst ocispec.Manifest, cfg ocispec.Image, extraLayer *ocispec.Descriptor) (string, error) {
	mfstDesc, err := writeImageManifest(ctx, conn, parentMfst, cfg, extraLayer)
	if err != nil {
		return "", err
	}
	return registerImageRef(ctx, conn, mfstDesc)
}

// Writes config and manifest blobs to the content store.
//
// GC reference labels are set on the manifest so the garbage collector can
// trace reachability to config and layer blobs. Returns the manifest descriptor.
func writeImageManifest(ctx context.Context, conn *containerd.Client, parentMfst ocispec.Manifest, cfg ocispec.Image, extraLayer *ocispec.Descriptor) (ocispec.Descriptor, error) {
	cs := conn.ContentStore()

	configDesc, err := writeConfigBlob(ctx, cs, cfg)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	layers := append([]ocispec.Descriptor{}, parentMfst.Layers...)
	if extraLayer != nil {
		layers = append(layers, *extraLayer)
	}

	// GC reference labels let the garbage collector trace reachability from
	// the manifest to its config and layer blobs. Without them the GC may
	// delete layer blobs before Unpack runs.
	gcLabels := map[string]string{
		gcLabelConfig: configDesc.Digest.String(),
	}
	for i, layer := range layers {
		gcLabels[fmt.Sprintf("containerd.io/gc.ref.content.l.%d", i)] = layer.Digest.String()
	}

	newMfst := ocispec.Manifest{
		Versioned: imgspecs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    layers,
	}
	return writeManifestBlob(ctx, cs, newMfst, gcLabels)
}

// Writes the given image config to the content store as a blob.
//
// The descriptor is computed from the raw JSON bytes before writing, so the
// digest and size are always consistent with the stored content. The content
// store ref key is the encoded digest with a configBlobRef prefix. WriteBlob
// is idempotent, so calling this with an identical config is safe. Returns the
// descriptor for the written blob.
func writeConfigBlob(ctx context.Context, cs content.Store, cfg ocispec.Image) (ocispec.Descriptor, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "marshal config")
	}
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	if err := content.WriteBlob(ctx, cs,
		fmt.Sprintf("%s-%s", configBlobRefPrefix, desc.Digest.Encoded()),
		bytes.NewReader(data), desc,
	); err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "write config")
	}
	return desc, nil
}

// Writes the given manifest to the content store as a blob with gcLabels.
//
// gcLabels is applied both at write time and via a follow-up Update call, which
// ensures the labels are present even when the blob already existed in the
// store. The returned descriptor carries the media type, digest, and byte size
// needed to reference the manifest from an index or image service entry.
func writeManifestBlob(ctx context.Context, cs content.Store, mfst ocispec.Manifest, gcLabels map[string]string) (ocispec.Descriptor, error) {
	data, err := json.Marshal(mfst)
	if err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "marshal manifest")
	}
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	if err := content.WriteBlob(ctx, cs,
		fmt.Sprintf("%s-%s", manifestBlobRefPrefix, desc.Digest.Encoded()),
		bytes.NewReader(data), desc,
		content.WithLabels(gcLabels),
	); err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "write manifest")
	}
	if _, err := cs.Update(ctx, content.Info{
		Digest: desc.Digest,
		Labels: gcLabels,
	}, "labels"); err != nil {
		return ocispec.Descriptor{}, crex.Wrapf(ErrContainer, err, "update manifest gc labels")
	}
	return desc, nil
}

// Registers the given manifest descriptor in the containerd image service.
//
// The name is derived from the first 16 hex characters of the manifest digest,
// prefixed with buildImageRefPrefix. If a record for that name already exists
// it is updated in place rather than returning an error, so the call is safe
// to repeat for the same manifest. Returns the reference assigned to the image.
func registerImageRef(ctx context.Context, conn *containerd.Client, mfstDesc ocispec.Descriptor) (string, error) {
	ref := fmt.Sprintf("%s:%s", buildImageRefPrefix, mfstDesc.Digest.Encoded()[:16])
	img := images.Image{Name: ref, Target: mfstDesc}
	if _, err := conn.ImageService().Create(ctx, img); err != nil {
		if !errdefs.IsAlreadyExists(err) {
			return "", crex.Wrapf(ErrContainer, err, "register image")
		}
		if _, err := conn.ImageService().Update(ctx, img); err != nil {
			return "", crex.Wrapf(ErrContainer, err, "register image")
		}
	}
	return ref, nil
}
