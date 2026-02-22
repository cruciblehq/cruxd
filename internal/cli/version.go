package cli

import (
	"context"
	"fmt"

	"github.com/cruciblehq/cruxd/internal"
)

// Represents the 'cruxd version' command.
type VersionCmd struct{}

// Executes the version command.
func (c *VersionCmd) Run(ctx context.Context) error {
	fmt.Println(internal.VersionString())
	return nil
}
