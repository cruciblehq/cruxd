// Package build orchestrates recipe execution against container runtimes.
//
// A recipe is an ordered sequence of stages, each backed by a container
// created from a base image. The build pipeline starts a container for
// each stage, dispatches its steps (shell commands, file copies, and
// inter-stage transfers), and exports the final non-transient stage as
// an OCI image. Multi-platform builds repeat the pipeline per platform,
// writing each result to a platform-specific output directory.
//
// Container operations are delegated to the runtime package. Step state
// (environment variables, working directory, shell) is accumulated across
// steps within a stage and reset between stages.
//
// Example usage:
//
//	result, err := build.Run(ctx, rt, build.Options{
//	    Recipe:    recipe,
//	    Resource:  "my-service",
//	    Output:    "dist",
//	    Root:      ".",
//	    Platforms: []string{"linux/amd64", "linux/arm64"},
//	})
//	if err != nil {
//	    return err
//	}
package build
