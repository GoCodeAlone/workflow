package setup

import (
	"testing"
)

func TestDefaultHandlers(t *testing.T) {
	handlers := DefaultHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one default handler")
	}
	// Should have 9 built-in handlers (HTTP, Messaging, StateMachine, Scheduler,
	// Integration, Pipeline, Event, Platform, CLI)
	if len(handlers) != 9 {
		t.Errorf("expected 9 default handlers, got %d", len(handlers))
	}
}

func TestDefaultTriggers(t *testing.T) {
	triggers := DefaultTriggers()
	if len(triggers) == 0 {
		t.Fatal("expected at least one default trigger")
	}
	// Should have 6 built-in triggers
	if len(triggers) != 6 {
		t.Errorf("expected 6 default triggers, got %d", len(triggers))
	}
}
