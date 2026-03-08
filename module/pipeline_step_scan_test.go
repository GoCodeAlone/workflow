package module

import (
	"context"
	"strings"
	"testing"
)

// newNoProviderApp returns a mock app with no services registered.
func newNoProviderApp() *scanMockApp {
	return &scanMockApp{services: map[string]any{}}
}

func TestScanSASTStep_NoProvider(t *testing.T) {
	factory := NewScanSASTStepFactory()
	step, err := factory("sast-step", map[string]any{"scanner": "semgrep"}, newNoProviderApp())
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected Execute to return an error, got nil")
	}
	if !strings.Contains(execErr.Error(), "no security scanner provider configured") {
		t.Errorf("unexpected error: %v", execErr)
	}
}

func TestScanContainerStep_NoProvider(t *testing.T) {
	factory := NewScanContainerStepFactory()
	step, err := factory("container-step", map[string]any{}, newNoProviderApp())
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected Execute to return an error, got nil")
	}
	if !strings.Contains(execErr.Error(), "no security scanner provider configured") {
		t.Errorf("unexpected error: %v", execErr)
	}
}

func TestScanDepsStep_NoProvider(t *testing.T) {
	factory := NewScanDepsStepFactory()
	step, err := factory("deps-step", map[string]any{}, newNoProviderApp())
	if err != nil {
		t.Fatalf("factory returned error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected Execute to return an error, got nil")
	}
	if !strings.Contains(execErr.Error(), "no security scanner provider configured") {
		t.Errorf("unexpected error: %v", execErr)
	}
}
