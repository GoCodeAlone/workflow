package module

import (
	"strings"
	"testing"
)

func TestShellExecStep_MissingImage(t *testing.T) {
	factory := NewShellExecStepFactory()
	_, err := factory("no-image", map[string]any{
		"commands": []any{"echo hello"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "'image' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShellExecStep_MissingCommands(t *testing.T) {
	factory := NewShellExecStepFactory()
	_, err := factory("no-cmds", map[string]any{
		"image": "alpine:latest",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing commands")
	}
	if !strings.Contains(err.Error(), "'commands' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShellExecStep_EmptyCommands(t *testing.T) {
	factory := NewShellExecStepFactory()
	_, err := factory("empty-cmds", map[string]any{
		"image":    "alpine:latest",
		"commands": []any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error for empty commands")
	}
}

func TestShellExecStep_ValidConfig(t *testing.T) {
	factory := NewShellExecStepFactory()
	step, err := factory("run-script", map[string]any{
		"image":    "alpine:latest",
		"commands": []any{"echo hello", "ls /tmp"},
		"work_dir": "/workspace",
		"timeout":  "30s",
		"env": map[string]any{
			"FOO": "bar",
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "run-script" {
		t.Errorf("expected name 'run-script', got %q", step.Name())
	}
}

func TestShellExecStep_InvalidTimeout(t *testing.T) {
	factory := NewShellExecStepFactory()
	_, err := factory("bad-timeout", map[string]any{
		"image":    "alpine:latest",
		"commands": []any{"echo hi"},
		"timeout":  "not-a-duration",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShellExecStep_ArtifactsOutMissingKey(t *testing.T) {
	factory := NewShellExecStepFactory()
	_, err := factory("bad-artifact", map[string]any{
		"image":    "alpine:latest",
		"commands": []any{"echo hi"},
		"artifacts_out": []any{
			map[string]any{"path": "/tmp/output.txt"}, // missing key
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing artifact key")
	}
}

func TestShellExecStep_NonStringCommand(t *testing.T) {
	factory := NewShellExecStepFactory()
	_, err := factory("bad-cmd", map[string]any{
		"image":    "alpine:latest",
		"commands": []any{42}, // non-string command
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-string command")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("unexpected error: %v", err)
	}
}
