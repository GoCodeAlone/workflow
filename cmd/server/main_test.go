package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/ai"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/module"
)

// mockGenerator implements ai.WorkflowGenerator for testing.
type mockGenerator struct{}

func (m *mockGenerator) GenerateWorkflow(_ context.Context, _ ai.GenerateRequest) (*ai.GenerateResponse, error) {
	return &ai.GenerateResponse{
		Workflow: &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "test-server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
			},
			Workflows: map[string]any{},
		},
		Explanation: "test workflow",
	}, nil
}

func (m *mockGenerator) GenerateComponent(_ context.Context, _ ai.ComponentSpec) (string, error) {
	return "package module\n\ntype TestComponent struct{}", nil
}

func (m *mockGenerator) SuggestWorkflow(_ context.Context, _ string) ([]ai.WorkflowSuggestion, error) {
	return []ai.WorkflowSuggestion{{Name: "test", Description: "test", Confidence: 0.9}}, nil
}

func (m *mockGenerator) IdentifyMissingComponents(_ context.Context, _ *config.WorkflowConfig) ([]ai.ComponentSpec, error) {
	return nil, nil
}

func TestInitAIService_NoProviders(t *testing.T) {
	// Ensure no env key is set
	t.Setenv("ANTHROPIC_API_KEY", "")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()

	// Reset flags for this test
	*anthropicKey = ""
	*copilotCLI = ""

	svc, deploy := initAIService(logger, registry, pool)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if deploy == nil {
		t.Fatal("expected non-nil deploy service")
	}

	providers := svc.Providers()
	if len(providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(providers))
	}
}

func TestInitAIService_AnthropicOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()

	*anthropicKey = ""
	*copilotCLI = ""

	svc, _ := initAIService(logger, registry, pool)

	providers := svc.Providers()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0] != ai.ProviderAnthropic {
		t.Errorf("expected anthropic provider, got %s", providers[0])
	}
}

func TestMuxRoutesRegistered(t *testing.T) {
	// Create AI service with mock generator
	svc := ai.NewService()
	mock := &mockGenerator{}
	svc.RegisterGenerator(ai.ProviderAnthropic, mock)

	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	deploy := ai.NewDeployService(svc, registry, pool)
	cfg := config.NewEmptyWorkflowConfig()

	mux := http.NewServeMux()
	ai.NewHandler(svc).RegisterRoutes(mux)
	ai.NewDeployHandler(deploy).RegisterRoutes(mux)
	dynamic.NewAPIHandler(loader, registry).RegisterRoutes(mux)
	module.NewWorkflowUIHandler(cfg).RegisterRoutes(mux)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"ai generate", http.MethodPost, "/api/ai/generate", ai.GenerateRequest{Intent: "test"}},
		{"ai suggest", http.MethodPost, "/api/ai/suggest", map[string]string{"useCase": "test"}},
		{"ai providers", http.MethodGet, "/api/ai/providers", nil},
		{"workflow modules", http.MethodGet, "/api/workflow/modules", nil},
		{"workflow validate", http.MethodPost, "/api/workflow/validate", config.WorkflowConfig{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != nil {
				body, _ := json.Marshal(tt.body)
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("route %s %s returned 404", tt.method, tt.path)
			}
		})
	}
}

func TestEndToEnd_MockProvider(t *testing.T) {
	svc := ai.NewService()
	mock := &mockGenerator{}
	svc.RegisterGenerator(ai.ProviderAnthropic, mock)

	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	deploy := ai.NewDeployService(svc, registry, pool)

	mux := http.NewServeMux()
	ai.NewHandler(svc).RegisterRoutes(mux)
	ai.NewDeployHandler(deploy).RegisterRoutes(mux)

	body, _ := json.Marshal(ai.GenerateRequest{Intent: "Create a simple HTTP server"})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ai.GenerateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Workflow == nil {
		t.Error("expected workflow in response")
	}
	if len(resp.Workflow.Modules) == 0 {
		t.Error("expected at least one module in workflow")
	}
}

func TestBuildEngine_WithConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "web-server", Type: "http.server", Config: map[string]any{"address": ":9090"}},
			{Name: "web-router", Type: "http.router", Config: map[string]any{"prefix": "/api"}, DependsOn: []string{"web-server"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	engine, loader, registry, err := buildEngine(cfg, logger)
	if err != nil {
		t.Fatalf("buildEngine failed: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if loader == nil {
		t.Fatal("expected non-nil loader")
	}
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestBuildEngine_EmptyConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := config.NewEmptyWorkflowConfig()

	engine, loader, registry, err := buildEngine(cfg, logger)
	if err != nil {
		t.Fatalf("buildEngine with empty config failed: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if loader == nil {
		t.Fatal("expected non-nil loader")
	}
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestCQRSWiring_QueryHandlerDelegateDispatches(t *testing.T) {
	// Test that a QueryHandler correctly dispatches to its delegate
	qh := module.NewQueryHandler("test-queries")
	called := false
	qh.SetDelegateHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/engine/config", nil)
	w := httptest.NewRecorder()
	qh.ServeHTTP(w, req)

	if !called {
		t.Error("expected delegate to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCQRSWiring_CommandHandlerDelegateDispatches(t *testing.T) {
	// Test that a CommandHandler correctly dispatches to its delegate
	ch := module.NewCommandHandler("test-commands")
	ch.SetDelegateHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"reloaded":true}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/engine/reload", nil)
	w := httptest.NewRecorder()
	ch.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestInitAIService_CopilotFailure(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()

	*anthropicKey = ""
	// NewClient accepts any path without validation, so the provider registers
	// successfully. The failure will occur later at generation time.
	*copilotCLI = "/nonexistent/path/to/copilot-cli-binary"
	defer func() { *copilotCLI = "" }()

	svc, deploy := initAIService(logger, registry, pool)
	if svc == nil {
		t.Fatal("expected non-nil service even with invalid copilot path")
	}
	if deploy == nil {
		t.Fatal("expected non-nil deploy service even with invalid copilot path")
	}

	// Copilot client is created successfully (path is validated at call time, not creation),
	// so we should have 1 provider (copilot) registered.
	providers := svc.Providers()
	if len(providers) != 1 {
		t.Errorf("expected 1 provider (copilot), got %d", len(providers))
	}
}

func TestIntegration_GenerateEndpoint(t *testing.T) {
	svc := ai.NewService()
	svc.RegisterGenerator(ai.ProviderAnthropic, &mockGenerator{})

	// Test using the AI handler directly (routes are wired through engine CQRS modules in production)
	handler := ai.NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body, _ := json.Marshal(ai.GenerateRequest{Intent: "Create a REST API"})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ai.GenerateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Workflow == nil {
		t.Error("expected workflow in response")
	}
	if resp.Explanation == "" {
		t.Error("expected non-empty explanation")
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	*configFile = ""
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := loadConfig(logger)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Modules) != 0 {
		t.Errorf("expected empty modules, got %d", len(cfg.Modules))
	}
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	*configFile = "/nonexistent/config.yaml"
	defer func() { *configFile = "" }()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	_, err := loadConfig(logger)
	if err == nil {
		t.Fatal("expected error for nonexistent config file")
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	// Create a temp YAML config file
	tmpFile, err := os.CreateTemp("", "workflow-test-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	yamlContent := `modules:
  - name: test-server
    type: http.server
    config:
      address: ":9999"
workflows: {}
triggers: {}
`
	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	*configFile = tmpFile.Name()
	defer func() { *configFile = "" }()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := loadConfig(logger)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if len(cfg.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "test-server" {
		t.Errorf("expected module name test-server, got %s", cfg.Modules[0].Name)
	}
}

func TestSetup_EmptyConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	*anthropicKey = ""
	*copilotCLI = ""

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := config.NewEmptyWorkflowConfig()

	app, err := setup(logger, cfg)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil serverApp")
	}
	if app.engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if app.logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestRun_ImmediateCancel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	*anthropicKey = ""
	*copilotCLI = ""

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := config.NewEmptyWorkflowConfig()

	app, err := setup(logger, cfg)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Create a context and cancel it immediately so run() exits quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = run(ctx, app, ":0")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
}

func TestRun_ServerStartsAndStops(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	*anthropicKey = ""
	*copilotCLI = ""

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := config.NewEmptyWorkflowConfig()

	app, err := setup(logger, cfg)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, app, ":0")
	}()

	// Give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel and wait for shutdown
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run failed: %v", err)
	}
}

func TestInitAIService_BothProviders(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-both")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()

	*anthropicKey = ""
	*copilotCLI = "/some/copilot/path"
	defer func() { *copilotCLI = "" }()

	svc, deploy := initAIService(logger, registry, pool)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if deploy == nil {
		t.Fatal("expected non-nil deploy service")
	}

	providers := svc.Providers()
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
}

func TestInitAIService_AnthropicViaFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()

	*anthropicKey = "flag-key-123"
	*copilotCLI = ""
	defer func() { *anthropicKey = "" }()

	svc, _ := initAIService(logger, registry, pool)

	providers := svc.Providers()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0] != ai.ProviderAnthropic {
		t.Errorf("expected anthropic provider, got %s", providers[0])
	}
}

func TestSetup_EngineError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	*anthropicKey = ""
	*copilotCLI = ""

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "bad", Type: "nonexistent.type"},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	_, err := setup(logger, cfg)
	if err == nil {
		t.Fatal("expected error for invalid module type in setup")
	}
}

func TestBuildEngine_InvalidModuleType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "bad-module", Type: "nonexistent.type.that.does.not.exist"},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	_, _, _, err := buildEngine(cfg, logger)
	if err == nil {
		t.Fatal("expected error for invalid module type")
	}
}

func TestSetup_WithModules(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	*anthropicKey = ""
	*copilotCLI = ""

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server", Config: map[string]any{"address": ":7070"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	app, err := setup(logger, cfg)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil serverApp")
	}
	if app.engine == nil {
		t.Fatal("expected non-nil engine")
	}
}
