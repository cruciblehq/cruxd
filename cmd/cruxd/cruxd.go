package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/cruxd/internal"
	"github.com/cruciblehq/cruxd/internal/cli"
	"github.com/cruciblehq/cruxd/internal/server"
)

// Starts the Crucible daemon.
//
// Initializes logging, parses flags, starts the server, and blocks until a
// termination signal is received. The server listens on a Unix domain socket
// for commands from the crux CLI.
func main() {
	slog.SetDefault(logger())

	slog.Debug("build", "version", internal.VersionString())

	slog.Debug("cruxd is running",
		"pid", os.Getpid(),
		"cwd", cwd(),
		"args", os.Args,
	)

	cli.Execute()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

// Runs the daemon until the context is cancelled.
func run(ctx context.Context) error {
	srv, err := server.New(server.Config{
		SocketPath: cli.RootCmd.Socket,
	})
	if err != nil {
		return err
	}

	if err := srv.Start(); err != nil {
		return err
	}

	slog.Info("cruxd is running")

	<-ctx.Done()

	slog.Info("shutting down")
	return srv.Stop()
}

// Creates a buffered logger seeded from build-time linker flags.
//
// The logger is reconfigured after flag parsing via cli.Execute.
func logger() *slog.Logger {
	handler := crex.NewHandler()
	handler.SetLevel(logLevel())
	return slog.New(handler.WithGroup(internal.Name))
}

// Returns the log level derived from build-time linker flags.
func logLevel() slog.Level {
	if internal.IsDebug() {
		return slog.LevelDebug
	}
	if internal.IsQuiet() {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

// Returns the current working directory or "(unknown)".
func cwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "(unknown)"
	}
	return cwd
}
