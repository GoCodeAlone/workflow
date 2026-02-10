package ai

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// TestService_MultipleProviders registers two mock generators with different
// provider names and verifies both appear in Providers().
func TestService_MultipleProviders(t *testing.T) {
	svc := NewService()

	mockAnthropic := &MockGenerator{}
	mockCopilot := &MockGenerator{}

	svc.RegisterGenerator(ProviderAnthropic, mockAnthropic)
	svc.RegisterGenerator(ProviderCopilot, mockCopilot)

	providers := svc.Providers()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}

	found := make(map[Provider]bool)
	for _, p := range providers {
		found[p] = true
	}
	if !found[ProviderAnthropic] {
		t.Error("expected anthropic provider to be registered")
	}
	if !found[ProviderCopilot] {
		t.Error("expected copilot provider to be registered")
	}

	// Auto-select should prefer anthropic when both are registered
	svc.SetPreferred(ProviderAuto)
	resp, err := svc.GenerateWorkflow(context.Background(), GenerateRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error with auto-select: %v", err)
	}
	if resp == nil || resp.Workflow == nil {
		t.Fatal("expected valid response from auto-selected provider")
	}

	// Explicit preference should work
	svc.SetPreferred(ProviderCopilot)
	resp, err = svc.GenerateWorkflow(context.Background(), GenerateRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error with explicit copilot: %v", err)
	}
	if resp == nil || resp.Workflow == nil {
		t.Fatal("expected valid response from copilot provider")
	}
}

// TestService_GenerateWorkflow_MockProvider registers a mock generator,
// calls GenerateWorkflow through the service, and verifies the response.
func TestService_GenerateWorkflow_MockProvider(t *testing.T) {
	svc := NewService()

	expectedModules := []config.ModuleConfig{
		{Name: "server", Type: "http.server", Config: map[string]interface{}{"address": ":8080"}},
		{Name: "router", Type: "http.router", DependsOn: []string{"server"}},
		{Name: "handler", Type: "http.handler", DependsOn: []string{"router"}},
	}

	mock := &MockGenerator{
		GenerateWorkflowFn: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
			return &GenerateResponse{
				Workflow: &config.WorkflowConfig{
					Modules: expectedModules,
				},
				Explanation: "Generated for: " + req.Intent,
			}, nil
		},
	}

	svc.RegisterGenerator(ProviderAnthropic, mock)

	resp, err := svc.GenerateWorkflow(context.Background(), GenerateRequest{
		Intent:      "Create an HTTP API",
		Constraints: []string{"use port 8080"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Workflow == nil {
		t.Fatal("expected non-nil workflow")
	}
	if len(resp.Workflow.Modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(resp.Workflow.Modules))
	}
	if resp.Workflow.Modules[0].Name != "server" {
		t.Errorf("expected first module 'server', got %q", resp.Workflow.Modules[0].Name)
	}
	if resp.Explanation != "Generated for: Create an HTTP API" {
		t.Errorf("unexpected explanation: %s", resp.Explanation)
	}
}

// TestService_GenerateComponent_MockProvider registers a mock generator,
// calls GenerateComponent through the service, and verifies the output.
func TestService_GenerateComponent_MockProvider(t *testing.T) {
	svc := NewService()

	mock := &MockGenerator{
		GenerateComponentFn: func(ctx context.Context, spec ComponentSpec) (string, error) {
			return `package component

import "fmt"

func Name() string { return "` + spec.Name + `" }
func New() interface{} { return &Component{} }

type Component struct{}
func (c *Component) Init() error { fmt.Println("init"); return nil }
`, nil
		},
	}

	svc.RegisterGenerator(ProviderAnthropic, mock)

	code, err := svc.GenerateComponent(context.Background(), ComponentSpec{
		Name:        "my-handler",
		Type:        "custom.handler",
		Description: "A custom handler component",
		Interface:   "modular.Module",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code == "" {
		t.Fatal("expected non-empty generated code")
	}
	if !contains(code, "my-handler") {
		t.Errorf("expected code to contain component name 'my-handler'")
	}
	if !contains(code, "func Name()") {
		t.Error("expected code to contain Name() function")
	}
	if !contains(code, "func New()") {
		t.Error("expected code to contain New() function")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
