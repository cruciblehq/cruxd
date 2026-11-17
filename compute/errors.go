package compute

import (
	"github.com/containerd/errdefs"
	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/crex"
)

var (
	ErrConnect   = crex.New("containerd connection failed")
	ErrImport    = crex.New("image import failed")
	ErrNoImages  = crex.New("no images in archive")
	ErrContainer = crex.New("container operation failed")
)

// Wraps a containerd error under [ErrContainer], tagging a missing resource so
// the agent can match it with [compute.ErrNotFound].
func wrapContainerd(err error) error {
	if errdefs.IsNotFound(err) {
		return crex.Tag(crex.Wrap(ErrContainer, err), compute.ErrNotFound)
	}
	return crex.Wrap(ErrContainer, err)
}
