package integration

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "integration-plugin" {
		t.Fatalf("expected name integration-plugin, got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", p.Version())
	}
}

func TestManifestValidates(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestWorkflowHandlers(t *testing.T) {
	p := New()
	handlers := p.WorkflowHandlers()

	if _, ok := handlers["integration"]; !ok {
		t.Error("missing workflow handler: integration")
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 workflow handler, got %d", len(handlers))
	}
}

func TestWorkflowHandlerFactory(t *testing.T) {
	p := New()
	handlers := p.WorkflowHandlers()
	handler := handlers["integration"]()
	if handler == nil {
		t.Fatal("integration handler factory returned nil")
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	handlers := loader.WorkflowHandlerFactories()
	if len(handlers) != 1 {
		t.Fatalf("expected 1 workflow handler factory after load, got %d", len(handlers))
	}
}
