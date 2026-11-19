package server

import (
	"sync"

	"github.com/cruciblehq/spec/agent"
)

// Tracks live containers by handle.
//
// Load creates a container and registers it under a generated handle; later
// operations look it up by that handle, and Destroy removes it. Safe for
// concurrent use.
type registry struct {
	mu         sync.Mutex                            // Protects the containers map from concurrent access.
	containers map[agent.ContainerID]agent.Container // Live containers by handle.
}

// Creates an empty registry.
func newRegistry() *registry {
	return &registry{
		containers: make(map[agent.ContainerID]agent.Container),
	}
}

// Registers c under id.
func (r *registry) add(id agent.ContainerID, c agent.Container) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.containers[id] = c
}

// Returns the container registered under id, if any.
func (r *registry) get(id agent.ContainerID) (agent.Container, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.containers[id]
	return c, ok
}

// Removes the container registered under id.
func (r *registry) remove(id agent.ContainerID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.containers, id)
}
