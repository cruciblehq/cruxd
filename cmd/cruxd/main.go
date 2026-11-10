package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/cruciblehq/cruxd/cmd/cruxd/internal"
	"github.com/cruciblehq/cruxd/internal/agent"
	"github.com/cruciblehq/cruxd/internal/compute"
	speccompute "github.com/cruciblehq/spec/compute"
)

// Port allocation.
//
// The control port is the endpoint for the crux/cruxd API; it is reserved and
// never reused. Published ports are assigned by cruxd from a range to services
// to avoid conflicts with the control port and the OS ephemeral port range.
const (

	// Environment variable overriding the control port.
	envPort = "CRUXD_PORT"

	// Inclusive bounds of the range cruxd allocates for published service ports.
	publishPortMin = 20000
	publishPortMax = 29999
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

// Starts the agent, connects to containerd, and runs until interrupted.
func main() {
	logger := newLogger()

	// Connect to containerd
	runtime, err := compute.New(compute.DefaultSocket, compute.DefaultNamespace)
	if err != nil {
		logger.Error("failed to connect to containerd", "socket", compute.DefaultSocket, "error", err)
		os.Exit(1)
	}
	defer runtime.Close()

	build := agent.Build{
		Version: internal.Version(),
		Commit:  internal.GitCommit(),
	}

	run(logger, newServer(agent.New(runtime, build)))
}

// Builds the logger.
//
// The logger is configured with log level based on the debug flag.
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if internal.IsDebug() {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// Builds the HTTP server for handler on the configured control port.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%s", port()),
		Handler: handler,

		// Request and response bodies can be large (e.g., image archives) and
		// long (e.g., streaming exec), so r/w timeouts are left unset.
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Serves until a termination signal arrives, then shuts down gracefully.
func run(logger *slog.Logger, srv *http.Server) {
	go func() {
		logger.Info("starting cruxd", "addr", srv.Addr, "version", internal.VersionString())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down cruxd")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "error", err)
		os.Exit(1)
	}
}
