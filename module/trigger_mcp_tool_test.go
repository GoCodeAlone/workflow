package module

import (
	"context"
	"testing"
)

func TestMCPToolTriggerConfig(t *testing.T) {
	cfg := MCPToolTriggerConfig{
		ToolName:    "analyze_logs",
		Description: "Analyze application logs",
		Parameters: []MCPToolParameter{
			{Name: "timeframe", Type: "string", Required: true},
			{Name: "severity", Type: "string", Enum: []string{"info", "warn", "error"}},
		},
	}
	if cfg.ToolName != "analyze_logs" {
		t.Errorf("expected analyze_logs, got %s", cfg.ToolName)
	}
	if len(cfg.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(cfg.Parameters))
	}
}

func TestMCPToolTriggerToToolDef(t *testing.T) {
	cfg := MCPToolTriggerConfig{
		ToolName:    "search_tasks",
		Description: "Search tasks by keyword",
		Parameters: []MCPToolParameter{
			{Name: "query", Type: "string", Required: true, Description: "Search query"},
			{Name: "limit", Type: "integer", Required: false, Description: "Max results"},
		},
	}
	def := cfg.ToToolDefinition()
	if def.Name != "search_tasks" {
		t.Errorf("expected search_tasks, got %s", def.Name)
	}
	if def.Description != "Search tasks by keyword" {
		t.Errorf("unexpected description: %s", def.Description)
	}
	props := def.InputSchema.Properties
	if _, ok := props["query"]; !ok {
		t.Error("missing query property")
	}
	if _, ok := props["limit"]; !ok {
		t.Error("missing limit property")
	}
	// Required should contain "query" only
	if len(def.InputSchema.Required) != 1 || def.InputSchema.Required[0] != "query" {
		t.Errorf("expected [query] required, got %v", def.InputSchema.Required)
	}
}

func TestMCPToolTriggerRuntime(t *testing.T) {
	called := false
	var capturedPipeline string
	var capturedArgs map[string]any

	executor := &mockPipelineExecutor{
		fn: func(ctx context.Context, name string, data map[string]any) (map[string]any, error) {
			called = true
			capturedPipeline = name
			capturedArgs = data
			return map[string]any{"result": "ok"}, nil
		},
	}

	cfg := MCPToolTriggerConfig{
		ToolName:    "analyze_logs",
		Description: "Analyze logs",
		Parameters: []MCPToolParameter{
			{Name: "timeframe", Type: "string", Required: true},
		},
	}

	runtime := NewMCPToolTriggerRuntime(cfg, "pipeline:analyze-logs", executor)

	args := map[string]any{"timeframe": "1h"}
	result, err := runtime.HandleToolCall(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected executor to be called")
	}
	if capturedPipeline != "pipeline:analyze-logs" {
		t.Errorf("expected pipeline:analyze-logs, got %s", capturedPipeline)
	}
	if capturedArgs["timeframe"] != "1h" {
		t.Errorf("expected timeframe=1h, got %v", capturedArgs["timeframe"])
	}
	if result == nil {
		t.Error("expected non-nil result")
	}

	// Test ToolName
	if runtime.ToolName() != "analyze_logs" {
		t.Errorf("expected analyze_logs, got %s", runtime.ToolName())
	}
}

// mockPipelineExecutor is a test double for interfaces.PipelineExecutor.
type mockPipelineExecutor struct {
	fn func(ctx context.Context, name string, data map[string]any) (map[string]any, error)
}

func (m *mockPipelineExecutor) ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error) {
	return m.fn(ctx, name, data)
}
