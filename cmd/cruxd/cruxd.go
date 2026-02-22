package main

import (
	"log/slog"
	"os"

	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/cruxd/internal"
	"github.com/cruciblehq/cruxd/internal/cli"
)

// The entry point for the cruxd daemon.
//
// Initializes logging, displays startup information, and executes the root
// command. If any error occurs during execution, it exits with a non-zero code.
func main() {
	slog.SetDefault(logger())

	slog.Debug("build", "version", internal.VersionString())

	slog.Debug("cruxd is running",
		"pid", os.Getpid(),
		"cwd", cwd(),
		"args", os.Args,
	)

	if err := cli.Execute(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
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
