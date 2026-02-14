package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
)

// Valid dynamic component source for testing.
const testDynamicSource = `package component

import (
	"context"
	"fmt"
)

func Name() string {
	return "test-dynamic"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing name")
	}
	return map[string]interface{}{
		"greeting": "hello " + name,
	}, nil
}
`

func newTestDeployService(mock *MockGenerator) (*DeployService, *dynamic.ComponentRegistry) {
	svc := NewService()
	svc.RegisterGenerator(ProviderAnthropic, mock)

	registry := dynamic.NewComponentRegistry()
	pool := dynamic.NewInterpreterPool()

	deploy := NewDeployService(svc, registry, pool)
	return deploy, registry
}

func TestDeployComponent_WithSource(t *testing.T) {
	mock := &MockGenerator{}
	deploy, registry := newTestDeployService(mock)

	spec := ComponentSpec{
		Name:   "greeter",
		Type:   "test.greeter",
		GoCode: testDynamicSource,
	}

	err := deploy.DeployComponent(context.Background(), spec)
	if err != nil {
		t.Fatalf("DeployComponent failed: %v", err)
	}

	// Verify the component is in the registry
	comp, ok := registry.Get("greeter")
	if !ok {
		t.Fatal("expected component in registry")
	}

	if comp.Name() != "test-dynamic" {
		t.Errorf("expected name %q, got %q", "test-dynamic", comp.Name())
	}

	// Verify it executes correctly
	result, err := comp.Execute(context.Background(), map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["greeting"] != "hello world" {
		t.Errorf("expected greeting=%q, got %q", "hello world", result["greeting"])
	}
}

func TestDeployComponent_WithGeneration(t *testing.T) {
	mock := &MockGenerator{
		GenerateComponentFn: func(ctx context.Context, spec ComponentSpec) (string, error) {
			return testDynamicSource, nil
		},
	}
	deploy, registry := newTestDeployService(mock)

	spec := ComponentSpec{
		Name:        "auto-generated",
		Type:        "test.auto",
		Description: "Auto-generated test component",
		// GoCode intentionally empty - should trigger generation
	}

	err := deploy.DeployComponent(context.Background(), spec)
	if err != nil {
		t.Fatalf("DeployComponent failed: %v", err)
	}

	if _, ok := registry.Get("auto-generated"); !ok {
		t.Error("expected component in registry after generation")
	}
}

func TestDeployComponent_BadPackage(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)

	spec := ComponentSpec{
		Name: "bad",
		GoCode: `package main

func Name() string { return "bad" }
`,
	}

	err := deploy.DeployComponent(context.Background(), spec)
	if err == nil {
		t.Error("expected error for non-component package")
	}
	if !strings.Contains(err.Error(), "package component") {
		t.Errorf("error should mention package component, got: %v", err)
	}
}

func TestDeployComponent_BlockedImport(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)

	spec := ComponentSpec{
		Name: "unsafe",
		GoCode: `package component

import "os/exec"

func Name() string { return "unsafe" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	_ = exec.Command("echo")
	return nil, nil
}
`,
	}

	err := deploy.DeployComponent(context.Background(), spec)
	if err == nil {
		t.Error("expected error for blocked import")
	}
}

func TestGenerateAndDeploy(t *testing.T) {
	mock := &MockGenerator{
		GenerateWorkflowFn: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
			return &GenerateResponse{
				Workflow: &config.WorkflowConfig{
					Modules: []config.ModuleConfig{
						{Name: "server", Type: "http.server"},
						{Name: "greeter", Type: "test.greeter"},
					},
					Workflows: map[string]any{},
					Triggers:  map[string]any{},
				},
				Components: []ComponentSpec{
					{
						Name:   "greeter",
						Type:   "test.greeter",
						GoCode: testDynamicSource,
					},
				},
				Explanation: "A simple greeter workflow",
			}, nil
		},
	}

	deploy, registry := newTestDeployService(mock)

	cfg, err := deploy.GenerateAndDeploy(context.Background(), "Create a greeter workflow")
	if err != nil {
		t.Fatalf("GenerateAndDeploy failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(cfg.Modules))
	}

	// Verify the component was deployed
	comp, ok := registry.Get("greeter")
	if !ok {
		t.Fatal("expected greeter component in registry")
	}

	result, err := comp.Execute(context.Background(), map[string]any{"name": "deploy"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["greeting"] != "hello deploy" {
		t.Errorf("expected greeting=%q, got %q", "hello deploy", result["greeting"])
	}
}

func TestGenerateAndDeploy_GenerationError(t *testing.T) {
	mock := &MockGenerator{
		GenerateWorkflowFn: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	deploy, _ := newTestDeployService(mock)

	_, err := deploy.GenerateAndDeploy(context.Background(), "fail")
	if err == nil {
		t.Error("expected error when generation fails")
	}
}

func TestSaveConfig(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")

	err := deploy.SaveConfig(cfg, path)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify the file was written
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "http.server") {
		t.Error("saved config missing module type")
	}
	if !strings.Contains(content, ":8080") {
		t.Error("saved config missing address")
	}
}

func TestSaveConfig_NilConfig(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)

	err := deploy.SaveConfig(nil, "/tmp/test.yaml")
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestSaveConfig_SubDirectory(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "workflow.yaml")

	err := deploy.SaveConfig(cfg, path)
	if err != nil {
		t.Fatalf("SaveConfig with subdirectory failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist")
	}
}

// --- Deploy API tests ---

func TestHandleDeploy_Valid(t *testing.T) {
	mock := &MockGenerator{
		GenerateWorkflowFn: func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
			return &GenerateResponse{
				Workflow: &config.WorkflowConfig{
					Modules: []config.ModuleConfig{
						{Name: "greeter", Type: "test.greeter"},
					},
					Workflows: map[string]any{},
					Triggers:  map[string]any{},
				},
				Components: []ComponentSpec{
					{
						Name:   "greeter",
						Type:   "test.greeter",
						GoCode: testDynamicSource,
					},
				},
			}, nil
		},
	}

	deploy, _ := newTestDeployService(mock)
	handler := NewDeployHandler(deploy)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body, _ := json.Marshal(deployRequest{Intent: "Create a greeter"})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/deploy", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "deployed" {
		t.Errorf("expected status=deployed, got %v", resp["status"])
	}
}

func TestHandleDeploy_EmptyIntent(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)
	handler := NewDeployHandler(deploy)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body, _ := json.Marshal(deployRequest{Intent: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/deploy", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDeploy_InvalidBody(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)
	handler := NewDeployHandler(deploy)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/deploy", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDeployComponent_Valid(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)
	handler := NewDeployHandler(deploy)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body, _ := json.Marshal(deployComponentRequest{
		Name:   "test-comp",
		Type:   "test.component",
		Source: testDynamicSource,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/deploy/component", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeployComponent_EmptyName(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)
	handler := NewDeployHandler(deploy)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body, _ := json.Marshal(deployComponentRequest{Source: testDynamicSource})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/deploy/component", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDeployComponent_BadSource(t *testing.T) {
	mock := &MockGenerator{}
	deploy, _ := newTestDeployService(mock)
	handler := NewDeployHandler(deploy)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body, _ := json.Marshal(deployComponentRequest{
		Name:   "bad",
		Source: "this is not valid go code",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/deploy/component", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Prompt tests ---

func TestDynamicComponentPrompt(t *testing.T) {
	spec := ComponentSpec{
		Name:        "price-checker",
		Type:        "stock.price.checker",
		Description: "Checks stock prices using mock data",
	}

	prompt := DynamicComponentPrompt(spec)

	if !strings.Contains(prompt, spec.Name) {
		t.Error("prompt missing component name")
	}
	if !strings.Contains(prompt, spec.Type) {
		t.Error("prompt missing component type")
	}
	if !strings.Contains(prompt, "package component") {
		t.Error("prompt missing package component instruction")
	}
	if !strings.Contains(prompt, "standard library") {
		t.Error("prompt missing stdlib-only instruction")
	}
	if !strings.Contains(prompt, "Name() string") {
		t.Error("prompt missing Name function signature")
	}
	if !strings.Contains(prompt, "Execute(") {
		t.Error("prompt missing Execute function signature")
	}
}

func TestEnsureDynamicFormat(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid dynamic source",
			source:  testDynamicSource,
			wantErr: false,
		},
		{
			name: "wrong package",
			source: `package main
func Name() string { return "bad" }
`,
			wantErr: true,
			errMsg:  "package component",
		},
		{
			name: "blocked import",
			source: `package component
import "os/exec"
func Name() string { return "bad" }
`,
			wantErr: true,
			errMsg:  "os/exec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ensureDynamicFormat(tt.source, "test")
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
