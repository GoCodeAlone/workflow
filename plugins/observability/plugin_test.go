package observability

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.Name() != "observability" {
		t.Errorf("Name() = %q, want %q", p.Name(), "observability")
	}
	if p.Version() != "1.0.0" {
		t.Errorf("Version() = %q, want %q", p.Version(), "1.0.0")
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestInterfaceSatisfaction(t *testing.T) {
	var _ plugin.EnginePlugin = New()
}

func TestManifestValidation(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	if m.Name != "observability" {
		t.Errorf("manifest Name = %q, want %q", m.Name, "observability")
	}
	if len(m.ModuleTypes) != 6 {
		t.Errorf("manifest ModuleTypes count = %d, want 6", len(m.ModuleTypes))
	}
}

func TestCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 5 {
		t.Fatalf("Capabilities() count = %d, want 5", len(caps))
	}

	expected := map[string]bool{
		"metrics":      false,
		"health-check": false,
		"logging":      false,
		"tracing":      false,
		"openapi":      false,
	}
	for _, c := range caps {
		if _, ok := expected[c.Name]; !ok {
			t.Errorf("unexpected capability %q", c.Name)
		} else {
			expected[c.Name] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing capability %q", name)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedTypes := []string{
		"metrics.collector",
		"health.checker",
		"log.collector",
		"observability.otel",
		"openapi.generator",
		"http.middleware.otel",
	}

	if len(factories) != len(expectedTypes) {
		t.Fatalf("ModuleFactories() count = %d, want %d", len(factories), len(expectedTypes))
	}

	for _, typ := range expectedTypes {
		factory, ok := factories[typ]
		if !ok {
			t.Errorf("missing factory for module type %q", typ)
			continue
		}
		mod := factory("test-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("factory for %q returned nil", typ)
			continue
		}
		if mod.Name() != "test-"+typ {
			t.Errorf("factory for %q: Name() = %q, want %q", typ, mod.Name(), "test-"+typ)
		}
	}
}

func TestModuleFactoriesWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	t.Run("metrics.collector with config", func(t *testing.T) {
		mod := factories["metrics.collector"]("mc", map[string]any{
			"namespace":      "testns",
			"subsystem":      "testsub",
			"metricsPath":    "/test-metrics",
			"enabledMetrics": []any{"workflow"},
		})
		if mod == nil {
			t.Fatal("factory returned nil")
		}
	})

	t.Run("health.checker with config", func(t *testing.T) {
		mod := factories["health.checker"]("hc", map[string]any{
			"healthPath":   "/test-health",
			"readyPath":    "/test-ready",
			"livePath":     "/test-live",
			"checkTimeout": "10s",
			"autoDiscover": false,
		})
		if mod == nil {
			t.Fatal("factory returned nil")
		}
	})

	t.Run("log.collector with config", func(t *testing.T) {
		mod := factories["log.collector"]("lc", map[string]any{
			"logLevel":      "debug",
			"outputFormat":  "text",
			"retentionDays": 14,
		})
		if mod == nil {
			t.Fatal("factory returned nil")
		}
	})

	t.Run("openapi.generator with config", func(t *testing.T) {
		mod := factories["openapi.generator"]("og", map[string]any{
			"title":       "Test API",
			"version":     "2.0.0",
			"description": "Test description",
			"servers":     []any{"http://localhost:9090"},
		})
		if mod == nil {
			t.Fatal("factory returned nil")
		}
	})

}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()

	expectedTypes := map[string]bool{
		"metrics.collector":  false,
		"health.checker":     false,
		"log.collector":      false,
		"observability.otel": false,
		"openapi.generator":  false,
		"http.middleware.otel": false,
	}

	if len(schemas) != len(expectedTypes) {
		t.Fatalf("ModuleSchemas() count = %d, want %d", len(schemas), len(expectedTypes))
	}

	for _, s := range schemas {
		if _, ok := expectedTypes[s.Type]; !ok {
			t.Errorf("unexpected schema type %q", s.Type)
		} else {
			expectedTypes[s.Type] = true
		}

		if s.Label == "" {
			t.Errorf("schema %q has empty Label", s.Type)
		}
		if s.Category == "" {
			t.Errorf("schema %q has empty Category", s.Type)
		}
		if s.Description == "" {
			t.Errorf("schema %q has empty Description", s.Type)
		}
	}

	for typ, found := range expectedTypes {
		if !found {
			t.Errorf("missing schema for type %q", typ)
		}
	}
}

func TestModuleSchemasValidFields(t *testing.T) {
	p := New()
	for _, s := range p.ModuleSchemas() {
		t.Run(s.Type, func(t *testing.T) {
			for _, f := range s.ConfigFields {
				if f.Key == "" {
					t.Errorf("field has empty Key")
				}
				if f.Label == "" {
					t.Errorf("field %q has empty Label", f.Key)
				}
				if f.Type == "" {
					t.Errorf("field %q has empty Type", f.Key)
				}
				// Validate field types are known
				validTypes := map[schema.ConfigFieldType]bool{
					schema.FieldTypeString:   true,
					schema.FieldTypeNumber:   true,
					schema.FieldTypeBool:     true,
					schema.FieldTypeSelect:   true,
					schema.FieldTypeJSON:     true,
					schema.FieldTypeDuration: true,
					schema.FieldTypeArray:    true,
					schema.FieldTypeMap:      true,
					schema.FieldTypeFilePath: true,
				}
				if !validTypes[f.Type] {
					t.Errorf("field %q has unknown type %q", f.Key, f.Type)
				}
			}
		})
	}
}

func TestWiringHooks(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()

	if len(hooks) != 5 {
		t.Fatalf("WiringHooks() count = %d, want 5", len(hooks))
	}

	expectedNames := map[string]bool{
		"observability.otel-middleware":   false,
		"observability.health-endpoints":  false,
		"observability.metrics-endpoint":  false,
		"observability.log-endpoint":      false,
		"observability.openapi-endpoints": false,
	}

	for _, h := range hooks {
		if _, ok := expectedNames[h.Name]; !ok {
			t.Errorf("unexpected wiring hook %q", h.Name)
		} else {
			expectedNames[h.Name] = true
		}
		if h.Hook == nil {
			t.Errorf("wiring hook %q has nil Hook function", h.Name)
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("missing wiring hook %q", name)
		}
	}
}

func TestStepFactoriesEmpty(t *testing.T) {
	p := New()
	steps := p.StepFactories()
	if steps != nil {
		t.Errorf("StepFactories() should return nil, got %v", steps)
	}
}

func TestTriggerFactoriesEmpty(t *testing.T) {
	p := New()
	triggers := p.TriggerFactories()
	if triggers != nil {
		t.Errorf("TriggerFactories() should return nil, got %v", triggers)
	}
}

func TestWorkflowHandlersEmpty(t *testing.T) {
	p := New()
	handlers := p.WorkflowHandlers()
	if handlers != nil {
		t.Errorf("WorkflowHandlers() should return nil, got %v", handlers)
	}
}

func TestModuleTypeCoverage(t *testing.T) {
	p := New()
	manifest := p.EngineManifest()

	factories := p.ModuleFactories()
	schemas := p.ModuleSchemas()

	schemaTypes := make(map[string]bool)
	for _, s := range schemas {
		schemaTypes[s.Type] = true
	}

	// Every module type in the manifest must have a factory and a schema.
	for _, typ := range manifest.ModuleTypes {
		if _, ok := factories[typ]; !ok {
			t.Errorf("manifest declares module type %q but no factory exists", typ)
		}
		if !schemaTypes[typ] {
			t.Errorf("manifest declares module type %q but no schema exists", typ)
		}
	}

	// Every factory must be declared in the manifest.
	manifestTypes := make(map[string]bool)
	for _, typ := range manifest.ModuleTypes {
		manifestTypes[typ] = true
	}
	for typ := range factories {
		if !manifestTypes[typ] {
			t.Errorf("factory for %q exists but not declared in manifest", typ)
		}
	}
}
