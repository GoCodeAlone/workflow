package module

import (
	"strings"
	"testing"
)

func TestFieldReencryptStep_MissingModule(t *testing.T) {
	_, err := NewFieldReencryptStepFactory()("reencrypt", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
	if !strings.Contains(err.Error(), "'module' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFieldReencryptStep_ValidConfig(t *testing.T) {
	step, err := NewFieldReencryptStepFactory()("reencrypt", map[string]any{
		"module": "field-protection",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "reencrypt" {
		t.Errorf("expected name 'reencrypt', got %q", step.Name())
	}
}

func TestFieldReencryptStep_WithTenantID(t *testing.T) {
	step, err := NewFieldReencryptStepFactory()("reencrypt", map[string]any{
		"module":    "field-protection",
		"tenant_id": "{{ .body.tenant_id }}",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "reencrypt" {
		t.Errorf("expected name 'reencrypt', got %q", step.Name())
	}
}
