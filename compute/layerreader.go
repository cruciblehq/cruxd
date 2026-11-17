package compute

import (
	"compress/gzip"
	"context"
	"io"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/cruciblehq/utils-go/crex"
)

// An io.ReadCloser over a layer blob (possibly compressed).
type layerReader struct {
	io.Reader
	ra io.Closer    // ReaderAt for the underlying content.
	gr *gzip.Reader // nil for uncompressed layers
}

// Closes the gzip reader (if any) and the underlying content.ReaderAt.
func (l *layerReader) Close() error {
	var grErr error
	if l.gr != nil {
		grErr = l.gr.Close()
	}
	raErr := l.ra.Close()
	if grErr != nil {
		return grErr
	}
	return raErr
}

// Opens the content at desc from cs, decompressing gzip layers automatically.
//
// The caller must close the returned [io.ReadCloser] when done.
func openLayerReader(ctx context.Context, cs content.Store, desc ocispec.Descriptor) (io.ReadCloser, error) {
	ra, err := cs.ReaderAt(ctx, desc)
	if err != nil {
		return nil, crex.Wrapf(ErrContainer, err, "could not read layer %s", desc.Digest)
	}
	r := content.NewReader(ra)
	switch desc.MediaType {
	case ocispec.MediaTypeImageLayerGzip, images.MediaTypeDockerSchema2LayerGzip:
		gr, err := gzip.NewReader(r)
		if err != nil {
			ra.Close()
			return nil, crex.Wrapf(ErrContainer, err, "could not decompress layer")
		}
		return &layerReader{Reader: gr, ra: ra, gr: gr}, nil
	}
	return &layerReader{Reader: r, ra: ra}, nil
}
