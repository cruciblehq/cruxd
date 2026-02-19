// Package server implements the cruxd daemon.
//
// The daemon listens on a Unix domain socket for JSON-encoded commands
// from the crux CLI. Each connection carries a single request-response
// exchange: the client sends a newline-delimited JSON envelope, the
// server dispatches the command, and writes the result back before
// closing the connection.
//
// Supported commands include building resources, querying daemon status,
// and initiating shutdown. Build commands are delegated to the build
// package, which in turn uses the runtime package for container
// operations against containerd.
//
// Example usage:
//
//	srv, err := server.New(server.Config{
//	    ContainerdAddress:   "/run/containerd/containerd.sock",
//	    ContainerdNamespace: "crucible",
//	})
//	if err != nil {
//	    return err
//	}
//
//	if err := srv.Start(); err != nil {
//	    return err
//	}
//	defer srv.Stop()
//
//	srv.Wait()
package server
