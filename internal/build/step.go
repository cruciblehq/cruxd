package build

import (
	"context"
	"log/slog"

	"github.com/cruciblehq/crex"
	"github.com/cruciblehq/cruxd/internal/runtime"
	"github.com/cruciblehq/spec/manifest"
)

// Executes a list of steps in order against the build container.
func executeSteps(ctx context.Context, ctr *runtime.Container, steps []manifest.Step, state *stepState, buildCtx string, stages map[string]*runtime.Container) error {
	for i, step := range steps {
		if err := executeStep(ctx, ctr, step, state, buildCtx, stages); err != nil {
			return crex.Wrapf(ErrBuild, "step %d: %w", i+1, err)
		}
	}
	return nil
}

// Executes a single step, dispatching to operation execution, group recursion,
// or state mutation depending on the step's fields.
func executeStep(ctx context.Context, ctr *runtime.Container, step manifest.Step, state *stepState, buildCtx string, stages map[string]*runtime.Container) error {
	hasOp := step.Run != "" || step.Copy != ""

	// Platform group: apply group-level modifiers and recurse.
	if len(step.Steps) > 0 {
		state.apply(step)
		return executeSteps(ctx, ctr, step.Steps, state, buildCtx, stages)
	}

	// Operation with optional scoped modifiers.
	if hasOp {
		return executeOperation(ctx, ctr, step, state, buildCtx, stages)
	}

	// Standalone modifier(s): persist in state.
	state.apply(step)
	return nil
}

// Executes a run or copy operation with scoped modifier overrides.
//
// Step-level modifiers override the persistent state for this operation only.
// The persistent state is not modified.
func executeOperation(ctx context.Context, ctr *runtime.Container, step manifest.Step, state *stepState, buildCtx string, stages map[string]*runtime.Container) error {
	resolved := state.resolve(step)

	if resolved.workdir != "" {
		if err := ctr.MkdirAll(ctx, resolved.workdir); err != nil {
			return err
		}
	}

	switch {
	case step.Run != "":
		slog.Debug("run", "command", step.Run, "shell", resolved.shell)
		result, err := ctr.Exec(ctx, resolved.shell, step.Run, resolved.environ(), resolved.workdir)
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return crex.Wrapf(ErrCommandFailed, "exit code %d: %s", result.ExitCode, result.Stderr)
		}

	case step.Copy != "":
		if err := executeCopy(ctx, ctr, step.Copy, resolved.workdir, buildCtx, stages); err != nil {
			return err
		}
	}

	return nil
}
