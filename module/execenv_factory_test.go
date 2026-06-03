package module

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/sandbox"
)

// TestExecEnvFactory_DefaultLocalDocker verifies that an empty execEnv or
// "local-docker" both resolve to a local Docker runner (non-nil SandboxRunner).
func TestExecEnvFactory_DefaultLocalDocker(t *testing.T) {
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}

	for _, execEnv := range []string{"", "local-docker"} {
		runner, err := resolveSandboxRunner(nil, execEnv, cfg)
		if err != nil {
			t.Errorf("execEnv=%q: unexpected error: %v", execEnv, err)
			continue
		}
		if runner == nil {
			t.Errorf("execEnv=%q: expected non-nil runner", execEnv)
			continue
		}
		_ = runner.Close()
	}
}

// TestExecEnvFactory_UnknownExecEnv_Error verifies that unknown or deferred
// exec_env values return a clear error rather than silently falling through.
func TestExecEnvFactory_UnknownExecEnv_Error(t *testing.T) {
	cfg := sandbox.SandboxConfig{Image: "alpine:3.19"}

	tests := []struct {
		execEnv     string
		errContains string
	}{
		{"remote", "not yet configured"},
		{"ephemeral", "not yet configured"},
		{"nope", "not configured"},
		{"argo", "not configured"},
	}

	for _, tt := range tests {
		runner, err := resolveSandboxRunner(nil, tt.execEnv, cfg)
		if err == nil {
			t.Errorf("execEnv=%q: expected error, got nil runner=%v", tt.execEnv, runner)
			if runner != nil {
				_ = runner.Close()
			}
			continue
		}
		if !strings.Contains(err.Error(), tt.errContains) {
			t.Errorf("execEnv=%q: expected error to contain %q, got: %v", tt.execEnv, tt.errContains, err)
		}
	}
}

// TestSandboxExec_ExecEnvAbsent_Unchanged verifies that a SandboxExecStep
// constructed without exec_env still uses the factory path and produces a
// local runner (identical behaviour to before this PR).
func TestSandboxExec_ExecEnvAbsent_Unchanged(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("check-exec-env", map[string]any{
		"command": []any{"echo", "hello"},
		// exec_env intentionally absent
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)

	// execEnv should be the zero value — the factory path will default to local-docker.
	if s.execEnv != "" {
		t.Errorf("expected empty execEnv for absent config key, got %q", s.execEnv)
	}

	// Confirm the factory resolves it to a local runner without error.
	cfg := s.buildSandboxConfig()
	runner, err := resolveSandboxRunner(s.app, s.execEnv, cfg)
	if err != nil {
		t.Fatalf("resolveSandboxRunner with empty execEnv: unexpected error: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner for empty execEnv")
	}
	_ = runner.Close()
}

// TestSandboxExec_ExecEnvLocalDocker_ExplicitlySet verifies that setting
// exec_env: local-docker explicitly is accepted and behaves identically.
func TestSandboxExec_ExecEnvLocalDocker_ExplicitlySet(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("explicit-local", map[string]any{
		"command":  []any{"echo", "hello"},
		"exec_env": "local-docker",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.execEnv != "local-docker" {
		t.Errorf("expected execEnv=local-docker, got %q", s.execEnv)
	}

	cfg := s.buildSandboxConfig()
	runner, err := resolveSandboxRunner(s.app, s.execEnv, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	_ = runner.Close()
}

// TestSandboxExec_Factory_InvalidExecEnv verifies the step factory rejects an
// unsupported exec_env at construction time (fail-early), not at Execute time.
func TestSandboxExec_Factory_InvalidExecEnv(t *testing.T) {
	app := NewMockApplication()
	factory := NewSandboxExecStepFactory()
	for _, ee := range []string{"remote", "ephemeral", "nope"} {
		if _, err := factory("sb", map[string]any{"image": "alpine", "exec_env": ee}, app); err == nil {
			t.Errorf("exec_env %q: expected factory error, got nil", ee)
		}
	}
	// local-docker + absent must still succeed.
	if _, err := factory("sb", map[string]any{"image": "alpine", "exec_env": "local-docker"}, app); err != nil {
		t.Errorf("exec_env local-docker: unexpected factory error: %v", err)
	}
}
