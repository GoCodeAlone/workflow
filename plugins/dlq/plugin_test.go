package dlq

import (
	"testing"
)

func TestPlugin_New(t *testing.T) {
	p := New()
	if p.Name() != "dlq" {
		t.Errorf("Name() = %q, want %q", p.Name(), "dlq")
	}
	if p.Version() != "1.0.0" {
		t.Errorf("Version() = %q, want %q", p.Version(), "1.0.0")
	}
}

func TestPlugin_ModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	if _, ok := factories["dlq.service"]; !ok {
		t.Error("ModuleFactories() missing dlq.service")
	}
}

func TestPlugin_ModuleFactory_Creates(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	factory := factories["dlq.service"]

	mod := factory("test-dlq", map[string]any{
		"max_retries":    5,
		"retention_days": 14,
	})
	if mod == nil {
		t.Fatal("factory returned nil")
	}
	if mod.Name() != "test-dlq" {
		t.Errorf("Name() = %q, want %q", mod.Name(), "test-dlq")
	}
}
