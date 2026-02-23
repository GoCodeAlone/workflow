package timeline

import (
	"testing"
)

func TestPlugin_New(t *testing.T) {
	p := New()
	if p.Name() != "timeline" {
		t.Errorf("Name() = %q, want %q", p.Name(), "timeline")
	}
	if p.Version() != "1.0.0" {
		t.Errorf("Version() = %q, want %q", p.Version(), "1.0.0")
	}
}

func TestPlugin_ModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	if _, ok := factories["timeline.service"]; !ok {
		t.Error("ModuleFactories() missing timeline.service")
	}
}

func TestPlugin_ModuleFactory_Creates(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	factory := factories["timeline.service"]

	mod := factory("test-timeline", map[string]any{
		"event_store": "some-event-store",
	})
	if mod == nil {
		t.Fatal("factory returned nil")
	}
	if mod.Name() != "test-timeline" {
		t.Errorf("Name() = %q, want %q", mod.Name(), "test-timeline")
	}
}
