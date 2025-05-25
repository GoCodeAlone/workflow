package config

import (
	"testing"
)

func TestWorkflowConfig(t *testing.T) {
	// Simple test to verify config package can be imported and tested
	cfg := &WorkflowConfig{
		Name:        "test",
		Description: "Test workflow",
	}
	
	if cfg.Name != "test" {
		t.Errorf("Expected name to be 'test', got '%s'", cfg.Name)
	}
	
	if cfg.Description != "Test workflow" {
		t.Errorf("Expected description to be 'Test workflow', got '%s'", cfg.Description)
	}
}