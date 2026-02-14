package copilotai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/ai"
	"github.com/GoCodeAlone/workflow/config"
	copilot "github.com/github/copilot-sdk/go"
)

// --- Mock types ---

type mockSession struct {
	sendAndWaitFn func(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error)
	destroyed     bool
}

func (m *mockSession) SendAndWait(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error) {
	return m.sendAndWaitFn(ctx, opts)
}

func (m *mockSession) Destroy() error {
	m.destroyed = true
	return nil
}

type mockClientWrapper struct {
	createSessionFn func(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error)
}

func (m *mockClientWrapper) CreateSession(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error) {
	return m.createSessionFn(ctx, cfg)
}

// Helper to build a Client with a mock wrapper.
func newTestClient(wrapper ClientWrapper) *Client {
	return &Client{
		cfg:     ClientConfig{Model: "test-model"},
		wrapper: wrapper,
	}
}

// Helper to create a SessionEvent with the given text content.
func sessionEventWithContent(text string) *copilot.SessionEvent {
	return &copilot.SessionEvent{
		Data: copilot.Data{Content: &text},
	}
}

// Helper to create a mock wrapper that returns a session with the given response.
func mockWrapperWithResponse(resp *copilot.SessionEvent, err error) *mockClientWrapper {
	sess := &mockSession{
		sendAndWaitFn: func(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error) {
			return resp, err
		},
	}
	return &mockClientWrapper{
		createSessionFn: func(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error) {
			return sess, nil
		},
	}
}

// Helper to create a mock wrapper that fails on session creation.
func mockWrapperSessionError(sessionErr error) *mockClientWrapper {
	return &mockClientWrapper{
		createSessionFn: func(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error) {
			return nil, sessionErr
		},
	}
}

// --- NewClient tests ---

func TestNewClient_Defaults(t *testing.T) {
	client, err := NewClient(ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.cfg.CLIPath != "" {
		t.Errorf("expected empty CLIPath in config, got '%s'", client.cfg.CLIPath)
	}
	if client.wrapper == nil {
		t.Error("expected non-nil wrapper")
	}
}

func TestNewClient_CustomCLIPath(t *testing.T) {
	client, err := NewClient(ClientConfig{CLIPath: "/usr/local/bin/copilot"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.cfg.CLIPath != "/usr/local/bin/copilot" {
		t.Errorf("expected CLIPath '/usr/local/bin/copilot', got '%s'", client.cfg.CLIPath)
	}
}

func TestNewClient_WithModel(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.cfg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got '%s'", client.cfg.Model)
	}
}

func TestWorkflowTools(t *testing.T) {
	tools := workflowTools()
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	expectedTools := map[string]bool{
		"list_components":      false,
		"get_component_schema": false,
		"validate_config":      false,
		"get_example_workflow": false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
		if tool.Description == "" {
			t.Errorf("tool '%s' has empty description", tool.Name)
		}
		if tool.Handler == nil {
			t.Errorf("tool '%s' has nil handler", tool.Name)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool '%s' not found", name)
		}
	}
}

// --- GenerateWorkflow tests ---

func TestGenerateWorkflow_Success(t *testing.T) {
	resp := ai.GenerateResponse{
		Workflow: &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "httpServer", Type: "http.server"},
			},
		},
		Explanation: "A simple HTTP server workflow.",
	}
	respJSON, _ := json.Marshal(resp)
	wrapper := mockWrapperWithResponse(sessionEventWithContent(string(respJSON)), nil)
	client := newTestClient(wrapper)

	result, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{
		Intent: "create an HTTP server",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Workflow == nil {
		t.Fatal("expected non-nil workflow")
	}
	if len(result.Workflow.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(result.Workflow.Modules))
	}
	if result.Workflow.Modules[0].Name != "httpServer" {
		t.Errorf("expected module name 'httpServer', got '%s'", result.Workflow.Modules[0].Name)
	}
}

func TestGenerateWorkflow_SessionError(t *testing.T) {
	wrapper := mockWrapperSessionError(fmt.Errorf("connection refused"))
	client := newTestClient(wrapper)

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create Copilot session") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateWorkflow_SendError(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, fmt.Errorf("timeout"))
	client := newTestClient(wrapper)

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "copilot request failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateWorkflow_EmptyResponse(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, nil)
	client := newTestClient(wrapper)

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateWorkflow_NilContent(t *testing.T) {
	event := &copilot.SessionEvent{
		Data: copilot.Data{Content: nil},
	}
	wrapper := mockWrapperWithResponse(event, nil)
	client := newTestClient(wrapper)

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateWorkflow_NoJSON(t *testing.T) {
	wrapper := mockWrapperWithResponse(sessionEventWithContent("Here is some plain text with no JSON."), nil)
	client := newTestClient(wrapper)

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no JSON found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateWorkflow_MalformedJSON(t *testing.T) {
	wrapper := mockWrapperWithResponse(sessionEventWithContent(`{"workflow": broken}`), nil)
	client := newTestClient(wrapper)

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateWorkflow_JSONInCodeBlock(t *testing.T) {
	resp := ai.GenerateResponse{
		Workflow: &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "broker", Type: "messaging.broker"},
			},
		},
		Explanation: "A messaging workflow.",
	}
	respJSON, _ := json.Marshal(resp)
	text := "Here is the workflow:\n```json\n" + string(respJSON) + "\n```"
	wrapper := mockWrapperWithResponse(sessionEventWithContent(text), nil)
	client := newTestClient(wrapper)

	result, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "messaging"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Workflow == nil || len(result.Workflow.Modules) != 1 {
		t.Fatal("expected 1 module in workflow")
	}
}

// --- GenerateComponent tests ---

func TestGenerateComponent_Success(t *testing.T) {
	code := "package main\n\nfunc Name() string { return \"test\" }"
	text := "Here is the code:\n```go\n" + code + "\n```"
	wrapper := mockWrapperWithResponse(sessionEventWithContent(text), nil)
	client := newTestClient(wrapper)

	result, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{
		Name:      "test",
		Type:      "custom.handler",
		Interface: "modular.Module",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "func Name()") {
		t.Errorf("expected Go code in result, got: %s", result)
	}
}

func TestGenerateComponent_SessionError(t *testing.T) {
	wrapper := mockWrapperSessionError(fmt.Errorf("auth failed"))
	client := newTestClient(wrapper)

	_, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{Name: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create Copilot session") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateComponent_SendError(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, fmt.Errorf("network error"))
	client := newTestClient(wrapper)

	_, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{Name: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "copilot request failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateComponent_EmptyResponse(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, nil)
	client := newTestClient(wrapper)

	_, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{Name: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateComponent_PlainText(t *testing.T) {
	// When response has no code block, ExtractCode returns trimmed text.
	wrapper := mockWrapperWithResponse(sessionEventWithContent("package main"), nil)
	client := newTestClient(wrapper)

	result, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{Name: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "package main" {
		t.Errorf("expected 'package main', got '%s'", result)
	}
}

// --- SuggestWorkflow tests ---

func TestSuggestWorkflow_Success(t *testing.T) {
	suggestions := []ai.WorkflowSuggestion{
		{
			Name:        "API Gateway",
			Description: "An API gateway workflow",
			Confidence:  0.9,
		},
		{
			Name:        "Message Processor",
			Description: "A message processing workflow",
			Confidence:  0.7,
		},
	}
	respJSON, _ := json.Marshal(suggestions)
	wrapper := mockWrapperWithResponse(sessionEventWithContent(string(respJSON)), nil)
	client := newTestClient(wrapper)

	result, err := client.SuggestWorkflow(context.Background(), "I need an API")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(result))
	}
	if result[0].Name != "API Gateway" {
		t.Errorf("expected first suggestion 'API Gateway', got '%s'", result[0].Name)
	}
	if result[0].Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", result[0].Confidence)
	}
}

func TestSuggestWorkflow_SessionError(t *testing.T) {
	wrapper := mockWrapperSessionError(fmt.Errorf("session error"))
	client := newTestClient(wrapper)

	_, err := client.SuggestWorkflow(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create Copilot session") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSuggestWorkflow_EmptyResponse(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, nil)
	client := newTestClient(wrapper)

	_, err := client.SuggestWorkflow(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSuggestWorkflow_NoJSON(t *testing.T) {
	wrapper := mockWrapperWithResponse(sessionEventWithContent("No suggestions available."), nil)
	client := newTestClient(wrapper)

	_, err := client.SuggestWorkflow(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no JSON found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSuggestWorkflow_SendError(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, fmt.Errorf("timeout"))
	client := newTestClient(wrapper)

	_, err := client.SuggestWorkflow(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "copilot request failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSuggestWorkflow_MalformedJSON(t *testing.T) {
	// Use valid JSON that cannot be parsed as []WorkflowSuggestion
	wrapper := mockWrapperWithResponse(sessionEventWithContent(`{"not": "an array"}`), nil)
	client := newTestClient(wrapper)

	_, err := client.SuggestWorkflow(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse suggestions") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- IdentifyMissingComponents tests ---

func TestIdentifyMissingComponents_Success(t *testing.T) {
	specs := []ai.ComponentSpec{
		{
			Name:        "customHandler",
			Type:        "custom.handler",
			Description: "A custom HTTP handler",
			Interface:   "modular.Module",
		},
	}
	specsJSON, _ := json.Marshal(specs)
	wrapper := mockWrapperWithResponse(sessionEventWithContent(string(specsJSON)), nil)
	client := newTestClient(wrapper)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server"},
			{Name: "custom", Type: "custom.handler"},
		},
	}

	result, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 missing component, got %d", len(result))
	}
	if result[0].Name != "customHandler" {
		t.Errorf("expected component name 'customHandler', got '%s'", result[0].Name)
	}
}

func TestIdentifyMissingComponents_NoMissing(t *testing.T) {
	// When ExtractJSON returns "", IdentifyMissingComponents returns nil, nil
	wrapper := mockWrapperWithResponse(sessionEventWithContent("All module types are built-in."), nil)
	client := newTestClient(wrapper)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server"},
		},
	}

	result, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for no missing components, got %v", result)
	}
}

func TestIdentifyMissingComponents_SessionError(t *testing.T) {
	wrapper := mockWrapperSessionError(fmt.Errorf("no CLI"))
	client := newTestClient(wrapper)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test", Type: "test.type"},
		},
	}

	_, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create Copilot session") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestIdentifyMissingComponents_SendError(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, fmt.Errorf("send failed"))
	client := newTestClient(wrapper)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test", Type: "test.type"},
		},
	}

	_, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "copilot request failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestIdentifyMissingComponents_EmptyResponse(t *testing.T) {
	wrapper := mockWrapperWithResponse(nil, nil)
	client := newTestClient(wrapper)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test", Type: "test.type"},
		},
	}

	_, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestIdentifyMissingComponents_MalformedJSON(t *testing.T) {
	wrapper := mockWrapperWithResponse(sessionEventWithContent(`[{"name": bad}]`), nil)
	client := newTestClient(wrapper)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test", Type: "test.type"},
		},
	}

	_, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse missing components") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- Tool handler invocation tests ---

func TestToolHandler_ListComponents(t *testing.T) {
	tools := workflowTools()
	var listTool *copilot.Tool
	for i := range tools {
		if tools[i].Name == "list_components" {
			listTool = &tools[i]
			break
		}
	}
	if listTool == nil {
		t.Fatal("list_components tool not found")
	}

	result, err := listTool.Handler(copilot.ToolInvocation{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.ResultType != "success" {
		t.Errorf("expected resultType 'success', got '%s'", result.ResultType)
	}
	if result.TextResultForLLM == "" {
		t.Error("expected non-empty result text")
	}
	// The result should be valid JSON listing module types.
	var modules map[string]string
	if err := json.Unmarshal([]byte(result.TextResultForLLM), &modules); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := modules["http.server"]; !ok {
		t.Error("expected http.server in listed modules")
	}
}

func TestToolHandler_GetComponentSchema(t *testing.T) {
	tools := workflowTools()
	var schemaTool *copilot.Tool
	for i := range tools {
		if tools[i].Name == "get_component_schema" {
			schemaTool = &tools[i]
			break
		}
	}
	if schemaTool == nil {
		t.Fatal("get_component_schema tool not found")
	}

	result, err := schemaTool.Handler(copilot.ToolInvocation{
		Arguments: map[string]any{"module_type": "http.server"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.ResultType != "success" {
		t.Errorf("expected resultType 'success', got '%s'", result.ResultType)
	}
	if !strings.Contains(result.TextResultForLLM, "address") {
		t.Errorf("expected schema to contain 'address', got: %s", result.TextResultForLLM)
	}
}

func TestToolHandler_ValidateConfig(t *testing.T) {
	tools := workflowTools()
	var validateTool *copilot.Tool
	for i := range tools {
		if tools[i].Name == "validate_config" {
			validateTool = &tools[i]
			break
		}
	}
	if validateTool == nil {
		t.Fatal("validate_config tool not found")
	}

	validYAML := `modules:
  - name: httpServer
    type: http.server
    config:
      address: ":8080"`
	result, err := validateTool.Handler(copilot.ToolInvocation{
		Arguments: map[string]any{"config_yaml": validYAML},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.ResultType != "success" {
		t.Errorf("expected resultType 'success', got '%s'", result.ResultType)
	}
	if !strings.Contains(result.TextResultForLLM, `"valid"`) {
		t.Errorf("expected 'valid' in result, got: %s", result.TextResultForLLM)
	}
}

func TestToolHandler_ValidateConfig_Invalid(t *testing.T) {
	tools := workflowTools()
	var validateTool *copilot.Tool
	for i := range tools {
		if tools[i].Name == "validate_config" {
			validateTool = &tools[i]
			break
		}
	}
	if validateTool == nil {
		t.Fatal("validate_config tool not found")
	}

	// Empty modules should be invalid
	result, err := validateTool.Handler(copilot.ToolInvocation{
		Arguments: map[string]any{"config_yaml": "modules: []"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result.TextResultForLLM, "no modules defined") {
		t.Errorf("expected validation error, got: %s", result.TextResultForLLM)
	}
}

func TestToolHandler_GetExampleWorkflow(t *testing.T) {
	tools := workflowTools()
	var exampleTool *copilot.Tool
	for i := range tools {
		if tools[i].Name == "get_example_workflow" {
			exampleTool = &tools[i]
			break
		}
	}
	if exampleTool == nil {
		t.Fatal("get_example_workflow tool not found")
	}

	result, err := exampleTool.Handler(copilot.ToolInvocation{
		Arguments: map[string]any{"category": "http"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.ResultType != "success" {
		t.Errorf("expected resultType 'success', got '%s'", result.ResultType)
	}
	if !strings.Contains(result.TextResultForLLM, "httpServer") {
		t.Errorf("expected http example to contain 'httpServer', got: %s", result.TextResultForLLM)
	}
}

func TestToolHandler_GetExampleWorkflow_Unknown(t *testing.T) {
	tools := workflowTools()
	var exampleTool *copilot.Tool
	for i := range tools {
		if tools[i].Name == "get_example_workflow" {
			exampleTool = &tools[i]
			break
		}
	}
	if exampleTool == nil {
		t.Fatal("get_example_workflow tool not found")
	}

	result, err := exampleTool.Handler(copilot.ToolInvocation{
		Arguments: map[string]any{"category": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result.TextResultForLLM, "Unknown category") {
		t.Errorf("expected 'Unknown category' error, got: %s", result.TextResultForLLM)
	}
}

// --- Session destroy verification ---

func TestSessionDestroyedAfterUse(t *testing.T) {
	sess := &mockSession{
		sendAndWaitFn: func(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error) {
			return nil, fmt.Errorf("some error")
		},
	}
	wrapper := &mockClientWrapper{
		createSessionFn: func(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error) {
			return sess, nil
		},
	}
	client := newTestClient(wrapper)

	// Even when the call fails, session should be destroyed.
	_, _ = client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if !sess.destroyed {
		t.Error("expected session to be destroyed after use")
	}
}

// --- createSession tests ---

func TestCreateSession_PassesConfig(t *testing.T) {
	var capturedCfg *copilot.SessionConfig
	wrapper := &mockClientWrapper{
		createSessionFn: func(ctx context.Context, cfg *copilot.SessionConfig) (SessionWrapper, error) {
			capturedCfg = cfg
			return &mockSession{
				sendAndWaitFn: func(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error) {
					return nil, nil
				},
			}, nil
		},
	}
	client := &Client{
		cfg: ClientConfig{
			Model: "test-model",
			Provider: &copilot.ProviderConfig{
				Type:    "anthropic",
				BaseURL: "https://api.example.com",
			},
		},
		wrapper: wrapper,
	}

	_, err := client.createSession(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCfg == nil {
		t.Fatal("expected session config to be captured")
	}
	if capturedCfg.Model != "test-model" {
		t.Errorf("expected model 'test-model', got '%s'", capturedCfg.Model)
	}
	if capturedCfg.Provider == nil {
		t.Fatal("expected provider config")
	}
	if capturedCfg.Provider.Type != "anthropic" {
		t.Errorf("expected provider type 'anthropic', got '%s'", capturedCfg.Provider.Type)
	}
	if capturedCfg.SystemMessage == nil {
		t.Fatal("expected system message config")
	}
	if capturedCfg.SystemMessage.Mode != "append" {
		t.Errorf("expected system message mode 'append', got '%s'", capturedCfg.SystemMessage.Mode)
	}
	if len(capturedCfg.Tools) == 0 {
		t.Error("expected tools to be passed to session config")
	}
}
