package eventstore

import (
	"testing"
)

func TestPlugin_New(t *testing.T) {
	p := New()
	if p.Name() != "eventstore" {
		t.Errorf("Name() = %q, want %q", p.Name(), "eventstore")
	}
	if p.Version() != "1.0.0" {
		t.Errorf("Version() = %q, want %q", p.Version(), "1.0.0")
	}
}

func TestPlugin_ModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	if _, ok := factories["eventstore.service"]; !ok {
		t.Error("ModuleFactories() missing eventstore.service")
	}
}

func TestPlugin_ModuleFactory_Creates(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	factory := factories["eventstore.service"]

	dbPath := t.TempDir() + "/test.db"
	mod := factory("test-es", map[string]any{
		"db_path":        dbPath,
		"retention_days": 60,
	})
	if mod == nil {
		t.Fatal("factory returned nil")
	}
	if mod.Name() != "test-es" {
		t.Errorf("Name() = %q, want %q", mod.Name(), "test-es")
	}
}
