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
	"github.com/cruciblehq/cruxd/compute"
	"github.com/cruciblehq/cruxd/identity"
	speccompute "github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/httpx"
)

// Port allocation.
//
// The control port is the endpoint for the crux/cruxd API; it is reserved and
// never reused. Published ports are assigned by cruxd from a range to services
// to avoid conflicts with the control port and the OS ephemeral port range.
const (

	// Environment variable overriding the control port.
	envPort = "CRUXD_PORT"
)

// Server timeouts.
const (

	// Timeout for reading request headers.
	readHeaderTimeout = 15 * time.Second

	// Idle time before a keep-alive connection is closed.
	idleTimeout = 120 * time.Second

	// Grace period for in-flight requests to finish during shutdown.
	shutdownTimeout = 10 * time.Second
)

// Returns the listen port (configured envPort, or the spec default).
func port() string {
	if p := os.Getenv(envPort); p != "" {
		return p
	}
	return strconv.Itoa(speccompute.DefaultPort)
}

// Directory holding the machine identity once enrolled.
const stateDir = "/var/lib/cruxd"

// Starts the agent, connects to containerd, and runs until interrupted.
func main() {
	logger := newLogger()

	id, err := identity.Load(stateDir, trust.EnrollmentPath)
	if err != nil {
		logger.Error("failed to load machine identity", "dir", stateDir, "error", err)
		os.Exit(1)
	}
	if !id.Enrolled() {
		logger.Info("machine is not enrolled, serving enrollment only")
	}

	// Connect to containerd
	runtime, err := compute.New(compute.DefaultSocket, compute.DefaultNamespace)
	if err != nil {
		logger.Error("failed to connect to containerd", "socket", compute.DefaultSocket, "error", err)
		os.Exit(1)
	}
	defer runtime.Close()

	build := agent.Build{
		Version: internal.Build.Version(),
		Commit:  internal.Build.Commit(),
	}

	tlsConfig := trust.ServerConfig(id.Certificate, id.Root(), id.Revocations)
	run(logger, newServer(agent.New(runtime, id, build), tlsConfig))
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
