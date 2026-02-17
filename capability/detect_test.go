package capability

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestDetectRequired_EmptyConfig(t *testing.T) {
	ResetMappings()
	cfg := config.NewEmptyWorkflowConfig()
	caps := DetectRequired(cfg)
	if len(caps) != 0 {
		t.Errorf("expected no capabilities for empty config, got %v", caps)
	}
}

func TestDetectRequired_ModuleTypes(t *testing.T) {
	ResetMappings()
	RegisterModuleTypeMapping("http.server", "http-server")
	RegisterModuleTypeMapping("http.router", "http-server")
	RegisterModuleTypeMapping("messaging.broker", "messaging")

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server"},
			{Name: "router", Type: "http.router"},
			{Name: "broker", Type: "messaging.broker"},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
	}

	caps := DetectRequired(cfg)
	expected := []string{"http-server", "messaging"}
	if len(caps) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, caps)
	}
	for i, c := range caps {
		if c != expected[i] {
			t.Errorf("caps[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestDetectRequired_TriggerTypes(t *testing.T) {
	ResetMappings()
	RegisterTriggerTypeMapping("http", "http-server")
	RegisterTriggerTypeMapping("schedule", "scheduler")

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: make(map[string]any),
		Triggers: map[string]any{
			"http":     map[string]any{},
			"schedule": map[string]any{},
		},
	}

	caps := DetectRequired(cfg)
	expected := []string{"http-server", "scheduler"}
	if len(caps) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, caps)
	}
	for i, c := range caps {
		if c != expected[i] {
			t.Errorf("caps[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestDetectRequired_WorkflowTypes(t *testing.T) {
	ResetMappings()
	RegisterWorkflowTypeMapping("http", "http-server")
	RegisterWorkflowTypeMapping("statemachine", "state-machine")

	cfg := &config.WorkflowConfig{
		Modules:  []config.ModuleConfig{},
		Triggers: make(map[string]any),
		Workflows: map[string]any{
			"http":         map[string]any{},
			"statemachine": map[string]any{},
		},
	}

	caps := DetectRequired(cfg)
	expected := []string{"http-server", "state-machine"}
	if len(caps) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, caps)
	}
	for i, c := range caps {
		if c != expected[i] {
			t.Errorf("caps[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestDetectRequired_Deduplication(t *testing.T) {
	ResetMappings()
	RegisterModuleTypeMapping("http.server", "http-server", "networking")
	RegisterModuleTypeMapping("http.router", "http-server")
	RegisterTriggerTypeMapping("http", "http-server")
	RegisterWorkflowTypeMapping("http", "http-server")

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server"},
			{Name: "router", Type: "http.router"},
		},
		Triggers: map[string]any{
			"http": map[string]any{},
		},
		Workflows: map[string]any{
			"http": map[string]any{},
		},
	}

	caps := DetectRequired(cfg)
	expected := []string{"http-server", "networking"}
	if len(caps) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, caps)
	}
	for i, c := range caps {
		if c != expected[i] {
			t.Errorf("caps[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestDetectRequired_UnknownTypesIgnored(t *testing.T) {
	ResetMappings()
	RegisterModuleTypeMapping("http.server", "http-server")

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server"},
			{Name: "custom", Type: "custom.unknown"},
		},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
	}

	caps := DetectRequired(cfg)
	if len(caps) != 1 || caps[0] != "http-server" {
		t.Errorf("expected [http-server], got %v", caps)
	}
}
