package module

import (
	"testing"
)

func TestNewEventProcessorLocator(t *testing.T) {
	app := CreateIsolatedApp(t)
	locator := NewEventProcessorLocator(app)
	if locator.App == nil {
		t.Fatal("expected App to be set")
	}
}

func TestEventProcessorLocator_Locate_Found(t *testing.T) {
	app := CreateIsolatedApp(t)
	ep := NewEventProcessor("eventProcessor")
	if err := ep.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	locator := NewEventProcessorLocator(app)
	found, err := locator.Locate("eventProcessor")
	if err != nil {
		t.Fatalf("Locate failed: %v", err)
	}
	if found != ep {
		t.Error("expected Locate to return the registered event processor")
	}
}

func TestEventProcessorLocator_Locate_NotFound(t *testing.T) {
	app := CreateIsolatedApp(t)
	locator := NewEventProcessorLocator(app)

	_, err := locator.Locate("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent processor")
	}
}

func TestEventProcessorLocator_LocateDefault(t *testing.T) {
	app := CreateIsolatedApp(t)
	ep := NewEventProcessor("eventProcessor")
	if err := ep.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	locator := NewEventProcessorLocator(app)
	found, err := locator.LocateDefault()
	if err != nil {
		t.Fatalf("LocateDefault failed: %v", err)
	}
	if found != ep {
		t.Error("expected LocateDefault to find the event processor")
	}
}

func TestEventProcessorLocator_LocateDefault_NotFound(t *testing.T) {
	app := CreateIsolatedApp(t)
	locator := NewEventProcessorLocator(app)

	_, err := locator.LocateDefault()
	if err == nil {
		t.Fatal("expected error when no default processor exists")
	}
}

func TestGetProcessor(t *testing.T) {
	app := CreateIsolatedApp(t)
	ep := NewEventProcessor("eventProcessor")
	if err := ep.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	found, err := GetProcessor(app)
	if err != nil {
		t.Fatalf("GetProcessor failed: %v", err)
	}
	if found != ep {
		t.Error("expected GetProcessor to return the registered event processor")
	}
}

func TestGetProcessor_NotFound(t *testing.T) {
	app := CreateIsolatedApp(t)

	_, err := GetProcessor(app)
	if err == nil {
		t.Fatal("expected error when no processor exists")
	}
}
