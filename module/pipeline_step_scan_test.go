package module

import (
	"context"
	"errors"
	"testing"
)

func TestScanSASTStep_ExecuteReturnsErrNotImplemented(t *testing.T) {
	factory := NewScanSASTStepFactory()
	step, err := factory("sast-step", map[string]any{"scanner": "semgrep"}, nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected Execute to return an error, got nil")
	}
	if !errors.Is(execErr, ErrNotImplemented) {
		t.Errorf("expected errors.Is(err, ErrNotImplemented), got: %v", execErr)
	}
}

func TestScanContainerStep_ExecuteReturnsErrNotImplemented(t *testing.T) {
	factory := NewScanContainerStepFactory()
	step, err := factory("container-step", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected Execute to return an error, got nil")
	}
	if !errors.Is(execErr, ErrNotImplemented) {
		t.Errorf("expected errors.Is(err, ErrNotImplemented), got: %v", execErr)
	}
}

func TestScanDepsStep_ExecuteReturnsErrNotImplemented(t *testing.T) {
	factory := NewScanDepsStepFactory()
	step, err := factory("deps-step", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected Execute to return an error, got nil")
	}
	if !errors.Is(execErr, ErrNotImplemented) {
		t.Errorf("expected errors.Is(err, ErrNotImplemented), got: %v", execErr)
	}
}
