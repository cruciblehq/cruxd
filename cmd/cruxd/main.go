package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/cruciblehq/cruxd/agent"
	"github.com/cruciblehq/cruxd/cmd/cruxd/internal"
	"github.com/cruciblehq/cruxd/identity"
	"github.com/cruciblehq/cruxd/server"
	specagent "github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/httpx"
)

// Control port configuration.
//
// The control port serves the agent protocol to crux.
const (
	envPort = "CRUXD_PORT" // Environment variable overriding the control port.
)

// Server timeouts.
const (
	readHeaderTimeout = 15 * time.Second  // Timeout for reading request headers.
	idleTimeout       = 120 * time.Second // Idle time before a keep-alive connection is closed.
	shutdownTimeout   = 10 * time.Second  // Grace period for in-flight requests to finish during shutdown.
)

// Returns the control port, from [envPort] when set and the spec default otherwise.
func port() string {
	if p := os.Getenv(envPort); p != "" {
		return p
	}
	return strconv.Itoa(specagent.DefaultPort)
}

// Environment variable holding the path of the enrollment data.
//
// Whoever provisions the machine writes the data and sets this. When it is
// unset, the agent has no enrollment data.
const envEnrollment = "CRUXD_ENROLLMENT"

// Directory holding the agent identity once enrolled.
const stateDir = "/var/lib/cruxd"

// Loads the agent identity, starts the agent on containerd, and serves the
// agent protocol until interrupted.
func main() {
	logger := newLogger()

	id, err := identity.Load(stateDir, os.Getenv(envEnrollment))
	if err != nil {
		logger.Error("failed to load agent identity", "dir", stateDir, "error", err)
		os.Exit(1)
	}
	if !id.Enrolled() {
		logger.Info("agent is not enrolled, serving enrollment only")
	}

	client, err := agent.New(agent.DefaultSocket, agent.DefaultNamespace)
	if err != nil {
		logger.Error("failed to connect to containerd", "socket", agent.DefaultSocket, "error", err)
		os.Exit(1)
	}
	defer client.Close()
	if err := client.Prune(context.Background()); err != nil {
		logger.Error("failed to remove containers left by an earlier run", "error", err)
		os.Exit(1)
	}

	build := server.Build{
		Version: internal.Build.Version(),
		Commit:  internal.Build.Commit(),
	}

	tlsConfig := trust.ServerConfig(id.Certificate, id.Root(), id.Revocations)
	run(logger, newServer(server.New(client, id, build), tlsConfig))
}

// Builds the logger.
//
// Records are written to stderr as JSON lines with all their attributes. The
// level is debug for debug builds and info otherwise.
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if internal.Debug {
		level = slog.LevelDebug
	}
	return slog.New(crex.NewHandlerWithLevel(level).SetFormatter(&crex.JSONFormatter{Verbose: true}))
}

// Builds the HTTPS server for handler on the configured control port.
func newServer(handler http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Addr:      fmt.Sprintf(":%s", port()),
		Handler:   handler,
		TLSConfig: tlsConfig,

		// Request and response bodies can be large (e.g., image archives) and
		// long (e.g., streaming exec), so r/w timeouts are left unset.
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Serves until a termination signal arrives, then shuts down gracefully.
func run(logger *slog.Logger, srv *http.Server) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("starting cruxd", "addr", srv.Addr, "version", internal.Build.String())
	if err := httpx.Serve(ctx, srv, shutdownTimeout); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
	logger.Info("cruxd stopped")
}
