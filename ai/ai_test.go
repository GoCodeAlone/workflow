package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestSystemPrompt(t *testing.T) {
	prompt := SystemPrompt()

	if prompt == "" {
		t.Fatal("SystemPrompt returned empty string")
	}

	// Verify it contains key module types
	expectedTypes := []string{
		"http.server", "http.router", "http.handler",
		"messaging.broker", "messaging.handler",
		"statemachine.engine", "state.tracker",
		"event.processor", "scheduler.modular",
	}
	for _, mt := range expectedTypes {
		if !strings.Contains(prompt, mt) {
			t.Errorf("SystemPrompt missing module type: %s", mt)
		}
	}

	// Verify it contains workflow types
	for _, wt := range []string{"HTTP Workflow", "Messaging Workflow", "State Machine Workflow", "Event Workflow"} {
		if !strings.Contains(prompt, wt) {
			t.Errorf("SystemPrompt missing workflow type section: %s", wt)
		}
	}

	// Verify it contains interface definitions
	for _, iface := range []string{"modular.Module", "WorkflowHandler"} {
		if !strings.Contains(prompt, iface) {
			t.Errorf("SystemPrompt missing interface: %s", iface)
		}
	}
}

func TestGeneratePrompt(t *testing.T) {
	req := GenerateRequest{
		Intent: "Create a REST API for user management",
		Context: map[string]string{
			"authentication": "JWT",
		},
		Constraints: []string{
			"Use rate limiting",
			"Include health check endpoint",
		},
	}

	prompt := GeneratePrompt(req)

	if !strings.Contains(prompt, req.Intent) {
		t.Error("prompt missing intent")
	}
	if !strings.Contains(prompt, "JWT") {
		t.Error("prompt missing context value")
	}
	if !strings.Contains(prompt, "rate limiting") {
		t.Error("prompt missing constraint")
	}
	if !strings.Contains(prompt, "WorkflowConfig") {
		t.Error("prompt missing output format instructions")
	}
}

func TestComponentPrompt(t *testing.T) {
	spec := ComponentSpec{
		Name:        "price-checker",
		Type:        "stock.price.checker",
		Description: "Checks stock prices",
		Interface:   "modular.Module",
	}

	prompt := ComponentPrompt(spec)

	if !strings.Contains(prompt, spec.Name) {
		t.Error("prompt missing component name")
	}
	if !strings.Contains(prompt, spec.Type) {
		t.Error("prompt missing component type")
	}
	if !strings.Contains(prompt, spec.Interface) {
		t.Error("prompt missing interface")
	}
}

func TestSuggestPrompt(t *testing.T) {
	useCase := "Monitor website uptime and send alerts"
	prompt := SuggestPrompt(useCase)

	if !strings.Contains(prompt, useCase) {
		t.Error("prompt missing use case")
	}
	if !strings.Contains(prompt, "confidence") {
		t.Error("prompt missing confidence field instruction")
	}
}

func TestMissingComponentsPrompt(t *testing.T) {
	types := []string{"http.server", "stock.price.checker", "trade.executor"}
	prompt := MissingComponentsPrompt(types)

	for _, mt := range types {
		if !strings.Contains(prompt, mt) {
			t.Errorf("prompt missing module type: %s", mt)
		}
	}
	// Verify built-in types are listed
	if !strings.Contains(prompt, "http.server") {
		t.Error("prompt missing built-in type list")
	}
}

func TestLoadExampleConfigs(t *testing.T) {
	// Test with a non-existent directory
	_, err := LoadExampleConfigs("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestExamplePromptSection(t *testing.T) {
	examples := map[string]string{
		"simple.yaml": "modules:\n  - name: test\n    type: http.server",
	}

	section := ExamplePromptSection(examples)
	if !strings.Contains(section, "simple.yaml") {
		t.Error("section missing filename")
	}
	if !strings.Contains(section, "http.server") {
		t.Error("section missing content")
	}

	// Empty examples
	empty := ExamplePromptSection(nil)
	if empty != "" {
		t.Error("expected empty string for nil examples")
	}
}

// MockGenerator implements WorkflowGenerator for testing.
type MockGenerator struct {
	GenerateWorkflowFn          func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	GenerateComponentFn         func(ctx context.Context, spec ComponentSpec) (string, error)
	SuggestWorkflowFn           func(ctx context.Context, useCase string) ([]WorkflowSuggestion, error)
	IdentifyMissingComponentsFn func(ctx context.Context, cfg *config.WorkflowConfig) ([]ComponentSpec, error)
}

func (m *MockGenerator) GenerateWorkflow(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	if m.GenerateWorkflowFn != nil {
		return m.GenerateWorkflowFn(ctx, req)
	}
	return &GenerateResponse{
		Workflow: &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "test-server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
			},
			Workflows: map[string]any{},
		},
		Explanation: "test workflow",
	}, nil
}

func (m *MockGenerator) GenerateComponent(ctx context.Context, spec ComponentSpec) (string, error) {
	if m.GenerateComponentFn != nil {
		return m.GenerateComponentFn(ctx, spec)
	}
	return "package module\n\n// Generated component\ntype TestComponent struct{}", nil
}

func (m *MockGenerator) SuggestWorkflow(ctx context.Context, useCase string) ([]WorkflowSuggestion, error) {
	if m.SuggestWorkflowFn != nil {
		return m.SuggestWorkflowFn(ctx, useCase)
	}
	return []WorkflowSuggestion{
		{
			Name:        "test-workflow",
			Description: "A test workflow",
			Confidence:  0.9,
		},
	}, nil
}

func (m *MockGenerator) IdentifyMissingComponents(ctx context.Context, cfg *config.WorkflowConfig) ([]ComponentSpec, error) {
	if m.IdentifyMissingComponentsFn != nil {
		return m.IdentifyMissingComponentsFn(ctx, cfg)
	}
	return nil, nil
}

func TestServiceGenerateWorkflow(t *testing.T) {
	svc := NewService()
	mock := &MockGenerator{}
	svc.RegisterGenerator(ProviderAnthropic, mock)

	ctx := context.Background()
	req := GenerateRequest{Intent: "test workflow"}

	resp, err := svc.GenerateWorkflow(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Workflow == nil {
		t.Fatal("expected workflow in response")
	}
	if len(resp.Workflow.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(resp.Workflow.Modules))
	}
}

func TestServiceGenerateComponent(t *testing.T) {
	svc := NewService()
	mock := &MockGenerator{}
	svc.RegisterGenerator(ProviderAnthropic, mock)

	ctx := context.Background()
	spec := ComponentSpec{Name: "test", Interface: "modular.Module"}

	code, err := svc.GenerateComponent(ctx, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(code, "TestComponent") {
		t.Error("expected generated component code")
	}
}

func TestServiceSuggestWorkflow(t *testing.T) {
	svc := NewService()
	mock := &MockGenerator{}
	svc.RegisterGenerator(ProviderAnthropic, mock)

	ctx := context.Background()

	suggestions, err := svc.SuggestWorkflow(ctx, "test use case")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	if suggestions[0].Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", suggestions[0].Confidence)
	}

	// Test caching - should return same result without calling mock again
	callCount := 0
	mock.SuggestWorkflowFn = func(ctx context.Context, useCase string) ([]WorkflowSuggestion, error) {
		callCount++
		return []WorkflowSuggestion{{Name: "new", Confidence: 0.5}}, nil
	}

	cached, err := svc.SuggestWorkflow(ctx, "test use case")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Error("expected cached result, but mock was called")
	}
	if cached[0].Confidence != 0.9 {
		t.Error("expected cached confidence 0.9")
	}

	// Clear cache and verify new call
	svc.ClearCache()
	fresh, err := svc.SuggestWorkflow(ctx, "test use case")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Error("expected mock to be called after cache clear")
	}
	if fresh[0].Confidence != 0.5 {
		t.Error("expected fresh confidence 0.5")
	}
}

func TestServiceProviderSelection(t *testing.T) {
	svc := NewService()

	// No generators registered
	_, err := svc.GenerateWorkflow(context.Background(), GenerateRequest{Intent: "test"})
	if err == nil {
		t.Error("expected error with no generators")
	}

	mock := &MockGenerator{}
	svc.RegisterGenerator(ProviderCopilot, mock)

	// Auto-select should pick copilot
	providers := svc.Providers()
	if len(providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(providers))
	}

	// Set explicit preference
	svc.SetPreferred(ProviderAnthropic)
	_, err = svc.GenerateWorkflow(context.Background(), GenerateRequest{Intent: "test"})
	if err == nil {
		t.Error("expected error for missing preferred provider")
	}

	// Register anthropic and it should work
	svc.RegisterGenerator(ProviderAnthropic, mock)
	_, err = svc.GenerateWorkflow(context.Background(), GenerateRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceIdentifyMissingComponents(t *testing.T) {
	svc := NewService()
	mock := &MockGenerator{
		IdentifyMissingComponentsFn: func(ctx context.Context, cfg *config.WorkflowConfig) ([]ComponentSpec, error) {
			var missing []ComponentSpec
			for _, mod := range cfg.Modules {
				if mod.Type == "stock.price.checker" || mod.Type == "trade.executor" {
					missing = append(missing, ComponentSpec{
						Name: mod.Name,
						Type: mod.Type,
					})
				}
			}
			return missing, nil
		},
	}
	svc.RegisterGenerator(ProviderAnthropic, mock)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
			{Name: "checker", Type: "stock.price.checker"},
			{Name: "executor", Type: "trade.executor"},
		},
	}

	missing, err := svc.IdentifyMissingComponents(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 2 {
		t.Errorf("expected 2 missing components, got %d", len(missing))
	}
}
