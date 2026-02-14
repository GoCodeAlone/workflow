package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func TestNewIntegrationWorkflowHandler(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestIntegrationWorkflowHandler_Name(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	if h.Name() == "" {
		t.Error("expected non-empty name")
	}
}

func TestIntegrationWorkflowHandler_CanHandle(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	if !h.CanHandle("integration") {
		t.Error("expected CanHandle('integration') to be true")
	}
	if h.CanHandle("http") {
		t.Error("expected CanHandle('http') to be false")
	}
}

func TestIntegrationWorkflowHandler_Init(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := make(map[string]any)
	err := h.Init(registry)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if registry[h.Name()] != h {
		t.Error("expected handler to be registered in registry")
	}
}

func TestIntegrationWorkflowHandler_StartStop(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	ctx := context.Background()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := h.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIntegrationWorkflowHandler_ConfigureWorkflow_InvalidFormat(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := CreateMockApplication()
	err := h.ConfigureWorkflow(app, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestIntegrationWorkflowHandler_ConfigureWorkflow_NoRegistry(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := CreateMockApplication()
	err := h.ConfigureWorkflow(app, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing registry")
	}
}

func TestIntegrationWorkflowHandler_ConfigureWorkflow_RegistryNotFound(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := CreateMockApplication()
	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "my-registry",
	})
	if err == nil {
		t.Fatal("expected error for missing registry service")
	}
}

func TestIntegrationWorkflowHandler_ExecuteIntegrationWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer server.Close()

	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")
	conn := module.NewHTTPIntegrationConnector("test-api", server.URL)
	conn.SetAllowPrivateIPs(true)
	_ = conn.Connect(context.Background())
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-api",
			Action:    "GET /data",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("ExecuteIntegrationWorkflow failed: %v", err)
	}
	stepResult, ok := result["step1"].(map[string]any)
	if !ok {
		t.Fatal("expected step1 result")
	}
	if stepResult["result"] != "ok" {
		t.Errorf("expected result ok, got %v", stepResult["result"])
	}
}

func TestIntegrationWorkflowHandler_ExecuteIntegrationWorkflow_ConnectorNotFound(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "missing",
			Action:    "GET /data",
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error for missing connector")
	}
}

func TestIntegrationWorkflowHandler_ExecuteIntegrationWorkflow_NotConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer server.Close()

	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")
	conn := module.NewHTTPIntegrationConnector("test-api", server.URL)
	conn.SetAllowPrivateIPs(true)
	// Not connecting - should auto-connect
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-api",
			Action:    "GET /data",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("ExecuteIntegrationWorkflow failed: %v", err)
	}
	if result["step1"] == nil {
		t.Error("expected step1 result")
	}
}

func TestIntegrationWorkflowHandler_ExecuteIntegrationWorkflow_VariableSubstitution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"value": "test-data"})
	}))
	defer server.Close()

	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")
	conn := module.NewHTTPIntegrationConnector("test-api", server.URL)
	conn.SetAllowPrivateIPs(true)
	_ = conn.Connect(context.Background())
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-api",
			Action:    "GET /data",
		},
		{
			Name:      "step2",
			Connector: "test-api",
			Action:    "GET /next",
			Input: map[string]any{
				"ref":    "${step1.value}",
				"static": "plain-val",
			},
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("ExecuteIntegrationWorkflow failed: %v", err)
	}
}

func TestIntegrationWorkflowHandler_ExecuteIntegrationWorkflow_WithOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal"})
	}))
	defer server.Close()

	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")
	conn := module.NewHTTPIntegrationConnector("test-api", server.URL)
	conn.SetAllowPrivateIPs(true)
	_ = conn.Connect(context.Background())
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-api",
			Action:    "GET /fail",
			OnError:   "continue",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("expected no error with OnError handler, got: %v", err)
	}
	if result["step1_error"] == nil {
		t.Error("expected step1_error in results")
	}
}

func TestIntegrationWorkflowHandler_ExecuteIntegrationWorkflow_WithOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")
	conn := module.NewHTTPIntegrationConnector("test-api", server.URL)
	conn.SetAllowPrivateIPs(true)
	_ = conn.Connect(context.Background())
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-api",
			Action:    "GET /data",
			OnSuccess: "next-step",
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("ExecuteIntegrationWorkflow failed: %v", err)
	}
}
