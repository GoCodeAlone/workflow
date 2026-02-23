package api

import (
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

func TestPluginImplementsEnginePlugin(t *testing.T) {
	p := New()
	var _ plugin.EnginePlugin = p
}

func TestPluginManifest(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	if m.Name != "api" {
		t.Errorf("expected name %q, got %q", "api", m.Name)
	}
	if len(m.ModuleTypes) != 7 {
		t.Errorf("expected 7 module types, got %d", len(m.ModuleTypes))
	}
}

func TestPluginCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(caps))
	}
	names := map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
	}
	for _, expected := range []string{"rest-api", "cqrs", "api-gateway"} {
		if !names[expected] {
			t.Errorf("missing capability %q", expected)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedTypes := []string{
		"api.query", "api.command", "api.handler",
		"api.gateway", "workflow.registry", "data.transformer",
		"processing.step",
	}
	for _, typ := range expectedTypes {
		factory, ok := factories[typ]
		if !ok {
			t.Errorf("missing factory for %q", typ)
			continue
		}
		mod := factory("test-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("factory for %q returned nil", typ)
		}
	}
}

func TestAPIQueryFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["api.query"]("query-test", map[string]any{
		"delegate": "my-delegate",
	})
	if mod == nil {
		t.Fatal("api.query factory returned nil with config")
	}
}

func TestAPICommandFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["api.command"]("cmd-test", map[string]any{
		"delegate": "my-delegate",
	})
	if mod == nil {
		t.Fatal("api.command factory returned nil with config")
	}
}

func TestAPIHandlerFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["api.handler"]("handler-test", map[string]any{
		"resourceName":       "orders",
		"workflowType":       "order-processing",
		"workflowEngine":     "sm-engine",
		"initialTransition":  "submit",
		"seedFile":           "data/orders.json",
		"sourceResourceName": "all-orders",
		"stateFilter":        "active",
		"fieldMapping": map[string]any{
			"id":     "order_id",
			"status": "state",
		},
		"transitionMap": map[string]any{
			"approve": "approved",
			"reject":  "rejected",
		},
		"summaryFields": []any{"id", "status", "name"},
	})
	if mod == nil {
		t.Fatal("api.handler factory returned nil with full config")
	}
}

func TestAPIGatewayFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["api.gateway"]("gw-test", map[string]any{
		"routes": []any{
			map[string]any{
				"pathPrefix":  "/api/v1",
				"backend":     "http://localhost:3000",
				"stripPrefix": true,
				"auth":        true,
				"timeout":     "30s",
				"methods":     []any{"GET", "POST"},
				"rateLimit": map[string]any{
					"requestsPerMinute": float64(100),
					"burstSize":         float64(20),
				},
			},
		},
		"globalRateLimit": map[string]any{
			"requestsPerMinute": float64(1000),
			"burstSize":         float64(100),
		},
		"cors": map[string]any{
			"allowOrigins": []any{"*"},
			"allowMethods": []any{"GET", "POST"},
			"allowHeaders": []any{"Authorization"},
			"maxAge":       float64(3600),
		},
		"auth": map[string]any{
			"type":   "bearer",
			"header": "Authorization",
		},
	})
	if mod == nil {
		t.Fatal("api.gateway factory returned nil with full config")
	}
}

func TestWorkflowRegistryFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["workflow.registry"]("registry-test", map[string]any{
		"storageBackend": "admin-db",
	})
	if mod == nil {
		t.Fatal("workflow.registry factory returned nil with config")
	}
}

func TestProcessingStepFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["processing.step"]("step-test", map[string]any{
		"componentId":          "my-processor",
		"successTransition":    "completed",
		"compensateTransition": "failed",
		"maxRetries":           3,
		"retryBackoffMs":       2000,
		"timeoutSeconds":       60,
	})
	if mod == nil {
		t.Fatal("processing.step factory returned nil with config")
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) != 7 {
		t.Fatalf("expected 7 module schemas, got %d", len(schemas))
	}

	types := map[string]bool{}
	for _, s := range schemas {
		types[s.Type] = true
	}
	expectedTypes := []string{
		"api.query", "api.command", "api.handler",
		"api.gateway", "workflow.registry", "data.transformer",
		"processing.step",
	}
	for _, expected := range expectedTypes {
		if !types[expected] {
			t.Errorf("missing schema for %q", expected)
		}
	}
}

func TestHelperFunctions(t *testing.T) {
	cfg := map[string]any{
		"str":    "hello",
		"intVal": 42,
		"fltVal": float64(3.14),
	}

	if v := getStringConfig(cfg, "str", "default"); v != "hello" {
		t.Errorf("expected %q, got %q", "hello", v)
	}
	if v := getStringConfig(cfg, "missing", "default"); v != "default" {
		t.Errorf("expected %q, got %q", "default", v)
	}
	if v := getIntConfig(cfg, "intVal", 0); v != 42 {
		t.Errorf("expected %d, got %d", 42, v)
	}
	if v := getIntConfig(cfg, "fltVal", 0); v != 3 {
		t.Errorf("expected %d, got %d", 3, v)
	}
	if v := getIntConfig(cfg, "missing", 99); v != 99 {
		t.Errorf("expected %d, got %d", 99, v)
	}
}

// --- injectable constructor tests ---

// stubModule is a minimal modular.Module used to verify that the injected
// constructor is called instead of the default concrete constructor.
type stubModule struct {
	name string
}

func (s *stubModule) Name() string                                  { return s.name }
func (s *stubModule) Dependencies() []string                        { return nil }
func (s *stubModule) ProvidesServices() []modular.ServiceProvider   { return nil }
func (s *stubModule) RequiresServices() []modular.ServiceDependency { return nil }
func (s *stubModule) RegisterConfig(_ modular.Application) error    { return nil }
func (s *stubModule) Init(_ modular.Application) error              { return nil }
func (s *stubModule) Start(_ modular.Application) error             { return nil }
func (s *stubModule) Stop(_ modular.Application) error              { return nil }

func TestInjectableQueryHandlerCtor(t *testing.T) {
	called := false
	p := New().WithQueryHandlerCtor(func(name string) modular.Module {
		called = true
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["api.query"]("test-q", map[string]any{})
	if !called {
		t.Fatal("injected QueryHandlerCtor was not called")
	}
	if mod == nil {
		t.Fatal("factory returned nil")
	}
}

func TestInjectableCommandHandlerCtor(t *testing.T) {
	called := false
	p := New().WithCommandHandlerCtor(func(name string) modular.Module {
		called = true
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["api.command"]("test-cmd", map[string]any{})
	if !called {
		t.Fatal("injected CommandHandlerCtor was not called")
	}
	if mod == nil {
		t.Fatal("factory returned nil")
	}
}

func TestInjectableRESTAPIHandlerCtor(t *testing.T) {
	var gotName, gotResource string
	p := New().WithRESTAPIHandlerCtor(func(name, resourceName string) modular.Module {
		gotName = name
		gotResource = resourceName
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["api.handler"]("test-h", map[string]any{"resourceName": "orders"})
	if mod == nil {
		t.Fatal("factory returned nil")
	}
	if gotName != "test-h" {
		t.Errorf("expected name %q, got %q", "test-h", gotName)
	}
	if gotResource != "orders" {
		t.Errorf("expected resourceName %q, got %q", "orders", gotResource)
	}
}

func TestInjectableAPIGatewayCtor(t *testing.T) {
	called := false
	p := New().WithAPIGatewayCtor(func(name string) modular.Module {
		called = true
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["api.gateway"]("test-gw", map[string]any{})
	if !called {
		t.Fatal("injected APIGatewayCtor was not called")
	}
	if mod == nil {
		t.Fatal("factory returned nil")
	}
}

func TestInjectableWorkflowRegistryCtor(t *testing.T) {
	var gotBackend string
	p := New().WithWorkflowRegistryCtor(func(name, storageBackend string) modular.Module {
		gotBackend = storageBackend
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["workflow.registry"]("test-reg", map[string]any{"storageBackend": "my-db"})
	if mod == nil {
		t.Fatal("factory returned nil")
	}
	if gotBackend != "my-db" {
		t.Errorf("expected storageBackend %q, got %q", "my-db", gotBackend)
	}
}

func TestInjectableDataTransformerCtor(t *testing.T) {
	called := false
	p := New().WithDataTransformerCtor(func(name string) modular.Module {
		called = true
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["data.transformer"]("test-dt", map[string]any{})
	if !called {
		t.Fatal("injected DataTransformerCtor was not called")
	}
	if mod == nil {
		t.Fatal("factory returned nil")
	}
}

func TestInjectableProcessingStepCtor(t *testing.T) {
	var gotConfig module.ProcessingStepConfig
	p := New().WithProcessingStepCtor(func(name string, cfg module.ProcessingStepConfig) modular.Module {
		gotConfig = cfg
		return &stubModule{name: name}
	})
	factories := p.ModuleFactories()
	mod := factories["processing.step"]("test-ps", map[string]any{
		"componentId":          "my-comp",
		"successTransition":    "done",
		"compensateTransition": "failed",
		"maxRetries":           5,
		"retryBackoffMs":       500,
		"timeoutSeconds":       10,
	})
	if mod == nil {
		t.Fatal("factory returned nil")
	}
	if gotConfig.ComponentID != "my-comp" {
		t.Errorf("expected ComponentID %q, got %q", "my-comp", gotConfig.ComponentID)
	}
	if gotConfig.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", gotConfig.MaxRetries)
	}
}

// TestDefaultCtorsUnchanged verifies that New() without any injected constructors
// produces the same module types as before this refactor.
func TestDefaultCtorsUnchanged(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	for _, typ := range []string{
		"api.query", "api.command", "api.handler",
		"api.gateway", "workflow.registry", "data.transformer",
		"processing.step",
	} {
		mod := factories[typ]("default-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("default factory for %q returned nil", typ)
		}
	}
}

// TestWithCtorChainingReturnsSelf verifies that the With* setter methods
// return the Plugin pointer, enabling method chaining.
func TestWithCtorChainingReturnsSelf(t *testing.T) {
	p := New()
	p2 := p.
		WithQueryHandlerCtor(func(name string) modular.Module { return &stubModule{name: name} }).
		WithCommandHandlerCtor(func(name string) modular.Module { return &stubModule{name: name} }).
		WithRESTAPIHandlerCtor(func(name, _ string) modular.Module { return &stubModule{name: name} }).
		WithAPIGatewayCtor(func(name string) modular.Module { return &stubModule{name: name} }).
		WithWorkflowRegistryCtor(func(name, _ string) modular.Module { return &stubModule{name: name} }).
		WithDataTransformerCtor(func(name string) modular.Module { return &stubModule{name: name} }).
		WithProcessingStepCtor(func(name string, _ module.ProcessingStepConfig) modular.Module { return &stubModule{name: name} })
	if p2 != p {
		t.Error("expected chained With* calls to return the same *Plugin")
	}
}
