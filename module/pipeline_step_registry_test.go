package module

import (
	"sort"
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestStepRegistry_RegisterAndCreate(t *testing.T) {
	registry := NewStepRegistry()

	factory := func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		return newMockStep(name, map[string]any{"type": "test"}), nil
	}

	registry.Register("test_step", factory)

	step, err := registry.Create("test_step", "my-step", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "my-step" {
		t.Errorf("expected step name 'my-step', got %q", step.Name())
	}
}

func TestStepRegistry_CreateUnknownType_ReturnsError(t *testing.T) {
	registry := NewStepRegistry()

	_, err := registry.Create("nonexistent", "step1", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown step type")
	}
	expected := "unknown step type: nonexistent"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestStepRegistry_Types_ReturnsRegisteredTypes(t *testing.T) {
	registry := NewStepRegistry()

	noopFactory := func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return newMockStep(name, nil), nil
	}

	registry.Register("alpha", noopFactory)
	registry.Register("beta", noopFactory)
	registry.Register("gamma", noopFactory)

	types := registry.Types()
	if len(types) != 3 {
		t.Fatalf("expected 3 types, got %d", len(types))
	}

	sort.Strings(types)
	expected := []string{"alpha", "beta", "gamma"}
	for i, exp := range expected {
		if types[i] != exp {
			t.Errorf("expected types[%d]=%q, got %q", i, exp, types[i])
		}
	}
}

func TestStepRegistry_Types_EmptyRegistry(t *testing.T) {
	registry := NewStepRegistry()

	types := registry.Types()
	if len(types) != 0 {
		t.Errorf("expected 0 types from empty registry, got %d", len(types))
	}
}

func TestStepRegistry_OverwriteFactory(t *testing.T) {
	registry := NewStepRegistry()

	factory1 := func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return newMockStep(name, map[string]any{"version": 1}), nil
	}
	factory2 := func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return newMockStep(name, map[string]any{"version": 2}), nil
	}

	registry.Register("my_type", factory1)
	registry.Register("my_type", factory2)

	// Should use the latest registered factory
	types := registry.Types()
	if len(types) != 1 {
		t.Errorf("expected 1 type after overwrite, got %d", len(types))
	}
}

func TestStepRegistry_FactoryReceivesConfig(t *testing.T) {
	registry := NewStepRegistry()

	var capturedName string
	var capturedConfig map[string]any

	factory := func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		capturedName = name
		capturedConfig = config
		return newMockStep(name, nil), nil
	}

	registry.Register("configurable", factory)

	cfg := map[string]any{"key": "value", "count": 42}
	_, err := registry.Create("configurable", "test-step", cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedName != "test-step" {
		t.Errorf("expected factory to receive name 'test-step', got %q", capturedName)
	}
	if capturedConfig["key"] != "value" {
		t.Errorf("expected factory to receive config with key='value'")
	}
	if capturedConfig["count"] != 42 {
		t.Errorf("expected factory to receive config with count=42")
	}
}
