package module

import (
	"os"
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestEventStoreServiceModule_Name(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{
		DBPath:        dbPath,
		RetentionDays: 30,
	})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}
	if m.Name() != "test-es" {
		t.Errorf("Name() = %q, want %q", m.Name(), "test-es")
	}
}

func TestEventStoreServiceModule_Init(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}
	if err := m.Init(nil); err != nil {
		t.Errorf("Init() error = %v", err)
	}
}

func TestEventStoreServiceModule_ProvidesServices(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}

	providers := m.ProvidesServices()
	if len(providers) != 2 {
		t.Fatalf("ProvidesServices() returned %d providers, want 2", len(providers))
	}

	if providers[0].Name != "test-es" {
		t.Errorf("providers[0].Name = %q, want %q", providers[0].Name, "test-es")
	}
	if providers[1].Name != "test-es.admin" {
		t.Errorf("providers[1].Name = %q, want %q", providers[1].Name, "test-es.admin")
	}
}

func TestEventStoreServiceModule_RequiresServices(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}
	deps := m.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("RequiresServices() returned %d deps, want 0", len(deps))
	}
}

func TestEventStoreServiceModule_Store(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}
	if m.Store() == nil {
		t.Error("Store() returned nil")
	}
}

func TestEventStoreServiceModule_RetentionDays(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{
		DBPath:        dbPath,
		RetentionDays: 60,
	})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}
	if m.RetentionDays() != 60 {
		t.Errorf("RetentionDays() = %d, want 60", m.RetentionDays())
	}
}

func TestEventStoreServiceModule_DefaultDBPath(t *testing.T) {
	// Test that an empty DBPath falls back to the default "data/events.db" path.
	m, err := NewEventStoreServiceModule("test-es", EventStoreServiceConfig{})
	if err != nil {
		t.Fatalf("NewEventStoreServiceModule() error = %v", err)
	}
	if m.Store() == nil {
		t.Error("Store() returned nil with default path")
	}
	// Clean up the created data directory (created as side-effect of default path).
	_ = os.RemoveAll("data")
}

// Verify EventStoreServiceModule satisfies the modular.Module interface.
var _ modular.Module = (*EventStoreServiceModule)(nil)
