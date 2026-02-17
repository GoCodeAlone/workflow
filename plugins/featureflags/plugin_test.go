package featureflags

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "feature-flags" {
		t.Fatalf("expected name feature-flags, got %s", p.Name())
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

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	if _, ok := factories["featureflag.service"]; !ok {
		t.Error("missing module factory: featureflag.service")
	}
	if len(factories) != 1 {
		t.Errorf("expected 1 module factory, got %d", len(factories))
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	factories := p.StepFactories()

	for _, name := range []string{"step.feature_flag", "step.ff_gate"} {
		if _, ok := factories[name]; !ok {
			t.Errorf("missing step factory: %s", name)
		}
	}
	if len(factories) != 2 {
		t.Errorf("expected 2 step factories, got %d", len(factories))
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	modules := loader.ModuleFactories()
	if len(modules) != 1 {
		t.Fatalf("expected 1 module factory after load, got %d", len(modules))
	}

	steps := loader.StepFactories()
	if len(steps) != 2 {
		t.Fatalf("expected 2 step factories after load, got %d", len(steps))
	}
}
