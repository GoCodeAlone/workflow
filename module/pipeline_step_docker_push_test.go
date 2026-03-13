package module

import (
	"strings"
	"testing"
)

func TestDockerPushStep_MissingImage(t *testing.T) {
	_, err := NewDockerPushStepFactory()("push", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "'image' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDockerPushStep_ValidConfig(t *testing.T) {
	step, err := NewDockerPushStepFactory()("push", map[string]any{
		"image":    "myapp:latest",
		"registry": "registry.example.com",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "push" {
		t.Errorf("expected name 'push', got %q", step.Name())
	}
}

func TestDockerPushStep_MinimalConfig(t *testing.T) {
	_, err := NewDockerPushStepFactory()("push", map[string]any{
		"image": "alpine:latest",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
