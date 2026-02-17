package plugin

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestPluginWorkflowRegistry_RegisterAndGet(t *testing.T) {
	r := NewPluginWorkflowRegistry()

	wf := EmbeddedWorkflow{
		Name:        "payment-flow",
		Description: "Handles payment processing",
		Config:      &config.WorkflowConfig{},
	}

	if err := r.Register("billing", wf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := r.Get("billing:payment-flow")
	if !ok {
		t.Fatal("expected workflow to be found")
	}
	if got.Name != "payment-flow" {
		t.Errorf("got name %q, want %q", got.Name, "payment-flow")
	}
	if got.Description != "Handles payment processing" {
		t.Errorf("got description %q, want %q", got.Description, "Handles payment processing")
	}
}

func TestPluginWorkflowRegistry_GetNotFound(t *testing.T) {
	r := NewPluginWorkflowRegistry()

	_, ok := r.Get("nonexistent:workflow")
	if ok {
		t.Fatal("expected workflow not to be found")
	}
}

func TestPluginWorkflowRegistry_DuplicateRegister(t *testing.T) {
	r := NewPluginWorkflowRegistry()

	wf := EmbeddedWorkflow{Name: "wf1"}
	if err := r.Register("p1", wf); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	err := r.Register("p1", wf)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestPluginWorkflowRegistry_EmptyPluginName(t *testing.T) {
	r := NewPluginWorkflowRegistry()
	err := r.Register("", EmbeddedWorkflow{Name: "wf1"})
	if err == nil {
		t.Fatal("expected error for empty plugin name")
	}
}

func TestPluginWorkflowRegistry_EmptyWorkflowName(t *testing.T) {
	r := NewPluginWorkflowRegistry()
	err := r.Register("p1", EmbeddedWorkflow{})
	if err == nil {
		t.Fatal("expected error for empty workflow name")
	}
}

func TestPluginWorkflowRegistry_List(t *testing.T) {
	r := NewPluginWorkflowRegistry()

	_ = r.Register("billing", EmbeddedWorkflow{Name: "payment"})
	_ = r.Register("billing", EmbeddedWorkflow{Name: "refund"})
	_ = r.Register("shipping", EmbeddedWorkflow{Name: "track"})

	names := r.List()
	if len(names) != 3 {
		t.Fatalf("got %d names, want 3", len(names))
	}

	// List returns sorted names
	expected := []string{"billing:payment", "billing:refund", "shipping:track"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestPluginWorkflowRegistry_Unregister(t *testing.T) {
	r := NewPluginWorkflowRegistry()

	_ = r.Register("billing", EmbeddedWorkflow{Name: "payment"})
	_ = r.Register("billing", EmbeddedWorkflow{Name: "refund"})
	_ = r.Register("shipping", EmbeddedWorkflow{Name: "track"})

	r.Unregister("billing")

	names := r.List()
	if len(names) != 1 {
		t.Fatalf("got %d names after unregister, want 1", len(names))
	}
	if names[0] != "shipping:track" {
		t.Errorf("remaining workflow = %q, want %q", names[0], "shipping:track")
	}

	// Verify billing workflows are gone
	if _, ok := r.Get("billing:payment"); ok {
		t.Error("billing:payment should have been unregistered")
	}
	if _, ok := r.Get("billing:refund"); ok {
		t.Error("billing:refund should have been unregistered")
	}
}

func TestPluginWorkflowRegistry_UnregisterNonexistent(t *testing.T) {
	r := NewPluginWorkflowRegistry()
	_ = r.Register("p1", EmbeddedWorkflow{Name: "wf1"})

	// Should not panic or remove anything
	r.Unregister("nonexistent")

	if len(r.List()) != 1 {
		t.Error("unregister of nonexistent plugin should not affect other workflows")
	}
}

func TestPluginWorkflowRegistry_ConfigYAML(t *testing.T) {
	r := NewPluginWorkflowRegistry()

	yamlConfig := `
modules:
  - name: log
    type: logger
workflows: {}
triggers: {}
`

	wf := EmbeddedWorkflow{
		Name:       "yaml-workflow",
		ConfigYAML: yamlConfig,
	}

	if err := r.Register("test", wf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := r.Get("test:yaml-workflow")
	if !ok {
		t.Fatal("expected workflow to be found")
	}
	if got.ConfigYAML != yamlConfig {
		t.Error("ConfigYAML was not preserved")
	}
}
