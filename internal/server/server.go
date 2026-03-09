package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/cruxd/internal/runtime"
	"github.com/cruciblehq/spec/paths"
	"github.com/cruciblehq/spec/protocol"
)

const (

	// Default containerd socket address.
	DefaultContainerdAddress = "/run/containerd/containerd.sock"

	// Default containerd namespace for images and containers.
	DefaultContainerdNamespace = "cruxd"

	// Group name used to grant socket access. Members of this group can
	// connect to the daemon socket without owning the process.
	socketGroup = "cruxd"

	// File mode applied to the Unix socket. Owner and group get read-write
	// (required for connect); others get no access.
	socketMode = 0660
)

// Holds server configuration.
type Config struct {
	SocketPath          string // Override for the Unix socket path. Empty uses the default.
	PIDFilePath         string // Override for the PID file path. Empty uses the default.
	ContainerdAddress   string // Containerd socket address. Empty uses [DefaultContainerdAddress].
	ContainerdNamespace string // Containerd namespace for images and containers. Empty uses [DefaultContainerdNamespace].
	ReadyFD             int    // File descriptor to signal readiness on. Negative means disabled.
}

// Listens on a Unix domain socket and dispatches commands.
type Server struct {
	socketPath  string           // Path to the Unix socket file.
	pidFilePath string           // Path to the PID file.
	readyFD     int              // File descriptor for readiness signaling (-1 = disabled).
	runtime     *runtime.Runtime // Containerd-backed container runtime.
	listener    net.Listener     // Listener for incoming connections.
	startedAt   time.Time        // Timestamp when the server started.
	builds      int              // Total number of build commands processed.
	done        chan struct{}    // Channel to signal server shutdown.
	mu          sync.Mutex       // Mutex to protect shared state.
}

// Creates a new server instance.
//
// The socket is not opened until [Start] is called.
func New(cfg Config) (*Server, error) {
	socketPath := cfg.SocketPath
	if socketPath == "" {
		socketPath = paths.Socket("default")
	}

	pidFilePath := cfg.PIDFilePath
	if pidFilePath == "" {
		pidFilePath = paths.PIDFile("default")
	}

	containerdAddress := cfg.ContainerdAddress
	if containerdAddress == "" {
		containerdAddress = DefaultContainerdAddress
	}

	containerdNamespace := cfg.ContainerdNamespace
	if containerdNamespace == "" {
		containerdNamespace = DefaultContainerdNamespace
	}

	rt, err := runtime.New(containerdAddress, containerdNamespace)
	if err != nil {
		return nil, crex.Wrap(ErrServer, err)
	}

	return &Server{
		socketPath:  socketPath,
		pidFilePath: pidFilePath,
		readyFD:     cfg.ReadyFD,
		runtime:     rt,
		done:        make(chan struct{}),
	}, nil
}

// Opens the Unix socket and begins accepting connections.
func (s *Server) Start() error {
	listener, err := listen(s.socketPath)
	if err != nil {
		return err
	}

	s.listener = listener
	s.startedAt = time.Now()

	if err := writePID(s.pidFilePath); err != nil {
		slog.Error("failed to write PID file", "error", err)
	}

	slog.Info("server listening on socket", "path", s.socketPath)

	s.signalReady()

	go s.accept()
	return nil
}

// Signals readiness to the parent process via the ready-fd.
//
// The ready-fd is a bootstrap channel that solves a sequencing problem: crux
// needs to know when the Unix socket is bound and accepting connections, but
// it cannot use the socket itself for that signal because the socket does not
// exist yet when crux needs to start waiting. The ready-fd (a pipe on Linux,
// stdout on Darwin) bridges the gap between process start and socket readiness.
//
// Writes a [protocol.CmdOK] envelope followed by a newline. If the fd is not
// a standard stream (stdin, stdout, stderr), it is closed after writing so
// the reader receives EOF. Standard streams are left open because on Darwin
// the ready-fd is stdout, and closing it would tear down the limactl SSH
// session that keeps cruxd alive.
func (s *Server) signalReady() {
	if s.readyFD < 0 {
		return
	}
	f := os.NewFile(uintptr(s.readyFD), "ready-fd")
	if f == nil {
		slog.Error("invalid file descriptor", "fd", s.readyFD)
		return
	}
	data, err := protocol.Encode(protocol.CmdOK, nil)
	if err != nil {
		slog.Error("failed to encode ready message", "fd", s.readyFD, "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		slog.Error("failed to signal readiness", "fd", s.readyFD, "error", err)
	}
	if s.readyFD > 2 {
		f.Close()
	}
}

// Creates the Unix socket listener, removes any stale socket from a previous
// run, and applies permissions.
func listen(socketPath string) (net.Listener, error) {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, paths.DefaultDirMode); err != nil {
		return nil, crex.Wrap(ErrServer, err)
	}

	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, crex.Wrapf(ErrServer, "failed to listen on %s", socketPath)
	}

	setSocketPermissions(socketPath)

	return listener, nil
}

// Restricts socket access to owner and group where supported.
//
// On virtiofs mounts (used by Lima on Darwin), permission changes may fail
// because the host filesystem controls access. This is non-fatal since the
// socket is already usable by the creating process.
func setSocketPermissions(socketPath string) {
	if err := os.Chmod(socketPath, socketMode); err != nil {
		slog.Debug("failed to chmod socket, filesystem may not support it", "path", socketPath, "error", err)
		return
	}

	if g, err := user.LookupGroup(socketGroup); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil {
			if err := os.Chown(socketPath, -1, gid); err != nil {
				slog.Warn("failed to chgrp socket", "group", socketGroup, "error", err)
			}
		}
	} else {
		slog.Warn("socket group not found, socket accessible to owner only", "group", socketGroup)
	}
}

// Shuts down the server and cleans up resources.
func (s *Server) Stop() error {
	close(s.done)

	if s.listener != nil {
		s.listener.Close()
	}

	if s.runtime != nil {
		s.runtime.Close()
	}

	os.Remove(s.socketPath)
	os.Remove(s.pidFilePath)

	return nil
}

// Blocks until the server stops.
func (s *Server) Wait() {
	<-s.done
}

// Accepts connections in a loop until the server shuts down.
func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				slog.Error("accept error", "error", err)
				continue
			}
		}

		go s.handle(conn)
	}
}

// Processes a single connection.
//
// Reads one newline-delimited JSON message, dispatches the command, and
// writes the response. The connection is closed after one exchange.
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	line, err := reader.ReadBytes(byte(10))
	if err != nil {
		slog.Error("read error", "error", err)
		return
	}

	env, payload, err := protocol.Decode(line)
	if err != nil {
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{Message: err.Error()})
		return
	}

	slog.Info("command received", "command", env.Command)

	ctx, cancel := contextWithDisconnect(context.Background(), reader)
	defer cancel()

	s.dispatch(ctx, conn, env.Command, payload)
}

// Routes a command to the appropriate handler.
func (s *Server) dispatch(ctx context.Context, conn net.Conn, cmd protocol.Command, payload json.RawMessage) {
	switch cmd {
	case protocol.CmdBuild:
		s.handleBuild(ctx, conn, payload)
	case protocol.CmdImageImport:
		s.handleImageImport(ctx, conn, payload)
	case protocol.CmdImageStart:
		s.handleImageStart(ctx, conn, payload)
	case protocol.CmdImageDestroy:
		s.handleImageDestroy(ctx, conn, payload)
	case protocol.CmdContainerStop:
		s.handleContainerStop(ctx, conn, payload)
	case protocol.CmdContainerDestroy:
		s.handleContainerDestroy(ctx, conn, payload)
	case protocol.CmdContainerStatus:
		s.handleContainerStatus(ctx, conn, payload)
	case protocol.CmdContainerExec:
		s.handleContainerExec(ctx, conn, payload)
	case protocol.CmdContainerUpdate:
		s.handleContainerUpdate(ctx, conn, payload)
	case protocol.CmdStatus:
		s.handleStatus(ctx, conn)
	default:
		s.respond(conn, protocol.CmdError, &protocol.ErrorResult{
			Message: fmt.Sprintf("unknown command: %s", cmd),
		})
	}
}

// Writes a JSON envelope response to the connection.
func (s *Server) respond(conn net.Conn, cmd protocol.Command, payload any) {
	data, err := protocol.Encode(cmd, payload)
	if err != nil {
		slog.Error("encode response failed", "error", err)
		return
	}
	data = append(data, byte(10))
	conn.Write(data)
}

// Writes the daemon PID to the PID file so the CLI can detect whether the
// daemon is already running and send it signals.
func writePID(pidFilePath string) error {
	dir := filepath.Dir(pidFilePath)
	if err := os.MkdirAll(dir, paths.DefaultDirMode); err != nil {
		return err
	}
	return os.WriteFile(pidFilePath, []byte(fmt.Sprintf("%d", os.Getpid())), paths.DefaultFileMode)
}

// Returns a derived context that is cancelled when the remote end of the
// connection closes.
//
// Detection works by reading from r in a background goroutine. The read blocks
// until the peer closes the connection, at which point it returns an error and
// the derived context is cancelled. The caller must ensure that no further data
// is expected on r for the lifetime of the returned context. If data arrives
// unexpectedly, it will be discarded and the context will be cancelled
// prematurely. The returned [context.CancelFunc] must always be called to
// release resources, even if the connection closes on its own.
func contextWithDisconnect(parent context.Context, r io.Reader) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	go func() {
		buf := make([]byte, 1)
		r.Read(buf)
		cancel()
	}()

	return ctx, cancel
}
