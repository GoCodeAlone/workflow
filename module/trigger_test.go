package module

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/modular"
)

// TestTriggerRegistry tests the trigger registry functionality
func TestTriggerRegistry(t *testing.T) {
	// Create a new registry
	registry := NewTriggerRegistry()
	if registry == nil {
		t.Fatal("Failed to create trigger registry")
	}

	// Create mock triggers
	trigger1 := &MockTrigger{name: "trigger1"}
	trigger2 := &MockTrigger{name: "trigger2"}

	// Register triggers
	registry.RegisterTrigger(trigger1)
	registry.RegisterTrigger(trigger2)

	// Test GetTrigger
	foundTrigger, ok := registry.GetTrigger("trigger1")
	if !ok {
		t.Error("Failed to find registered trigger1")
	}
	if foundTrigger.Name() != "trigger1" {
		t.Errorf("Expected trigger name 'trigger1', got '%s'", foundTrigger.Name())
	}

	foundTrigger, ok = registry.GetTrigger("trigger2")
	if !ok {
		t.Error("Failed to find registered trigger2")
	}
	if foundTrigger.Name() != "trigger2" {
		t.Errorf("Expected trigger name 'trigger2', got '%s'", foundTrigger.Name())
	}

	// Test non-existent trigger
	_, ok = registry.GetTrigger("non-existent")
	if ok {
		t.Error("GetTrigger should return false for non-existent trigger")
	}

	// Test GetAllTriggers
	allTriggers := registry.GetAllTriggers()
	if len(allTriggers) != 2 {
		t.Errorf("Expected 2 triggers, got %d", len(allTriggers))
	}

	// Verify all triggers are present
	if _, exists := allTriggers["trigger1"]; !exists {
		t.Error("trigger1 not found in GetAllTriggers result")
	}
	if _, exists := allTriggers["trigger2"]; !exists {
		t.Error("trigger2 not found in GetAllTriggers result")
	}
}

// MockTrigger is a simple mock implementation of the Trigger interface for testing
type MockTrigger struct {
	name            string
	initCalled      bool
	startCalled     bool
	stopCalled      bool
	configureCalled bool
}

func (t *MockTrigger) Name() string {
	return t.name
}

func (t *MockTrigger) Init(app modular.Application) error {
	t.initCalled = true
	return nil
}

func (t *MockTrigger) Start(ctx context.Context) error {
	t.startCalled = true
	return nil
}

func (t *MockTrigger) Stop(ctx context.Context) error {
	t.stopCalled = true
	return nil
}

func (t *MockTrigger) Configure(app modular.Application, cfg interface{}) error {
	t.configureCalled = true
	return nil
}
