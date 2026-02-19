package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/cruciblehq/cruxd/internal"
	"github.com/cruciblehq/cruxd/internal/build"
	"github.com/cruciblehq/spec/protocol"
)

// Handles a build command.
//
// Receives a recipe from crux and executes it against the container runtime.
func (s *Server) handleBuild(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.BuildRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	result, err := build.Run(ctx, s.runtime, build.Options{
		Recipe:       req.Recipe,
		Resource:     req.Resource,
		Output:       req.Output,
		Root:         req.Root,
		Entrypoint:   req.Entrypoint,
		Platforms:    req.Platforms,
	})
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.mu.Lock()
	s.builds++
	s.mu.Unlock()

	s.respond(conn, protocol.CmdOK, &protocol.BuildResult{Output: result.Output})
}

// Handles a status command.
func (s *Server) handleStatus(conn net.Conn) {
	s.mu.Lock()
	builds := s.builds
	s.mu.Unlock()

	uptime := time.Since(s.startedAt).Truncate(time.Second)

	s.respond(conn, protocol.CmdOK, &protocol.StatusResult{
		Running: true,
		Version: internal.VersionString(),
		Pid:     os.Getpid(),
		Uptime:  uptime.String(),
		Builds:  builds,
	})
}

// Handles a shutdown command.
func (s *Server) handleShutdown(conn net.Conn) {
	s.respond(conn, protocol.CmdOK, nil)
	slog.Info("shutdown requested")

	go func() {
		s.Stop()
	}()
}
