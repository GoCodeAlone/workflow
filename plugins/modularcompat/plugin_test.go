package modularcompat

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "modular-compat" {
		t.Fatalf("expected name modular-compat, got %s", p.Name())
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

	for _, name := range []string{"scheduler.modular", "cache.modular", "database.modular"} {
		if _, ok := factories[name]; !ok {
			t.Errorf("missing module factory: %s", name)
		}
	}
	if len(factories) != 3 {
		t.Errorf("expected 3 module factories, got %d", len(factories))
	}
}

func TestSchedulerModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["scheduler.modular"]("test-sched", nil)
	if mod == nil {
		t.Fatal("scheduler.modular factory returned nil")
	}
}

func TestCacheModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["cache.modular"]("test-cache", nil)
	if mod == nil {
		t.Fatal("cache.modular factory returned nil")
	}
}

func TestDatabaseModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["database.modular"]("test-db", nil)
	if mod == nil {
		t.Fatal("database.modular factory returned nil")
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	modules := loader.ModuleFactories()
	if len(modules) != 3 {
		t.Fatalf("expected 3 module factories after load, got %d", len(modules))
	}
}
