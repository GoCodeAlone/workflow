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
				{Name: "test-server", Type: "http.server", Config: map[string]interface{}{"address": ":8080"}},
			},
			Workflows: map[string]interface{}{},
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
		body   interface{}
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
