package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestNewWorkflowUIHandler_NilConfig(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.config == nil {
		t.Fatal("expected config to be initialized with empty config")
	}
}

func TestNewWorkflowUIHandler_WithConfig(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test", Type: "http.server"},
		},
	}
	h := NewWorkflowUIHandler(cfg)
	if h.config != cfg {
		t.Error("expected handler to hold provided config")
	}
}

func TestWorkflowUIHandler_RegisterRoutes(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()

	// RegisterRoutes should not panic
	h.RegisterRoutes(mux)

	// Verify the API endpoints are accessible
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/workflow/config"},
		{http.MethodGet, "/api/workflow/modules"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s %s not registered", tc.method, tc.path)
		}
	}
}

func TestWorkflowUIHandler_HandleGetConfig(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "myserver", Type: "http.server"},
		},
	}
	h := NewWorkflowUIHandler(cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", ct)
	}

	var result config.WorkflowConfig
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(result.Modules))
	}
	if result.Modules[0].Name != "myserver" {
		t.Errorf("expected module name 'myserver', got '%s'", result.Modules[0].Name)
	}
}

func TestWorkflowUIHandler_HandlePutConfig_JSON(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	newCfg := config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "updated-server", Type: "http.server"},
			{Name: "router", Type: "http.router"},
		},
	}
	body, _ := json.Marshal(newCfg)

	req := httptest.NewRequest(http.MethodPut, "/api/workflow/config", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify the config was updated
	req = httptest.NewRequest(http.MethodGet, "/api/workflow/config", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result config.WorkflowConfig
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Modules) != 2 {
		t.Errorf("expected 2 modules after update, got %d", len(result.Modules))
	}
}

func TestWorkflowUIHandler_HandlePutConfig_YAML(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	yamlBody := `
modules:
  - name: yaml-server
    type: http.server
`

	req := httptest.NewRequest(http.MethodPut, "/api/workflow/config", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/x-yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify the config was updated with YAML content
	req = httptest.NewRequest(http.MethodGet, "/api/workflow/config", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result config.WorkflowConfig
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Modules) != 1 {
		t.Errorf("expected 1 module after YAML update, got %d", len(result.Modules))
	}
	if result.Modules[0].Name != "yaml-server" {
		t.Errorf("expected module name 'yaml-server', got '%s'", result.Modules[0].Name)
	}
}

func TestWorkflowUIHandler_HandlePutConfig_TextYAML(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	yamlBody := `
modules:
  - name: text-yaml-server
    type: http.server
`
	req := httptest.NewRequest(http.MethodPut, "/api/workflow/config", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "text/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestWorkflowUIHandler_HandlePutConfig_InvalidJSON(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/workflow/config", strings.NewReader("not valid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestWorkflowUIHandler_HandlePutConfig_InvalidYAML(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/workflow/config", strings.NewReader(":\n  :\n  bad: ["))
	req.Header.Set("Content-Type", "application/x-yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestWorkflowUIHandler_HandleGetModules(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/modules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var modules []moduleTypeDef
	if err := json.NewDecoder(w.Body).Decode(&modules); err != nil {
		t.Fatalf("failed to decode modules: %v", err)
	}
	if len(modules) == 0 {
		t.Error("expected at least one module type definition")
	}

	// Check that we have some known module types
	found := false
	for _, m := range modules {
		if m.Type == "http.server" {
			found = true
			if m.Category != "http" {
				t.Errorf("expected http.server category 'http', got '%s'", m.Category)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find 'http.server' in available modules")
	}
}

func TestWorkflowUIHandler_HandleValidate_Valid(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cfg := config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
			{Name: "router", Type: "http.router", DependsOn: []string{"server"}},
		},
	}
	body, _ := json.Marshal(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/validate", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result validationResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid=true, got errors: %v", result.Errors)
	}
}

func TestWorkflowUIHandler_HandleValidate_NoModules(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cfg := config.WorkflowConfig{
		Modules: []config.ModuleConfig{},
	}
	body, _ := json.Marshal(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/validate", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result validationResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Valid {
		t.Error("expected valid=false for no modules")
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one error")
	}
}

func TestWorkflowUIHandler_HandleValidate_DuplicateNames(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cfg := config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
			{Name: "server", Type: "http.server"},
		},
	}
	body, _ := json.Marshal(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/validate", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result validationResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Valid {
		t.Error("expected valid=false for duplicate names")
	}
}

func TestWorkflowUIHandler_HandleValidate_EmptyModuleName(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cfg := config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "", Type: "http.server"},
		},
	}
	body, _ := json.Marshal(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/validate", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result validationResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Valid {
		t.Error("expected valid=false for empty module name")
	}
}

func TestWorkflowUIHandler_HandleValidate_UnknownDependency(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cfg := config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "router", Type: "http.router", DependsOn: []string{"nonexistent"}},
		},
	}
	body, _ := json.Marshal(cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/validate", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result validationResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Valid {
		t.Error("expected valid=false for unknown dependency")
	}
}

func TestWorkflowUIHandler_HandleValidate_InvalidJSON(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/validate", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestWorkflowUIHandler_HandleStatus_Default(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "a", Type: "http.server"},
			{Name: "b", Type: "http.router"},
		},
		Workflows: map[string]any{"w1": nil},
	}
	h := NewWorkflowUIHandler(cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["status"] != "running" {
		t.Errorf("expected status 'running', got %v", result["status"])
	}
	if result["moduleCount"] != float64(2) {
		t.Errorf("expected moduleCount 2, got %v", result["moduleCount"])
	}
	if result["workflowCount"] != float64(1) {
		t.Errorf("expected workflowCount 1, got %v", result["workflowCount"])
	}
}

func TestWorkflowUIHandler_HandleStatus_WithStatusFunc(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	h.SetStatusFunc(func() map[string]any {
		return map[string]any{
			"status": "custom",
			"uptime": "10m",
		}
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["status"] != "custom" {
		t.Errorf("expected status 'custom', got %v", result["status"])
	}
	if result["uptime"] != "10m" {
		t.Errorf("expected uptime '10m', got %v", result["uptime"])
	}
}

func TestWorkflowUIHandler_HandleReload_NoFunc(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/reload", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestWorkflowUIHandler_HandleReload_Success(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	called := false
	h.SetReloadFunc(func(cfg *config.WorkflowConfig) error {
		called = true
		return nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/reload", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if !called {
		t.Error("expected reload function to be called")
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["status"] != "reloaded" {
		t.Errorf("expected status 'reloaded', got %v", result["status"])
	}
}

func TestWorkflowUIHandler_HandleReload_Error(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	h.SetReloadFunc(func(cfg *config.WorkflowConfig) error {
		return fmt.Errorf("reload failed")
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/reload", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["error"] != "reload failed" {
		t.Errorf("expected error 'reload failed', got %v", result["error"])
	}
}

func TestWorkflowUIHandler_SetReloadFunc(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	if h.reloadFn != nil {
		t.Error("expected nil reloadFn initially")
	}
	h.SetReloadFunc(func(cfg *config.WorkflowConfig) error { return nil })
	if h.reloadFn == nil {
		t.Error("expected non-nil reloadFn after set")
	}
}

func TestWorkflowUIHandler_SetStatusFunc(t *testing.T) {
	h := NewWorkflowUIHandler(nil)
	if h.engineStatus != nil {
		t.Error("expected nil engineStatus initially")
	}
	h.SetStatusFunc(func() map[string]any { return nil })
	if h.engineStatus == nil {
		t.Error("expected non-nil engineStatus after set")
	}
}
