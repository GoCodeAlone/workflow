package copilotai

import (
	"testing"
)

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
	if client.copilotCli == nil {
		t.Error("expected non-nil copilotCli")
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
		"get_example_workflow":  false,
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
