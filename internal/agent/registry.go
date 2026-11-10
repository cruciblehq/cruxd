package agent

import (
	"sync"

	"github.com/cruciblehq/spec/compute"
)

// Tracks live containers by handle.
//
// Load creates a container and registers it under a generated handle; later
// operations look it up by that handle, and Destroy removes it. Safe for
// concurrent use.
type registry struct {
	mu         sync.Mutex                                // Protects the containers map from concurrent access
	containers map[compute.ContainerID]compute.Container // Maps container IDs to their corresponding container instances
}

// Creates an empty registry.
func newRegistry() *registry {
	return &registry{
		containers: make(map[compute.ContainerID]compute.Container),
	}
}

// Registers c under id.
func (r *registry) add(id compute.ContainerID, c compute.Container) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.containers[id] = c
}

// Returns the container registered under id, if any.
func (r *registry) get(id compute.ContainerID) (compute.Container, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.containers[id]
	return c, ok
}

// Removes the container registered under id.
func (r *registry) remove(id compute.ContainerID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.containers, id)
}
