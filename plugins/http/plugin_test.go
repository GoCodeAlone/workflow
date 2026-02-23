package http

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.Name() != "workflow-plugin-http" {
		t.Errorf("Name() = %q, want %q", p.Name(), "workflow-plugin-http")
	}
	if p.Version() != "1.0.0" {
		t.Errorf("Version() = %q, want %q", p.Version(), "1.0.0")
	}
}

func TestImplementsEnginePlugin(t *testing.T) {
	var _ plugin.EnginePlugin = New()
}

func TestCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) == 0 {
		t.Fatal("Capabilities() returned empty slice")
	}

	expected := map[string]bool{
		"http-server":     false,
		"http-router":     false,
		"http-handler":    false,
		"http-middleware": false,
		"http-proxy":      false,
		"static-files":    false,
	}

	for _, c := range caps {
		if _, ok := expected[c.Name]; ok {
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
		"http.server",
		"http.router",
		"http.handler",
		"http.proxy",
		"reverseproxy",
		"http.simple_proxy",
		"static.fileserver",
		"http.middleware.auth",
		"http.middleware.logging",
		"http.middleware.ratelimit",
		"http.middleware.cors",
		"http.middleware.requestid",
		"http.middleware.securityheaders",
	}

	for _, mt := range expectedTypes {
		if _, ok := factories[mt]; !ok {
			t.Errorf("missing module factory for type %q", mt)
		}
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	steps := p.StepFactories()

	expectedSteps := []string{
		"step.rate_limit",
		"step.circuit_breaker",
	}

	for _, st := range expectedSteps {
		if _, ok := steps[st]; !ok {
			t.Errorf("missing step factory for %q", st)
		}
	}
}

func TestTriggerFactories(t *testing.T) {
	p := New()
	triggers := p.TriggerFactories()
	if _, ok := triggers["http"]; !ok {
		t.Error("missing trigger factory for http")
	}
}

func TestWorkflowHandlers(t *testing.T) {
	p := New()
	handlers := p.WorkflowHandlers()
	if _, ok := handlers["http"]; !ok {
		t.Error("missing workflow handler factory for http")
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) == 0 {
		t.Fatal("ModuleSchemas() returned empty slice")
	}

	schemaTypes := make(map[string]bool)
	for _, s := range schemas {
		schemaTypes[s.Type] = true
	}

	expectedTypes := []string{
		"http.server",
		"http.router",
		"http.handler",
		"http.proxy",
		"reverseproxy",
		"http.simple_proxy",
		"static.fileserver",
		"http.middleware.auth",
		"http.middleware.logging",
		"http.middleware.ratelimit",
		"http.middleware.cors",
		"http.middleware.requestid",
		"http.middleware.securityheaders",
	}

	for _, et := range expectedTypes {
		if !schemaTypes[et] {
			t.Errorf("missing schema for type %q", et)
		}
	}
}

func TestWiringHooks(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()
	if len(hooks) < 6 {
		t.Errorf("WiringHooks() returned %d hooks, want >= 6", len(hooks))
	}

	hookNames := make(map[string]bool)
	for _, h := range hooks {
		hookNames[h.Name] = true
	}

	expectedHooks := []string{
		"http-auth-provider-wiring",
		"http-static-fileserver-registration",
		"http-health-endpoint-registration",
		"http-metrics-endpoint-registration",
		"http-log-endpoint-registration",
		"http-openapi-endpoint-registration",
	}

	for _, name := range expectedHooks {
		if !hookNames[name] {
			t.Errorf("missing wiring hook %q", name)
		}
	}
}

func TestEngineManifest(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if m == nil {
		t.Fatal("EngineManifest() returned nil")
	}
	if m.Name != "workflow-plugin-http" {
		t.Errorf("manifest.Name = %q, want %q", m.Name, "workflow-plugin-http")
	}
	if len(m.ModuleTypes) != 13 {
		t.Errorf("manifest has %d module types, want 13", len(m.ModuleTypes))
	}
	if len(m.StepTypes) != 2 {
		t.Errorf("manifest has %d step types, want 2", len(m.StepTypes))
	}
	if len(m.TriggerTypes) != 1 {
		t.Errorf("manifest has %d trigger types, want 1", len(m.TriggerTypes))
	}
	if len(m.WorkflowTypes) != 1 {
		t.Errorf("manifest has %d workflow types, want 1", len(m.WorkflowTypes))
	}
}

// TestModuleFactorySmoke creates a module from each factory and verifies basic properties.
func TestModuleFactorySmoke(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	tests := []struct {
		moduleType string
		config     map[string]any
	}{
		{"http.server", map[string]any{"address": ":9090"}},
		{"http.router", map[string]any{}},
		{"http.handler", map[string]any{"contentType": "text/plain"}},
		{"http.proxy", map[string]any{}},
		{"reverseproxy", map[string]any{}},
		{"http.simple_proxy", map[string]any{}},
		{"static.fileserver", map[string]any{"root": "/tmp/test"}},
		{"http.middleware.auth", map[string]any{"authType": "Bearer"}},
		{"http.middleware.logging", map[string]any{"logLevel": "debug"}},
		{"http.middleware.ratelimit", map[string]any{"requestsPerMinute": 100, "burstSize": 20}},
		{"http.middleware.cors", map[string]any{}},
		{"http.middleware.requestid", map[string]any{}},
		{"http.middleware.securityheaders", map[string]any{"frameOptions": "SAMEORIGIN"}},
	}

	for _, tt := range tests {
		t.Run(tt.moduleType, func(t *testing.T) {
			factory, ok := factories[tt.moduleType]
			if !ok {
				t.Fatalf("no factory for %q", tt.moduleType)
			}

			mod := factory("test-"+tt.moduleType, tt.config)
			if mod == nil {
				t.Fatalf("factory for %q returned nil module", tt.moduleType)
			}
			if mod.Name() == "" {
				t.Errorf("module for %q has empty name", tt.moduleType)
			}
		})
	}
}

func TestStepFactorySmoke(t *testing.T) {
	p := New()
	steps := p.StepFactories()

	tests := []struct {
		stepType string
		config   map[string]any
		wantName string
	}{
		{
			"step.rate_limit",
			map[string]any{"requests_per_minute": 100, "burst_size": 20},
			"test-rate-limit",
		},
		{
			"step.circuit_breaker",
			map[string]any{"failure_threshold": 5, "timeout": "30s"},
			"test-circuit-breaker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.stepType, func(t *testing.T) {
			factory, ok := steps[tt.stepType]
			if !ok {
				t.Fatalf("no step factory for %q", tt.stepType)
			}

			step, err := factory(tt.wantName, tt.config, nil)
			if err != nil {
				t.Fatalf("step factory error: %v", err)
			}
			if step == nil {
				t.Fatal("step factory returned nil")
			}

			ps, ok := step.(module.PipelineStep)
			if !ok {
				t.Fatal("step does not implement PipelineStep")
			}
			if ps.Name() != tt.wantName {
				t.Errorf("step.Name() = %q, want %q", ps.Name(), tt.wantName)
			}
		})
	}
}

func TestTriggerFactorySmoke(t *testing.T) {
	p := New()
	triggers := p.TriggerFactories()

	factory, ok := triggers["http"]
	if !ok {
		t.Fatal("no trigger factory for http")
	}

	trigger := factory()
	if trigger == nil {
		t.Fatal("trigger factory returned nil")
	}

	mt, ok := trigger.(module.Trigger)
	if !ok {
		t.Fatal("trigger does not implement module.Trigger")
	}
	if mt.Name() != module.HTTPTriggerName {
		t.Errorf("trigger.Name() = %q, want %q", mt.Name(), module.HTTPTriggerName)
	}
}

func TestWorkflowHandlerFactorySmoke(t *testing.T) {
	p := New()
	wfHandlers := p.WorkflowHandlers()

	factory, ok := wfHandlers["http"]
	if !ok {
		t.Fatal("no workflow handler factory for http")
	}

	handler := factory()
	if handler == nil {
		t.Fatal("workflow handler factory returned nil")
	}
}

func TestRateLimitMiddlewareFactory_RequestsPerHour(t *testing.T) {
	factories := moduleFactories()
	factory, ok := factories["http.middleware.ratelimit"]
	if !ok {
		t.Fatal("no factory for http.middleware.ratelimit")
	}

	// requestsPerHour as int
	mod := factory("auth-register-rl", map[string]any{
		"requestsPerHour": 5,
		"burstSize":       5,
	})
	if mod == nil {
		t.Fatal("factory returned nil for requestsPerHour config")
	}
	if mod.Name() != "auth-register-rl" {
		t.Errorf("expected name %q, got %q", "auth-register-rl", mod.Name())
	}

	// requestsPerHour as float64 (YAML unmarshals numbers as float64)
	mod2 := factory("auth-register-rl2", map[string]any{
		"requestsPerHour": float64(5),
		"burstSize":       float64(5),
	})
	if mod2 == nil {
		t.Fatal("factory returned nil for requestsPerHour float64 config")
	}
}

func TestRateLimitMiddlewareFactory_InvalidValues(t *testing.T) {
	factories := moduleFactories()
	factory, ok := factories["http.middleware.ratelimit"]
	if !ok {
		t.Fatal("no factory for http.middleware.ratelimit")
	}

	// Zero requestsPerHour must fall through to requestsPerMinute path (not crash).
	modZeroRPH := factory("rl-zero-rph", map[string]any{
		"requestsPerHour":    0,
		"requestsPerMinute":  30,
		"burstSize":          5,
	})
	if modZeroRPH == nil {
		t.Fatal("factory returned nil for zero requestsPerHour config")
	}

	// Negative requestsPerMinute must use default (60).
	modNegRPM := factory("rl-neg-rpm", map[string]any{
		"requestsPerMinute": -10,
	})
	if modNegRPM == nil {
		t.Fatal("factory returned nil for negative requestsPerMinute config")
	}

	// Zero burstSize must keep default (10).
	modZeroBurst := factory("rl-zero-burst", map[string]any{
		"requestsPerMinute": 60,
		"burstSize":         0,
	})
	if modZeroBurst == nil {
		t.Fatal("factory returned nil for zero burstSize config")
	}
}

func TestPluginLoaderIntegration(t *testing.T) {
	p := New()

	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)

	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin() error: %v", err)
	}

	// Verify factories were registered
	mf := loader.ModuleFactories()
	if len(mf) < 13 {
		t.Errorf("loader has %d module factories, want >= 13", len(mf))
	}

	sf := loader.StepFactories()
	if len(sf) < 2 {
		t.Errorf("loader has %d step factories, want >= 2", len(sf))
	}

	tf := loader.TriggerFactories()
	if len(tf) < 1 {
		t.Errorf("loader has %d trigger factories, want >= 1", len(tf))
	}

	wh := loader.WorkflowHandlerFactories()
	if len(wh) < 1 {
		t.Errorf("loader has %d workflow handler factories, want >= 1", len(wh))
	}

	hooks := loader.WiringHooks()
	if len(hooks) < 6 {
		t.Errorf("loader has %d wiring hooks, want >= 6", len(hooks))
	}
}
