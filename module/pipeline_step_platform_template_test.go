package module

import (
	"strings"
	"testing"
)

func TestPlatformTemplateStep_MissingTemplateName(t *testing.T) {
	_, err := NewPlatformTemplateStepFactory()("pt", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing template_name")
	}
	if !strings.Contains(err.Error(), "'template_name' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPlatformTemplateStep_ValidConfig(t *testing.T) {
	step, err := NewPlatformTemplateStepFactory()("pt", map[string]any{
		"template_name": "standard-webapp",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "pt" {
		t.Errorf("expected name 'pt', got %q", step.Name())
	}
}

func TestPlatformTemplateStep_WithVersionAndParams(t *testing.T) {
	step, err := NewPlatformTemplateStepFactory()("pt", map[string]any{
		"template_name":    "standard-webapp",
		"template_version": "v1.2",
		"parameters": map[string]any{
			"replicas": 3,
			"image":    "myapp:latest",
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "pt" {
		t.Errorf("expected name 'pt', got %q", step.Name())
	}
}

func TestPlatformTemplateStep_WithApp(t *testing.T) {
	// App without platform.TemplateRegistry — factory still succeeds,
	// Execute would fail (registry nil) but factory returns no error.
	app := NewMockApplication()
	step, err := NewPlatformTemplateStepFactory()("pt", map[string]any{
		"template_name": "standard-webapp",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "pt" {
		t.Errorf("expected name 'pt', got %q", step.Name())
	}
}
