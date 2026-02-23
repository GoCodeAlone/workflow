package module

import (
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestDLQServiceModule_Name(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{MaxRetries: 5, RetentionDays: 14})
	if m.Name() != "test-dlq" {
		t.Errorf("Name() = %q, want %q", m.Name(), "test-dlq")
	}
}

func TestDLQServiceModule_Init(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{})
	if err := m.Init(nil); err != nil {
		t.Errorf("Init() error = %v", err)
	}
}

func TestDLQServiceModule_ProvidesServices(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{})

	providers := m.ProvidesServices()
	if len(providers) != 3 {
		t.Fatalf("ProvidesServices() returned %d providers, want 3", len(providers))
	}

	expected := map[string]bool{
		"test-dlq":       false,
		"test-dlq.admin": false,
		"test-dlq.store": false,
	}
	for _, p := range providers {
		if _, ok := expected[p.Name]; !ok {
			t.Errorf("unexpected service name %q", p.Name)
		}
		expected[p.Name] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected service %q not found", name)
		}
	}
}

func TestDLQServiceModule_RequiresServices(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{})
	deps := m.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("RequiresServices() returned %d deps, want 0", len(deps))
	}
}

func TestDLQServiceModule_DLQMux(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{})
	if m.DLQMux() == nil {
		t.Error("DLQMux() returned nil")
	}
}

func TestDLQServiceModule_Store(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{})
	if m.Store() == nil {
		t.Error("Store() returned nil")
	}
}

func TestDLQServiceModule_Config(t *testing.T) {
	m := NewDLQServiceModule("test-dlq", DLQServiceConfig{MaxRetries: 5, RetentionDays: 14})
	if m.MaxRetries() != 5 {
		t.Errorf("MaxRetries() = %d, want 5", m.MaxRetries())
	}
	if m.RetentionDays() != 14 {
		t.Errorf("RetentionDays() = %d, want 14", m.RetentionDays())
	}
}

// Verify DLQServiceModule satisfies the modular.Module interface.
var _ modular.Module = (*DLQServiceModule)(nil)
