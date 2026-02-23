package module

import (
	"testing"

	"github.com/CrisisTextLine/modular"
	evstore "github.com/GoCodeAlone/workflow/store"
)

func TestTimelineServiceModule_Name(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	es, err := evstore.NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore() error = %v", err)
	}
	m := NewTimelineServiceModule("test-timeline", es)
	if m.Name() != "test-timeline" {
		t.Errorf("Name() = %q, want %q", m.Name(), "test-timeline")
	}
}

func TestTimelineServiceModule_Init(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	es, err := evstore.NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore() error = %v", err)
	}
	m := NewTimelineServiceModule("test-timeline", es)
	if err := m.Init(nil); err != nil {
		t.Errorf("Init() error = %v", err)
	}
}

func TestTimelineServiceModule_ProvidesServices(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	es, err := evstore.NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore() error = %v", err)
	}
	m := NewTimelineServiceModule("test-timeline", es)

	providers := m.ProvidesServices()
	if len(providers) != 4 {
		t.Fatalf("ProvidesServices() returned %d providers, want 4", len(providers))
	}

	expected := map[string]bool{
		"test-timeline":          false,
		"test-timeline.timeline": false,
		"test-timeline.replay":   false,
		"test-timeline.backfill": false,
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

func TestTimelineServiceModule_RequiresServices(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	es, err := evstore.NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore() error = %v", err)
	}
	m := NewTimelineServiceModule("test-timeline", es)
	deps := m.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("RequiresServices() returned %d deps, want 0", len(deps))
	}
}

func TestTimelineServiceModule_Muxes(t *testing.T) {
	dbPath := t.TempDir() + "/test-events.db"
	es, err := evstore.NewSQLiteEventStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteEventStore() error = %v", err)
	}
	m := NewTimelineServiceModule("test-timeline", es)

	if m.TimelineMux() == nil {
		t.Error("TimelineMux() returned nil")
	}
	if m.ReplayMux() == nil {
		t.Error("ReplayMux() returned nil")
	}
	if m.BackfillMux() == nil {
		t.Error("BackfillMux() returned nil")
	}
}

// Verify TimelineServiceModule satisfies the modular.Module interface.
var _ modular.Module = (*TimelineServiceModule)(nil)
