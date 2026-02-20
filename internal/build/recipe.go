package build

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/spec/paths"
	"github.com/cruciblehq/cruxd/internal/runtime"
	"github.com/cruciblehq/spec/manifest"
)

// Holds shared state for building all stages of a recipe.
type recipe struct {
	rt         *runtime.Runtime     // Container runtime for image and container operations.
	resource   string               // Resource name, used as a prefix for container IDs.
	output     string               // Output directory for the final build artifact.
	context    string               // Directory containing the manifest, root for resolving copy sources.
	entrypoint []string             // OCI entrypoint to set on the output image (services only).
	platforms  []string             // Target platforms to build for.
	containers []*runtime.Container // All stage containers across all platforms, destroyed after the build completes.
}

// Creates a new [recipe] from the given options.
func newRecipe(rt *runtime.Runtime, opts Options) *recipe {
	return &recipe{
		rt:         rt,
		resource:   opts.Resource,
		output:     opts.Output,
		context:    opts.Root,
		entrypoint: opts.Entrypoint,
		platforms:  opts.Platforms,
	}
}

// Builds the recipe end-to-end against the container runtime.
//
// Each target platform is built independently. Stages are built in declaration
// order for each platform. The non-transient stage is exported as the final
// image to the platform's output directory. All stage containers are destroyed
// when the build completes.
func (r *recipe) build(ctx context.Context, recipeStages []manifest.Stage) (*Result, error) {
	defer r.destroyContainers(ctx)

	for _, platform := range r.platforms {
		if err := r.buildPlatform(ctx, recipeStages, platform); err != nil {
			return nil, err
		}
	}

	return &Result{Output: r.output}, nil
}

// Builds all stages of the recipe for a single platform.
//
// Each platform maintains its own set of named stage containers for
// cross-stage copy lookups. The output is written to a platform-specific
// subdirectory when building for multiple platforms.
func (r *recipe) buildPlatform(ctx context.Context, recipeStages []manifest.Stage, platform string) error {
	slog.Info("building platform", "platform", platform)

	output := r.platformOutput(platform)
	if err := os.MkdirAll(output, paths.DefaultDirMode); err != nil {
		return crex.Wrap(ErrFileSystemOperation, err)
	}

	stages := make(map[string]*runtime.Container)

	for i, stage := range recipeStages {
		if err := r.buildStage(ctx, stage, i, platform, output, stages); err != nil {
			return crex.Wrapf(ErrBuild, "platform %s, stage %s: %w", platform, stageLabel(stage.Name, i), err)
		}
	}

	return nil
}

// Builds a single stage of a recipe for a specific platform.
//
// Resolves the stage's base image, starts a build container, executes the
// stage's steps, then commits the result. Non-transient stages are exported
// to the output directory.
func (r *recipe) buildStage(ctx context.Context, stage manifest.Stage, index int, platform, output string, stages map[string]*runtime.Container) error {
	label := stageLabel(stage.Name, index)
	slog.Info(fmt.Sprintf("building stage %s", label), "platform", platform)

	src, err := stage.ParseFrom()
	if err != nil {
		return err
	}

	id := r.containerID(stage.Name, index, platform)
	ctr, err := r.rt.StartContainer(ctx, src.Value, id, platform)
	if err != nil {
		return crex.Wrap(runtime.ErrRuntime, err)
	}

	r.containers = append(r.containers, ctr)
	if stage.Name != "" {
		stages[stage.Name] = ctr
	}

	if err := executeSteps(ctx, ctr, stage.Steps, newStepState(), r.context, stages); err != nil {
		return err
	}

	if !stage.Transient {
		if err := ctr.Stop(ctx); err != nil {
			return crex.Wrap(runtime.ErrRuntime, err)
		}

		if err := ctr.Export(ctx, output, r.entrypoint); err != nil {
			return crex.Wrap(runtime.ErrRuntime, err)
		}
	}

	return nil
}

// Destroys all stage containers.
func (r *recipe) destroyContainers(ctx context.Context) {
	for _, ctr := range r.containers {
		ctr.Destroy(ctx)
	}
}

// Returns a unique container ID for a stage, scoped to this resource and platform.
func (r *recipe) containerID(name string, index int, platform string) string {
	slug := platformSlug(platform)
	if name != "" {
		return fmt.Sprintf("%s-%s-stage-%s", r.resource, slug, name)
	}
	return fmt.Sprintf("%s-%s-stage-%d", r.resource, slug, index+1)
}

// Returns the output directory for a specific platform.
//
// When building for a single platform, the output directory is left as-is
// to preserve the existing {output}/image.tar convention. For multi-platform
// builds, each platform gets a subdirectory (e.g., {output}/linux-amd64).
func (r *recipe) platformOutput(platform string) string {
	if len(r.platforms) == 1 {
		return r.output
	}
	return filepath.Join(r.output, platformSlug(platform))
}

// Converts a platform string to a filesystem-safe slug.
//
// Replaces slashes with dashes (e.g., "linux/amd64" becomes "linux-amd64").
func platformSlug(platform string) string {
	return strings.ReplaceAll(platform, "/", "-")
}

// Returns a label for a stage, preferring the name when available and falling
// back to the 1-based index.
func stageLabel(name string, index int) string {
	if name != "" {
		return fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("%d", index+1)
}
