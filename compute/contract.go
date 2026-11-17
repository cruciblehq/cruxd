package compute

import (
	"context"

	"github.com/cruciblehq/spec/compute"
)

// Default containerd socket and namespace used inside the machine.
const (
	DefaultSocket    = "/run/containerd/containerd.sock" // Containerd socket inside the machine.
	DefaultNamespace = "crux"                            // Containerd namespace for all operations.
)

// Make sure the direct implementation satisfies the spec.
var (
	_ compute.Container = (*Container)(nil)
	_ compute.Runtime   = runtimeAdapter{}
)

// Adapts [Client] to [compute.Runtime].
//
// Embeds the client to promote its image and lifecycle methods and overrides
// only Load so that it returns the [compute.Container] interface rather than
// the concrete [Container].
type runtimeAdapter struct {
	*Client
}

// Creates a container and returns it as the contract interface type.
func (a runtimeAdapter) Load(ctx context.Context, ref compute.ImageRef, opts compute.Options) (compute.Container, error) {
	return a.Client.Load(ctx, ref, opts)
}

// Connects to containerd at socket within namespace and returns a Runtime.
func New(socket, namespace string) (compute.Runtime, error) {
	c, err := newClient(socket, namespace)
	if err != nil {
		return nil, err
	}
	return runtimeAdapter{c}, nil
}
