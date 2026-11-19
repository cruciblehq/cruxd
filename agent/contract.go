package agent

import (
	"github.com/cruciblehq/spec/agent"
)

// Default containerd socket and namespace on the machine.
const (
	DefaultSocket    = "/run/containerd/containerd.sock" // Containerd socket on the machine.
	DefaultNamespace = "crux"                            // Containerd namespace for all agent operations.
)

// Compile-time checks that Client and Container implement the spec interfaces.
var (
	_ agent.Container = (*Container)(nil)
	_ agent.Agent     = (*Client)(nil)
)
