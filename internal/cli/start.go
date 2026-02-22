package cli

import (
	"context"
	"log/slog"

	"github.com/cruciblehq/cruxd/internal/server"
)

// Represents the 'cruxd start' command.
type StartCmd struct{}

// Executes the start command.
//
// Starts the gRPC server on a Unix domain socket and blocks until the context
// is cancelled (e.g. via SIGINT or SIGTERM).
func (c *StartCmd) Run(ctx context.Context) error {
	srv, err := server.New(server.Config{
		SocketPath: RootCmd.Socket,
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
