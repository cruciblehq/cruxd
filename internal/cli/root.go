package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/cruxd/internal"
)

// Represents the root command for the cruxd daemon.
var RootCmd struct {
	Quiet   bool       `short:"q" help:"Suppress informational output."`
	Verbose bool       `short:"v" help:"Enable verbose output."`
	Debug   bool       `short:"d" help:"Enable debug output."`
	Socket  string     `short:"s" help:"Override the default Unix socket path." placeholder:"PATH"`
	Start   StartCmd   `cmd:"" help:"Start the daemon."`
	Version VersionCmd `cmd:"" help:"Show version information."`
}

// Parses arguments, configures logging, and runs the selected subcommand.
func Execute() error {

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	kongCtx := kong.Parse(&RootCmd,
		kong.Name(internal.Name),
		kong.Description("The Crucible daemon.\n\nListens on a Unix domain socket for commands from the crux CLI."),
		kong.UsageOnError(),
		kong.Vars{
			"version": internal.VersionString(),
		},
		kong.BindTo(ctx, (*context.Context)(nil)),
	)

	configureLogger()

	return kongCtx.Run()
}

// Configures the global logger based on CLI flags.
func configureLogger() {
	handler, ok := slog.Default().Handler().(crex.Handler)
	if !ok {
		return // Not a crex.Handler, nothing to configure
	}

	debug := RootCmd.Debug || internal.IsDebug()
	quiet := RootCmd.Quiet || internal.IsQuiet()
	verbose := RootCmd.Verbose || internal.IsVerbose()

	// Configure formatter
	formatter := crex.NewPrettyFormatter(isatty(os.Stderr))
	formatter.SetVerbose(verbose)

	// Configure handler
	if debug {
		handler.SetLevel(slog.LevelDebug)
	} else if quiet {
		handler.SetLevel(slog.LevelWarn)
	} else {
		handler.SetLevel(slog.LevelInfo)
	}

	// Commit
	handler.SetFormatter(formatter)
	handler.SetStream(os.Stderr)
	handler.Flush()
}

// Whether the given file is an interactive terminal.
func isatty(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
