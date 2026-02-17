package messaging

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "messaging" {
		t.Fatalf("expected name messaging, got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", p.Version())
	}
	if p.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestManifestValidates(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestInterfaceSatisfaction(t *testing.T) {
	var _ plugin.EnginePlugin = (*Plugin)(nil)
}

func TestCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}

	names := map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
		if c.InterfaceType == nil {
			t.Errorf("capability %q has nil InterfaceType", c.Name)
		}
	}

	for _, expected := range []string{"message-broker", "message-handler"} {
		if !names[expected] {
			t.Errorf("missing capability: %s", expected)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedModules := []string{
		"messaging.broker",
		"messaging.broker.eventbus",
		"messaging.handler",
		"messaging.nats",
		"messaging.kafka",
		"notification.slack",
		"webhook.sender",
	}

	for _, moduleType := range expectedModules {
		if _, ok := factories[moduleType]; !ok {
			t.Errorf("missing module factory: %s", moduleType)
		}
	}

	if len(factories) != len(expectedModules) {
		t.Errorf("expected %d module factories, got %d", len(expectedModules), len(factories))
	}
}

func TestModuleFactoriesCreateModules(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	tests := []struct {
		moduleType string
		config     map[string]any
	}{
		{"messaging.broker", map[string]any{"maxQueueSize": float64(500), "deliveryTimeout": "10s"}},
		{"messaging.broker.eventbus", map[string]any{}},
		{"messaging.handler", map[string]any{}},
		{"messaging.nats", map[string]any{}},
		{"messaging.kafka", map[string]any{"brokers": []any{"localhost:9092"}, "groupId": "test-group"}},
		{"notification.slack", map[string]any{}},
		{"webhook.sender", map[string]any{"maxRetries": float64(5)}},
	}

	for _, tt := range tests {
		t.Run(tt.moduleType, func(t *testing.T) {
			factory, ok := factories[tt.moduleType]
			if !ok {
				t.Fatalf("missing factory for %s", tt.moduleType)
			}
			mod := factory("test-"+tt.moduleType, tt.config)
			if mod == nil {
				t.Fatalf("factory for %s returned nil", tt.moduleType)
			}
			if mod.Name() == "" {
				t.Fatalf("module from %s has empty name", tt.moduleType)
			}
		})
	}
}

func TestTriggerFactories(t *testing.T) {
	p := New()
	factories := p.TriggerFactories()

	expectedTriggers := []string{"event", "eventbus"}

	for _, triggerType := range expectedTriggers {
		if _, ok := factories[triggerType]; !ok {
			t.Errorf("missing trigger factory: %s", triggerType)
		}
	}

	if len(factories) != len(expectedTriggers) {
		t.Errorf("expected %d trigger factories, got %d", len(expectedTriggers), len(factories))
	}

	// Verify trigger factories return non-nil values.
	for triggerType, factory := range factories {
		trigger := factory()
		if trigger == nil {
			t.Errorf("trigger factory %q returned nil", triggerType)
		}
	}
}

func TestWorkflowHandlers(t *testing.T) {
	p := New()
	handlers := p.WorkflowHandlers()

	if _, ok := handlers["messaging"]; !ok {
		t.Fatal("missing workflow handler: messaging")
	}

	if len(handlers) != 1 {
		t.Errorf("expected 1 workflow handler, got %d", len(handlers))
	}

	handler := handlers["messaging"]()
	if handler == nil {
		t.Fatal("messaging workflow handler factory returned nil")
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()

	expectedTypes := map[string]bool{
		"messaging.broker":          true,
		"messaging.broker.eventbus": true,
		"messaging.handler":         true,
		"messaging.nats":            true,
		"messaging.kafka":           true,
		"notification.slack":        true,
		"webhook.sender":            true,
	}

	if len(schemas) != len(expectedTypes) {
		t.Errorf("expected %d schemas, got %d", len(expectedTypes), len(schemas))
	}

	for _, s := range schemas {
		if !expectedTypes[s.Type] {
			t.Errorf("unexpected schema type: %s", s.Type)
		}
		if s.Label == "" {
			t.Errorf("schema %s has empty label", s.Type)
		}
		if s.Category == "" {
			t.Errorf("schema %s has empty category", s.Type)
		}
	}
}

func TestPluginLoaderIntegration(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	// Verify all module factories were loaded
	moduleFactories := loader.ModuleFactories()
	expectedModuleCount := 7
	if len(moduleFactories) != expectedModuleCount {
		t.Errorf("expected %d module factories after load, got %d", expectedModuleCount, len(moduleFactories))
	}
}

func TestManifestModuleTypes(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	expectedModuleTypes := []string{
		"messaging.broker",
		"messaging.broker.eventbus",
		"messaging.handler",
		"messaging.nats",
		"messaging.kafka",
		"notification.slack",
		"webhook.sender",
	}

	if len(m.ModuleTypes) != len(expectedModuleTypes) {
		t.Errorf("manifest has %d module types, expected %d", len(m.ModuleTypes), len(expectedModuleTypes))
	}

	typeSet := make(map[string]bool)
	for _, mt := range m.ModuleTypes {
		typeSet[mt] = true
	}

	for _, expected := range expectedModuleTypes {
		if !typeSet[expected] {
			t.Errorf("manifest missing module type: %s", expected)
		}
	}
}

func TestManifestTriggerTypes(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if len(m.TriggerTypes) != 2 {
		t.Errorf("expected 2 trigger types, got %d", len(m.TriggerTypes))
	}
}

func TestManifestWorkflowTypes(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if len(m.WorkflowTypes) != 1 {
		t.Errorf("expected 1 workflow type, got %d", len(m.WorkflowTypes))
	}
	if len(m.WorkflowTypes) > 0 && m.WorkflowTypes[0] != "messaging" {
		t.Errorf("expected workflow type 'messaging', got %q", m.WorkflowTypes[0])
	}
}
