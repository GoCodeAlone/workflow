package module

import (
	"strings"
	"testing"
)

func TestDockerRunStep_MissingImage(t *testing.T) {
	_, err := NewDockerRunStepFactory()("run", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "'image' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDockerRunStep_ValidConfig(t *testing.T) {
	step, err := NewDockerRunStepFactory()("run", map[string]any{
		"image":   "alpine:latest",
		"command": []any{"echo", "hello"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "run" {
		t.Errorf("expected name 'run', got %q", step.Name())
	}
}

func TestDockerRunStep_NonStringCommand(t *testing.T) {
	_, err := NewDockerRunStepFactory()("run", map[string]any{
		"image":   "alpine:latest",
		"command": []any{42},
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-string command element")
	}
}

func TestDockerRunStep_InvalidTimeout(t *testing.T) {
	_, err := NewDockerRunStepFactory()("run", map[string]any{
		"image":   "alpine:latest",
		"timeout": "not-valid",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}
