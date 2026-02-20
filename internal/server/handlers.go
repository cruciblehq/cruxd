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
		Recipe:     req.Recipe,
		Resource:   req.Resource,
		Output:     req.Output,
		Root:       req.Root,
		Entrypoint: req.Entrypoint,
		Platforms:  req.Platforms,
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

// Handles an image-import command.
func (s *Server) handleImageImport(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ImageImportRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	tag := protocol.ImageTag(req.Ref, req.Version)

	if err := s.runtime.ImportImage(ctx, req.Path, tag); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, nil)
}

// Handles an image-start command.
func (s *Server) handleImageStart(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ImageStartRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	tag := protocol.ImageTag(req.Ref, req.Version)

	if _, err := s.runtime.StartFromTag(ctx, tag, req.ID); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, nil)
}

// Handles an image-destroy command.
func (s *Server) handleImageDestroy(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ImageDestroyRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	tag := protocol.ImageTag(req.Ref, req.Version)

	if err := s.runtime.DestroyImage(ctx, tag); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, nil)
}

// Handles a container-stop command.
func (s *Server) handleContainerStop(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ContainerStopRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	ctr := s.runtime.Container(req.ID)
	if err := ctr.Stop(ctx); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, nil)
}

// Handles a container-destroy command.
func (s *Server) handleContainerDestroy(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ContainerDestroyRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	ctr := s.runtime.Container(req.ID)
	ctr.Destroy(ctx)

	s.respond(conn, protocol.CmdOK, nil)
}

// Handles a container-status command.
func (s *Server) handleContainerStatus(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ContainerStatusRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	ctr := s.runtime.Container(req.ID)
	status, err := ctr.Status(ctx)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, &protocol.ContainerStatusResult{Status: status})
}

// Handles a container-exec command.
func (s *Server) handleContainerExec(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ContainerExecRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	ctr := s.runtime.Container(req.ID)
	result, err := ctr.ExecArgs(ctx, req.Command)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, &protocol.ContainerExecResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	})
}

// Handles a container-update command.
func (s *Server) handleContainerUpdate(ctx context.Context, conn net.Conn, payload json.RawMessage) {
	req, err := protocol.DecodePayload[protocol.ContainerUpdateRequest](payload)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	tag := protocol.ImageTag(req.Ref, req.Version)
	ctr := s.runtime.Container(req.ID)

	if err := ctr.Stop(ctx); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	if err := s.runtime.ImportImage(ctx, req.Path, tag); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	if _, err := s.runtime.StartFromTag(ctx, tag, req.ID); err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	s.respond(conn, protocol.CmdOK, nil)
}
