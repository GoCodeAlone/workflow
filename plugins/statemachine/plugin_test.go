package statemachine

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

func TestPluginImplementsEnginePlugin(t *testing.T) {
	p := New()
	var _ plugin.EnginePlugin = p
}

func TestPluginManifest(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	if m.Name != "statemachine" {
		t.Errorf("expected name %q, got %q", "statemachine", m.Name)
	}
	if len(m.ModuleTypes) != 3 {
		t.Errorf("expected 3 module types, got %d", len(m.ModuleTypes))
	}
	if len(m.StepTypes) != 2 {
		t.Errorf("expected 2 step types, got %d", len(m.StepTypes))
	}
	if len(m.WorkflowTypes) != 1 {
		t.Errorf("expected 1 workflow type, got %d", len(m.WorkflowTypes))
	}
}

func TestPluginCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	names := map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
	}
	for _, expected := range []string{"state-machine", "state-tracking"} {
		if !names[expected] {
			t.Errorf("missing capability %q", expected)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedTypes := []string{"statemachine.engine", "state.tracker", "state.connector"}
	for _, typ := range expectedTypes {
		factory, ok := factories[typ]
		if !ok {
			t.Errorf("missing factory for %q", typ)
			continue
		}
		mod := factory("test-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("factory for %q returned nil", typ)
		}
	}
}

func TestModuleFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	// Test statemachine.engine with config
	engineMod := factories["statemachine.engine"]("sm-engine", map[string]any{
		"maxInstances": float64(500),
		"instanceTTL":  "12h",
	})
	if engineMod == nil {
		t.Fatal("statemachine.engine factory returned nil with config")
	}

	// Test state.tracker with config
	trackerMod := factories["state.tracker"]("sm-tracker", map[string]any{
		"retentionDays": float64(60),
	})
	if trackerMod == nil {
		t.Fatal("state.tracker factory returned nil with config")
	}
}

func TestWorkflowHandlers(t *testing.T) {
	p := New()
	wfHandlers := p.WorkflowHandlers()

	factory, ok := wfHandlers["statemachine"]
	if !ok {
		t.Fatal("missing statemachine workflow handler factory")
	}
	handler := factory()
	if handler == nil {
		t.Fatal("statemachine workflow handler factory returned nil")
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	factories := p.StepFactories()

	expectedTypes := []string{"step.statemachine_transition", "step.statemachine_get"}
	for _, typ := range expectedTypes {
		if _, ok := factories[typ]; !ok {
			t.Errorf("missing step factory for %q", typ)
		}
	}
	if len(factories) != len(expectedTypes) {
		t.Errorf("expected %d step factories, got %d", len(expectedTypes), len(factories))
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) != 3 {
		t.Fatalf("expected 3 module schemas, got %d", len(schemas))
	}

	types := map[string]bool{}
	for _, s := range schemas {
		types[s.Type] = true
	}
	for _, expected := range []string{"statemachine.engine", "state.tracker", "state.connector"} {
		if !types[expected] {
			t.Errorf("missing schema for %q", expected)
		}
	}
}
