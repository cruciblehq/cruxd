package build

import (
	"testing"

	"github.com/cruciblehq/spec/manifest"
)

func TestNewStepState(t *testing.T) {
	s := newStepState()
	if s.shell != defaultShell {
		t.Fatalf("shell = %q, want %q", s.shell, defaultShell)
	}
	if s.workdir != "" {
		t.Fatalf("workdir = %q, want empty", s.workdir)
	}
	if len(s.env) != 0 {
		t.Fatalf("env = %v, want empty", s.env)
	}
}

func TestApply(t *testing.T) {
	s := newStepState()

	s.apply(manifest.Step{Shell: "/bin/bash"})
	if s.shell != "/bin/bash" {
		t.Fatalf("shell = %q, want /bin/bash", s.shell)
	}

	s.apply(manifest.Step{Workdir: "/app"})
	if s.workdir != "/app" {
		t.Fatalf("workdir = %q, want /app", s.workdir)
	}
	if s.shell != "/bin/bash" {
		t.Fatalf("shell changed to %q after workdir apply", s.shell)
	}

	s.apply(manifest.Step{Env: map[string]string{"A": "1", "B": "2"}})
	if s.env["A"] != "1" || s.env["B"] != "2" {
		t.Fatalf("env = %v, want A=1 B=2", s.env)
	}

	s.apply(manifest.Step{Env: map[string]string{"A": "override"}})
	if s.env["A"] != "override" {
		t.Fatalf("env[A] = %q, want override", s.env["A"])
	}
	if s.env["B"] != "2" {
		t.Fatalf("env[B] = %q, want 2 (preserved)", s.env["B"])
	}
}

func TestApplyEmptyFieldsNoOp(t *testing.T) {
	s := newStepState()
	s.apply(manifest.Step{Shell: "/bin/zsh", Workdir: "/opt"})
	s.apply(manifest.Step{})
	if s.shell != "/bin/zsh" {
		t.Fatalf("shell = %q, want /bin/zsh", s.shell)
	}
	if s.workdir != "/opt" {
		t.Fatalf("workdir = %q, want /opt", s.workdir)
	}
}

func TestResolve(t *testing.T) {
	s := newStepState()
	s.apply(manifest.Step{
		Shell:   "/bin/bash",
		Workdir: "/app",
		Env:     map[string]string{"A": "1"},
	})

	resolved := s.resolve(manifest.Step{
		Shell:   "/bin/zsh",
		Workdir: "/tmp",
		Env:     map[string]string{"B": "2"},
	})

	if resolved.shell != "/bin/zsh" {
		t.Fatalf("resolved.shell = %q, want /bin/zsh", resolved.shell)
	}
	if resolved.workdir != "/tmp" {
		t.Fatalf("resolved.workdir = %q, want /tmp", resolved.workdir)
	}
	if resolved.env["A"] != "1" || resolved.env["B"] != "2" {
		t.Fatalf("resolved.env = %v, want A=1 B=2", resolved.env)
	}

	// Original state is unchanged.
	if s.shell != "/bin/bash" {
		t.Fatalf("original shell mutated to %q", s.shell)
	}
	if s.workdir != "/app" {
		t.Fatalf("original workdir mutated to %q", s.workdir)
	}
	if _, ok := s.env["B"]; ok {
		t.Fatal("original env mutated: B leaked in")
	}
}

func TestResolveInheritsState(t *testing.T) {
	s := newStepState()
	s.apply(manifest.Step{Shell: "/bin/bash", Workdir: "/app"})

	resolved := s.resolve(manifest.Step{})
	if resolved.shell != "/bin/bash" {
		t.Fatalf("shell = %q, want /bin/bash", resolved.shell)
	}
	if resolved.workdir != "/app" {
		t.Fatalf("workdir = %q, want /app", resolved.workdir)
	}
}

func TestResolveEnvOverride(t *testing.T) {
	s := newStepState()
	s.apply(manifest.Step{Env: map[string]string{"K": "base"}})

	resolved := s.resolve(manifest.Step{Env: map[string]string{"K": "override"}})
	if resolved.env["K"] != "override" {
		t.Fatalf("env[K] = %q, want override", resolved.env["K"])
	}
	if s.env["K"] != "base" {
		t.Fatalf("original env[K] mutated to %q", s.env["K"])
	}
}

func TestEnviron(t *testing.T) {
	s := newStepState()
	if len(s.environ()) != 0 {
		t.Fatal("empty state should produce no environ entries")
	}

	s.apply(manifest.Step{Env: map[string]string{"PATH": "/usr/bin", "HOME": "/root"}})
	env := s.environ()
	if len(env) != 2 {
		t.Fatalf("len(environ) = %d, want 2", len(env))
	}

	m := make(map[string]bool)
	for _, e := range env {
		m[e] = true
	}
	if !m["PATH=/usr/bin"] || !m["HOME=/root"] {
		t.Fatalf("environ = %v, want PATH=/usr/bin and HOME=/root", env)
	}
}
