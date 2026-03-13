package module

import (
	"strings"
	"testing"
)

func TestDockerBuildStep_MissingContext(t *testing.T) {
	_, err := NewDockerBuildStepFactory()("build", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing context")
	}
	if !strings.Contains(err.Error(), "'context' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDockerBuildStep_ValidConfig(t *testing.T) {
	step, err := NewDockerBuildStepFactory()("build", map[string]any{
		"context":    ".",
		"dockerfile": "Dockerfile",
		"tags":       []any{"myapp:latest", "myapp:v1"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "build" {
		t.Errorf("expected name 'build', got %q", step.Name())
	}
}

func TestDockerBuildStep_DefaultDockerfile(t *testing.T) {
	step, err := NewDockerBuildStepFactory()("build", map[string]any{
		"context": ".",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.(*DockerBuildStep).dockerfile != "Dockerfile" {
		t.Errorf("expected default dockerfile 'Dockerfile', got %q", step.(*DockerBuildStep).dockerfile)
	}
}

func TestDockerBuildStep_NonStringTag(t *testing.T) {
	_, err := NewDockerBuildStepFactory()("build", map[string]any{
		"context": ".",
		"tags":    []any{42},
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-string tag")
	}
}

func TestDockerBuildStep_BuildArgs(t *testing.T) {
	step, err := NewDockerBuildStepFactory()("build", map[string]any{
		"context": ".",
		"build_args": map[string]any{
			"VERSION": "1.0",
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	args := step.(*DockerBuildStep).buildArgs
	if v, ok := args["VERSION"]; !ok || *v != "1.0" {
		t.Errorf("expected build_arg VERSION=1.0, got %v", args)
	}
}
