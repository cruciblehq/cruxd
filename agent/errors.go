package agent

import (
	"github.com/containerd/errdefs"
	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/crex"
)

var (
	ErrConnect   = crex.New("containerd connection failed")
	ErrImport    = crex.New("image import failed")
	ErrNoImages  = crex.New("no images in archive")
	ErrContainer = crex.New("container operation failed")
	ErrInvalidID = crex.New("invalid user or group ID")
)

// Wraps a containerd error under [ErrContainer], tagging a missing resource so
// callers can match it with [agent.ErrNotFound].
func wrapContainerd(err error) error {
	if errdefs.IsNotFound(err) {
		return crex.Tag(crex.Wrap(ErrContainer, err), agent.ErrNotFound)
	}
	return crex.Wrap(ErrContainer, err)
}
