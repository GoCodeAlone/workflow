package openapi

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "workflow-plugin-openapi" {
		t.Fatalf("expected name workflow-plugin-openapi, got %s", p.Name())
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

	expectedModules := []string{
		"openapi",
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

func TestWiringHooks(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()
	if len(hooks) == 0 {
		t.Fatal("expected at least one wiring hook")
	}
	for _, h := range hooks {
		if h.Name == "" {
			t.Error("wiring hook has empty name")
		}
		if h.Hook == nil {
			t.Errorf("wiring hook %q has nil Hook func", h.Name)
		}
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) == 0 {
		t.Fatal("expected at least one module schema")
	}
	for _, s := range schemas {
		if s.Type == "" {
			t.Error("module schema has empty type")
		}
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}
}
