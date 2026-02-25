package main

import (
	"strings"
	"testing"
)

func TestKnownModuleTypesPopulated(t *testing.T) {
	types := KnownModuleTypes()
	if len(types) == 0 {
		t.Fatal("expected known module types to be non-empty")
	}
	// Check some well-known types
	expected := []string{
		"storage.sqlite",
		"http.server",
		"http.router",
		"auth.jwt",
		"messaging.broker",
		"statemachine.engine",
		"metrics.collector",
		"health.checker",
		"cache.redis",
	}
	for _, e := range expected {
		if _, ok := types[e]; !ok {
			t.Errorf("expected module type %q to be in registry", e)
		}
	}
}

func TestKnownModuleTypesPluginField(t *testing.T) {
	types := KnownModuleTypes()
	for typeName, info := range types {
		if info.Plugin == "" {
			t.Errorf("module type %q has empty Plugin field", typeName)
		}
		if info.Type != typeName {
			t.Errorf("module type %q has mismatched Type field: %q", typeName, info.Type)
		}
	}
}

func TestKnownModuleTypesStateful(t *testing.T) {
	types := KnownModuleTypes()

	// These should be stateful
	statefulTypes := []string{"storage.sqlite", "database.workflow", "statemachine.engine", "auth.user-store"}
	for _, typeName := range statefulTypes {
		info, ok := types[typeName]
		if !ok {
			t.Errorf("module type %q not found", typeName)
			continue
		}
		if !info.Stateful {
			t.Errorf("expected module type %q to be stateful", typeName)
		}
	}

	// These should NOT be stateful
	nonStatefulTypes := []string{"http.server", "health.checker", "messaging.broker"}
	for _, typeName := range nonStatefulTypes {
		info, ok := types[typeName]
		if !ok {
			t.Errorf("module type %q not found", typeName)
			continue
		}
		if info.Stateful {
			t.Errorf("expected module type %q to NOT be stateful", typeName)
		}
	}
}

func TestKnownStepTypesPopulated(t *testing.T) {
	types := KnownStepTypes()
	if len(types) == 0 {
		t.Fatal("expected known step types to be non-empty")
	}
	expected := []string{
		"step.validate",
		"step.transform",
		"step.json_response",
		"step.db_query",
		"step.publish",
		"step.http_call",
		"step.cache_get",
		"step.rate_limit",
	}
	for _, e := range expected {
		if _, ok := types[e]; !ok {
			t.Errorf("expected step type %q to be in registry", e)
		}
	}
}

func TestKnownStepTypesAllHaveStepPrefix(t *testing.T) {
	types := KnownStepTypes()
	for typeName := range types {
		if !strings.HasPrefix(typeName, "step.") {
			t.Errorf("step type %q does not start with 'step.'", typeName)
		}
	}
}

func TestKnownStepTypesPluginField(t *testing.T) {
	types := KnownStepTypes()
	for typeName, info := range types {
		if info.Plugin == "" {
			t.Errorf("step type %q has empty Plugin field", typeName)
		}
		if info.Type != typeName {
			t.Errorf("step type %q has mismatched Type field: %q", typeName, info.Type)
		}
	}
}

func TestKnownTriggerTypes(t *testing.T) {
	triggers := KnownTriggerTypes()
	expected := []string{"http", "event", "schedule"}
	for _, e := range expected {
		if !triggers[e] {
			t.Errorf("expected trigger type %q to be known", e)
		}
	}
}

func TestModuleTypeCount(t *testing.T) {
	types := KnownModuleTypes()
	// We should have a substantial number of module types
	if len(types) < 30 {
		t.Errorf("expected at least 30 module types, got %d", len(types))
	}
}

func TestStepTypeCount(t *testing.T) {
	types := KnownStepTypes()
	// We should have a substantial number of step types
	if len(types) < 20 {
		t.Errorf("expected at least 20 step types, got %d", len(types))
	}
}
