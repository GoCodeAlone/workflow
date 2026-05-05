package handlers

import (
	"testing"
)

func TestMCPHandlerConfig(t *testing.T) {
	cfg := MCPHandlerConfig{
		ServerName:   "self_improve",
		LogToolCalls: true,
		Routes: map[string]MCPHandlerRoute{
			"validate_config": {
				Pipeline:    "validate_proposed_config",
				Description: "Validate a proposed config change",
			},
			"diff_config": {
				Pipeline:    "diff_current_vs_proposed",
				Description: "Show diff between current and proposed config",
			},
		},
	}
	if cfg.ServerName != "self_improve" {
		t.Errorf("expected self_improve, got %s", cfg.ServerName)
	}
	if len(cfg.Routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(cfg.Routes))
	}
}

func TestMCPHandlerRouteToToolDefs(t *testing.T) {
	cfg := MCPHandlerConfig{
		ServerName: "test",
		Routes: map[string]MCPHandlerRoute{
			"my_tool": {
				Pipeline:    "my_pipeline",
				Description: "Does a thing",
			},
		},
	}
	tools := cfg.ToToolDefinitions()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool def, got %d", len(tools))
	}
	if tools[0].Name != "my_tool" {
		t.Errorf("expected my_tool, got %s", tools[0].Name)
	}
	if tools[0].Description != "Does a thing" {
		t.Errorf("unexpected description: %s", tools[0].Description)
	}
}

func TestMCPWorkflowHandlerCanHandle(t *testing.T) {
	h := NewMCPWorkflowHandler()
	tests := []struct {
		workflowType string
		expected     bool
	}{
		{"mcp", true},
		{"mcp-self-improve", true},
		{"http", false},
		{"messaging", false},
	}
	for _, tc := range tests {
		got := h.CanHandle(tc.workflowType)
		if got != tc.expected {
			t.Errorf("CanHandle(%q) = %v, want %v", tc.workflowType, got, tc.expected)
		}
	}
}
