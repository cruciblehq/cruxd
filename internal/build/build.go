package build

import (
	"context"
	"log/slog"
	"os"
	goruntime "runtime"

	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/spec/paths"
	"github.com/cruciblehq/cruxd/internal/runtime"
	"github.com/cruciblehq/spec/manifest"
)

// Controls recipe execution.
type Options struct {
	Recipe     *manifest.Recipe // Recipe to execute.
	Resource   string           // Resource name, used as a prefix for container IDs.
	Output     string           // Directory for the exported image.
	Root       string           // Project root, for resolving copy sources.
	Entrypoint []string         // OCI entrypoint for the output image (services only).
	Platforms  []string         // Target platforms (e.g., ["linux/amd64"]). Defaults to host.
}

// Returned after successful recipe execution.
type Result struct {
	Output string // Directory containing the exported image.
}

// Executes a recipe against the container runtime.
//
// Stages are built in declaration order. Each stage starts a container from
// its base image, executes the stage's steps, and the non-transient stage is
// exported as the final image to the output directory.
func Run(ctx context.Context, rt *runtime.Runtime, opts Options) (*Result, error) {
	if len(opts.Platforms) == 0 {
		opts.Platforms = []string{"linux/" + goruntime.GOARCH}
	}

	slog.Info("executing recipe",
		"resource", opts.Resource,
		"output", opts.Output,
		"stages", len(opts.Recipe.Stages),
		"platforms", opts.Platforms,
	)

	if err := os.MkdirAll(opts.Output, paths.DefaultDirMode); err != nil {
		return nil, crex.Wrap(ErrFileSystemOperation, err)
	}

	return newRecipe(rt, opts).build(ctx, opts.Recipe.Stages)
}
