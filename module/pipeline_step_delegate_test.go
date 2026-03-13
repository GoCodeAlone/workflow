package module

import (
	"strings"
	"testing"
)

func TestDelegateStep_MissingService(t *testing.T) {
	_, err := NewDelegateStepFactory()("delegate", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing service")
	}
	if !strings.Contains(err.Error(), "'service' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDelegateStep_ValidConfig(t *testing.T) {
	step, err := NewDelegateStepFactory()("delegate", map[string]any{
		"service": "my-handler",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "delegate" {
		t.Errorf("expected name 'delegate', got %q", step.Name())
	}
}

func TestDelegateStep_WithApp(t *testing.T) {
	app := NewMockApplication()
	step, err := NewDelegateStepFactory()("fwd", map[string]any{
		"service": "my-handler",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "fwd" {
		t.Errorf("expected name 'fwd', got %q", step.Name())
	}
}
