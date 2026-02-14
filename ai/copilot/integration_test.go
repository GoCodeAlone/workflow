package copilotai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/ai"
	"github.com/GoCodeAlone/workflow/config"
	copilot "github.com/github/copilot-sdk/go"
)

// TestCopilotIntegration_ToolHandlersInvocable invokes each tool handler
// directly with real inputs and verifies the outputs contain expected content.
func TestCopilotIntegration_ToolHandlersInvocable(t *testing.T) {
	tools := workflowTools()
	toolMap := make(map[string]copilot.Tool)
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	t.Run("list_components", func(t *testing.T) {
		tool, ok := toolMap["list_components"]
		if !ok {
			t.Fatal("list_components tool not found")
		}

		result, err := tool.Handler(copilot.ToolInvocation{})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.ResultType != "success" {
			t.Errorf("expected ResultType 'success', got %q", result.ResultType)
		}

		var modules map[string]string
		if err := json.Unmarshal([]byte(result.TextResultForLLM), &modules); err != nil {
			t.Fatalf("result is not valid JSON map: %v", err)
		}

		// Should contain many module types
		expectedTypes := []string{
			"http.server", "http.router", "http.handler",
			"messaging.broker", "messaging.handler",
			"statemachine.engine", "event.processor",
			"scheduler.modular", "cache.modular",
		}
		for _, mt := range expectedTypes {
			if _, exists := modules[mt]; !exists {
				t.Errorf("expected module type %q in list, not found", mt)
			}
		}

		if len(modules) < 10 {
			t.Errorf("expected at least 10 module types, got %d", len(modules))
		}
	})

	t.Run("get_component_schema", func(t *testing.T) {
		tool, ok := toolMap["get_component_schema"]
		if !ok {
			t.Fatal("get_component_schema tool not found")
		}

		result, err := tool.Handler(copilot.ToolInvocation{
			Arguments: map[string]any{"module_type": "http.server"},
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.ResultType != "success" {
			t.Errorf("expected ResultType 'success', got %q", result.ResultType)
		}
		if !strings.Contains(result.TextResultForLLM, "address") {
			t.Errorf("expected schema to contain 'address', got: %s", result.TextResultForLLM)
		}
	})

	t.Run("validate_config_valid", func(t *testing.T) {
		tool, ok := toolMap["validate_config"]
		if !ok {
			t.Fatal("validate_config tool not found")
		}

		validYAML := `modules:
  - name: httpServer
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn:
      - httpServer`

		result, err := tool.Handler(copilot.ToolInvocation{
			Arguments: map[string]any{"config_yaml": validYAML},
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if result.ResultType != "success" {
			t.Errorf("expected ResultType 'success', got %q", result.ResultType)
		}
		if !strings.Contains(result.TextResultForLLM, `"valid"`) {
			t.Errorf("expected result to indicate valid config, got: %s", result.TextResultForLLM)
		}
	})

	t.Run("validate_config_invalid", func(t *testing.T) {
		tool, ok := toolMap["validate_config"]
		if !ok {
			t.Fatal("validate_config tool not found")
		}

		result, err := tool.Handler(copilot.ToolInvocation{
			Arguments: map[string]any{"config_yaml": "modules: []"},
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if !strings.Contains(result.TextResultForLLM, "no modules defined") {
			t.Errorf("expected validation error about no modules, got: %s", result.TextResultForLLM)
		}
	})

	t.Run("get_example_workflow", func(t *testing.T) {
		tool, ok := toolMap["get_example_workflow"]
		if !ok {
			t.Fatal("get_example_workflow tool not found")
		}

		categories := map[string]string{
			"http":         "httpServer",
			"messaging":    "messageBroker",
			"statemachine": "orderEngine",
			"event":        "eventProcessor",
			"trigger":      "workflows",
		}

		for category, expectedContent := range categories {
			t.Run(category, func(t *testing.T) {
				result, err := tool.Handler(copilot.ToolInvocation{
					Arguments: map[string]any{"category": category},
				})
				if err != nil {
					t.Fatalf("handler returned error for category %q: %v", category, err)
				}
				if result.ResultType != "success" {
					t.Errorf("expected ResultType 'success', got %q", result.ResultType)
				}
				if !strings.Contains(result.TextResultForLLM, expectedContent) {
					t.Errorf("expected example for %q to contain %q, got: %s",
						category, expectedContent, result.TextResultForLLM)
				}
			})
		}
	})
}

// TestCopilotIntegration_ClientWithMockWrapper exercises all Client methods
// through a mock wrapper, verifying the full request-response flow.
func TestCopilotIntegration_ClientWithMockWrapper(t *testing.T) {
	t.Run("GenerateWorkflow", func(t *testing.T) {
		resp := ai.GenerateResponse{
			Workflow: &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: "server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
					{Name: "router", Type: "http.router", DependsOn: []string{"server"}},
				},
			},
			Explanation: "HTTP server with router",
		}
		respJSON, _ := json.Marshal(resp)
		wrapper := mockWrapperWithResponse(sessionEventWithContent(string(respJSON)), nil)
		client := newTestClient(wrapper)

		result, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{
			Intent:      "create an HTTP server with routing",
			Constraints: []string{"use port 8080"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Workflow == nil {
			t.Fatal("expected non-nil workflow")
		}
		if len(result.Workflow.Modules) != 2 {
			t.Fatalf("expected 2 modules, got %d", len(result.Workflow.Modules))
		}
		if result.Explanation != "HTTP server with router" {
			t.Errorf("unexpected explanation: %s", result.Explanation)
		}
	})

	t.Run("GenerateComponent", func(t *testing.T) {
		code := `package component

import "fmt"

func Name() string { return "custom-handler" }
func New() interface{} { return &Handler{} }

type Handler struct{}
func (h *Handler) ServeHTTP() { fmt.Println("handling") }`

		text := "```go\n" + code + "\n```"
		wrapper := mockWrapperWithResponse(sessionEventWithContent(text), nil)
		client := newTestClient(wrapper)

		result, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{
			Name:        "custom-handler",
			Type:        "custom.handler",
			Description: "A custom HTTP handler",
			Interface:   "modular.Module",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "func Name()") {
			t.Errorf("expected generated code to contain Name(), got: %s", result)
		}
		if !strings.Contains(result, "custom-handler") {
			t.Errorf("expected generated code to contain component name")
		}
	})

	t.Run("SuggestWorkflow", func(t *testing.T) {
		suggestions := []ai.WorkflowSuggestion{
			{Name: "REST API", Description: "Full REST API", Confidence: 0.95},
			{Name: "WebSocket Server", Description: "Real-time server", Confidence: 0.8},
			{Name: "Message Queue", Description: "Async processing", Confidence: 0.6},
		}
		respJSON, _ := json.Marshal(suggestions)
		wrapper := mockWrapperWithResponse(sessionEventWithContent(string(respJSON)), nil)
		client := newTestClient(wrapper)

		result, err := client.SuggestWorkflow(context.Background(), "build a web application")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 suggestions, got %d", len(result))
		}
		if result[0].Name != "REST API" {
			t.Errorf("expected first suggestion 'REST API', got %q", result[0].Name)
		}
		if result[0].Confidence != 0.95 {
			t.Errorf("expected confidence 0.95, got %f", result[0].Confidence)
		}
	})

	t.Run("IdentifyMissingComponents", func(t *testing.T) {
		specs := []ai.ComponentSpec{
			{Name: "priceChecker", Type: "stock.price.checker", Description: "Checks prices", Interface: "modular.Module"},
			{Name: "tradeExecutor", Type: "trade.executor", Description: "Executes trades", Interface: "modular.Module"},
		}
		specsJSON, _ := json.Marshal(specs)
		wrapper := mockWrapperWithResponse(sessionEventWithContent(string(specsJSON)), nil)
		client := newTestClient(wrapper)

		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "server", Type: "http.server"},
				{Name: "checker", Type: "stock.price.checker"},
				{Name: "executor", Type: "trade.executor"},
			},
		}

		result, err := client.IdentifyMissingComponents(context.Background(), cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 missing components, got %d", len(result))
		}

		names := make(map[string]bool)
		for _, s := range result {
			names[s.Name] = true
		}
		if !names["priceChecker"] {
			t.Error("expected priceChecker in missing components")
		}
		if !names["tradeExecutor"] {
			t.Error("expected tradeExecutor in missing components")
		}
	})
}

// TestCopilotIntegration_ProviderRegistered verifies that a Copilot client
// can be registered as a provider in the AI service and appears in the
// provider list.
func TestCopilotIntegration_ProviderRegistered(t *testing.T) {
	svc := ai.NewService()

	// Create a copilot client with a mock wrapper
	resp := ai.GenerateResponse{
		Workflow: &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "test", Type: "http.server"},
			},
		},
		Explanation: "test",
	}
	respJSON, _ := json.Marshal(resp)
	wrapper := mockWrapperWithResponse(sessionEventWithContent(string(respJSON)), nil)
	client := newTestClient(wrapper)

	// Register the copilot client as a provider
	svc.RegisterGenerator(ai.ProviderCopilot, client)

	providers := svc.Providers()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0] != ai.ProviderCopilot {
		t.Errorf("expected provider %q, got %q", ai.ProviderCopilot, providers[0])
	}

	// Verify we can generate through the service
	result, err := svc.GenerateWorkflow(context.Background(), ai.GenerateRequest{
		Intent: "test workflow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Workflow == nil {
		t.Fatal("expected non-nil workflow from service")
	}
}
