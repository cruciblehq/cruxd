// Package server serves the agent protocol over HTTP.
//
// It routes requests for the endpoints defined in spec/agent to the agent
// that backs it, and handles enrollment and the handshake itself.
package server
