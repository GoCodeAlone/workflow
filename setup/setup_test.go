package setup

import (
	"testing"
)

func TestDefaultHandlers(t *testing.T) {
	handlers := DefaultHandlers()
	if len(handlers) == 0 {
		t.Fatal("expected at least one default handler")
	}
	// Should have 8 built-in handlers
	if len(handlers) != 8 {
		t.Errorf("expected 8 default handlers, got %d", len(handlers))
	}
}

func TestDefaultTriggers(t *testing.T) {
	triggers := DefaultTriggers()
	if len(triggers) == 0 {
		t.Fatal("expected at least one default trigger")
	}
	// Should have 5 built-in triggers
	if len(triggers) != 5 {
		t.Errorf("expected 5 default triggers, got %d", len(triggers))
	}
}
