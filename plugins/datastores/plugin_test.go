package datastores

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "datastores" {
		t.Fatalf("expected name datastores, got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", p.Version())
	}
}

func TestManifestValidates(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	factories := p.StepFactories()

	expectedSteps := []string{
		"step.nosql_get",
		"step.nosql_put",
		"step.nosql_delete",
		"step.nosql_query",
	}

	for _, stepType := range expectedSteps {
		if _, ok := factories[stepType]; !ok {
			t.Errorf("missing step factory: %s", stepType)
		}
	}

	if len(factories) != len(expectedSteps) {
		t.Errorf("expected %d step factories, got %d", len(expectedSteps), len(factories))
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedModules := []string{
		"nosql.memory",
		"nosql.dynamodb",
		"nosql.mongodb",
		"nosql.redis",
	}

	for _, modType := range expectedModules {
		if _, ok := factories[modType]; !ok {
			t.Errorf("missing module factory: %s", modType)
		}
	}

	if len(factories) != len(expectedModules) {
		t.Errorf("expected %d module factories, got %d", len(expectedModules), len(factories))
	}
}

func TestCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) == 0 {
		t.Fatal("expected at least one capability")
	}
	for _, c := range caps {
		if c.Name == "" {
			t.Error("capability has empty name")
		}
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	steps := loader.StepFactories()
	if len(steps) != len(p.StepFactories()) {
		t.Fatalf("expected %d step factories after load, got %d", len(p.StepFactories()), len(steps))
	}
}
