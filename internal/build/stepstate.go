package build

import (
	"maps"

	"github.com/cruciblehq/spec/manifest"
)

// Default shell used for run steps when no shell modifier has been set.
const defaultShell = "/bin/sh"

// Tracks accumulated modifiers during step execution.
//
// State flows linearly through the step list. Standalone modifiers update
// the state permanently via apply. Operations read the effective values for
// a single step via resolve without modifying the persistent state.
type stepState struct {
	shell   string
	workdir string
	env     map[string]string
}

// Creates a new [stepState] with default values.
func newStepState() *stepState {
	return &stepState{
		shell: defaultShell,
		env:   make(map[string]string),
	}
}

// Persists modifier fields from a step into the state.
//
// Called for standalone modifier steps and platform groups. The state is
// mutated permanently, affecting all subsequent steps.
func (s *stepState) apply(step manifest.Step) {
	if step.Shell != "" {
		s.shell = step.Shell
	}
	if step.Workdir != "" {
		s.workdir = step.Workdir
	}
	maps.Copy(s.env, step.Env)
}

// Returns a new [stepState] with step-level modifiers overlaid on the
// persistent state. The receiver is not modified.
//
// Step-level modifiers override the corresponding state values for this
// operation only.
func (s *stepState) resolve(step manifest.Step) *stepState {
	resolved := &stepState{
		shell:   s.shell,
		workdir: s.workdir,
		env:     make(map[string]string, len(s.env)+len(step.Env)),
	}
	maps.Copy(resolved.env, s.env)
	maps.Copy(resolved.env, step.Env)

	if step.Shell != "" {
		resolved.shell = step.Shell
	}
	if step.Workdir != "" {
		resolved.workdir = step.Workdir
	}

	return resolved
}

// Formats the environment as a list of "key=value" strings suitable for
// passing to container exec.
func (s *stepState) environ() []string {
	env := make([]string, 0, len(s.env))
	for k, v := range s.env {
		env = append(env, k+"="+v)
	}
	return env
}
