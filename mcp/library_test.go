package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestNewInProcessServer(t *testing.T) {
	s := NewInProcessServer()
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	// Verify all tools are registered
	tools := s.ListTools()
	if len(tools) == 0 {
		t.Fatal("expected tools to be registered")
	}
	// Verify key tools exist
	expected := []string{
		"validate_config", "template_validate_config", "inspect_config",
		"list_module_types", "list_step_types", "list_trigger_types",
		"get_module_schema", "get_step_schema", "modernize",
	}
	toolMap := make(map[string]bool)
	for _, tool := range tools {
		toolMap[tool] = true
	}
	for _, name := range expected {
		if !toolMap[name] {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestInProcessToolInvocation(t *testing.T) {
	s := NewInProcessServer()
	ctx := context.Background()

	// Call validate_config with a simple valid config
	result, err := s.CallTool(ctx, "validate_config", map[string]any{
		"yaml_content": "modules:\n  - name: server\n    type: http.server\n    config:\n      port: 8080\n",
	})
	if err != nil {
		t.Fatalf("validate_config failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestInProcessListModuleTypes(t *testing.T) {
	s := NewInProcessServer()
	ctx := context.Background()

	result, err := s.CallTool(ctx, "list_module_types", map[string]any{})
	if err != nil {
		t.Fatalf("list_module_types failed: %v", err)
	}
	// Should contain at least http.server
	content, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty module types list")
	}
	if !strings.Contains(content, "http.server") {
		t.Errorf("expected module types to include http.server, got: %s", content)
	}
}

func TestInProcessCallTool_UnknownTool(t *testing.T) {
	s := NewInProcessServer()
	ctx := context.Background()

	_, err := s.CallTool(ctx, "nonexistent_tool", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestInProcessListToolsNotEmpty(t *testing.T) {
	s := NewInProcessServer()
	tools := s.ListTools()
	if len(tools) < 20 {
		t.Errorf("expected at least 20 tools, got %d: %v", len(tools), tools)
	}
}

func TestInProcessServer_ImplementsMCPProvider(t *testing.T) {
	// Compile-time check: InProcessServer must satisfy MCPProvider
	var _ MCPProvider = (*InProcessServer)(nil)
}
