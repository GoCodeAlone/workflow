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
	conn.AllowPrivateIPs()
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
	conn.AllowPrivateIPs()
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
	conn.AllowPrivateIPs()
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
	conn.AllowPrivateIPs()
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
	conn.AllowPrivateIPs()
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

// --- Tests for extracted helper functions ---

func TestParseConnectorConfigs_Empty(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseConnectorConfigs(nil, registry)
	if err == nil {
		t.Fatal("expected error for empty connectors")
	}
}

func TestParseConnectorConfigs_InvalidEntry(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseConnectorConfigs([]any{"not-a-map"}, registry)
	if err == nil {
		t.Fatal("expected error for non-map entry")
	}
}

func TestParseConnectorConfigs_MissingName(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseConnectorConfigs([]any{map[string]any{"type": "http"}}, registry)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseConnectorConfigs_MissingType(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseConnectorConfigs([]any{map[string]any{"name": "c"}}, registry)
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestParseConnectorConfigs_UnknownType(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseConnectorConfigs([]any{map[string]any{"name": "c", "type": "unknown"}}, registry)
	if err == nil {
		t.Fatal("expected error for unknown connector type")
	}
}

func TestParseConnectorConfigs_ValidHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	registry := module.NewIntegrationRegistry("test")
	err := parseConnectorConfigs([]any{
		map[string]any{
			"name": "my-conn",
			"type": "http",
			"config": map[string]any{
				"baseURL":         server.URL,
				"allowPrivateIPs": true,
			},
		},
	}, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	conn, err := registry.GetConnector("my-conn")
	if err != nil || conn == nil {
		t.Fatal("expected connector to be registered")
	}
}

func TestParseStepConfigs_InvalidEntry(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseStepConfigs([]any{"not-a-map"}, registry)
	if err == nil {
		t.Fatal("expected error for non-map step entry")
	}
}

func TestParseStepConfigs_MissingName(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseStepConfigs([]any{map[string]any{"connector": "c", "action": "GET /"}}, registry)
	if err == nil {
		t.Fatal("expected error for missing step name")
	}
}

func TestParseStepConfigs_MissingConnector(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseStepConfigs([]any{map[string]any{"name": "s", "action": "GET /"}}, registry)
	if err == nil {
		t.Fatal("expected error for missing connector")
	}
}

func TestParseStepConfigs_ConnectorNotRegistered(t *testing.T) {
	registry := module.NewIntegrationRegistry("test")
	err := parseStepConfigs([]any{map[string]any{"name": "s", "connector": "missing", "action": "GET /"}}, registry)
	if err == nil {
		t.Fatal("expected error for unregistered connector")
	}
}

func TestParseStepConfigs_MissingAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	registry := module.NewIntegrationRegistry("test")
	conn := module.NewHTTPIntegrationConnector("my-conn", server.URL)
	conn.AllowPrivateIPs()
	registry.RegisterConnector(conn)

	err := parseStepConfigs([]any{map[string]any{"name": "s", "connector": "my-conn"}}, registry)
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestParseStepConfigs_Valid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	registry := module.NewIntegrationRegistry("test")
	conn := module.NewHTTPIntegrationConnector("my-conn", server.URL)
	conn.AllowPrivateIPs()
	registry.RegisterConnector(conn)

	err := parseStepConfigs([]any{map[string]any{"name": "s", "connector": "my-conn", "action": "GET /"}}, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteStepWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	conn := module.NewHTTPIntegrationConnector("c", server.URL)
	conn.AllowPrivateIPs()
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	step := &IntegrationStep{Name: "s", Action: "GET /", RetryCount: 0}
	result, err := executeStepWithRetry(context.Background(), conn, step, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecuteStepWithRetry_RetryOnError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "retry"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	conn := module.NewHTTPIntegrationConnector("c", server.URL)
	conn.AllowPrivateIPs()
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	step := &IntegrationStep{Name: "s", Action: "GET /", RetryCount: 3, RetryDelay: "1ms"}
	result, err := executeStepWithRetry(context.Background(), conn, step, nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecuteStepWithRetry_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "fail"})
	}))
	defer server.Close()

	conn := module.NewHTTPIntegrationConnector("c", server.URL)
	conn.AllowPrivateIPs()
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	step := &IntegrationStep{Name: "s", Action: "GET /", RetryCount: 5, RetryDelay: "1s"}
	_, err := executeStepWithRetry(ctx, conn, step, nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestExecuteStepWithRetry_ExhaustedRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "always-fail"})
	}))
	defer server.Close()

	conn := module.NewHTTPIntegrationConnector("c", server.URL)
	conn.AllowPrivateIPs()
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	step := &IntegrationStep{Name: "s", Action: "GET /", RetryCount: 2, RetryDelay: "1ms"}
	_, err := executeStepWithRetry(context.Background(), conn, step, nil)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestResolveParamValue_PlainValue(t *testing.T) {
	results := map[string]any{"step1": map[string]any{"value": "hello"}}
	got := resolveParamValue(42, results)
	if got != 42 {
		t.Errorf("expected 42, got %v", got)
	}
}

func TestResolveParamValue_ExactMatch(t *testing.T) {
	results := map[string]any{"step1": "direct"}
	got := resolveParamValue("${step1}", results)
	if got != "direct" {
		t.Errorf("expected 'direct', got %v", got)
	}
}

func TestResolveParamValue_DotNotation(t *testing.T) {
	results := map[string]any{
		"step1": map[string]any{"value": "resolved"},
	}
	got := resolveParamValue("${step1.value}", results)
	if got != "resolved" {
		t.Errorf("expected 'resolved', got %v", got)
	}
}

func TestResolveParamValue_DotNotation_MissingKey(t *testing.T) {
	results := map[string]any{
		"step1": map[string]any{"other": "x"},
	}
	got := resolveParamValue("${step1.value}", results)
	if got != "${step1.value}" {
		t.Errorf("expected original string, got %v", got)
	}
}

func TestResolveParamValue_DotNotation_NonMapResult(t *testing.T) {
	results := map[string]any{"step1": "not-a-map"}
	got := resolveParamValue("${step1.value}", results)
	if got != "${step1.value}" {
		t.Errorf("expected original string when step result is not a map, got %v", got)
	}
}

func TestResolveParamValue_DotNotation_MissingStep(t *testing.T) {
	results := map[string]any{}
	got := resolveParamValue("${step1.value}", results)
	if got != "${step1.value}" {
		t.Errorf("expected original string when step not found, got %v", got)
	}
}

func TestResolveParamValue_NotAReference(t *testing.T) {
	results := map[string]any{"foo": "bar"}
	got := resolveParamValue("just-a-string", results)
	if got != "just-a-string" {
		t.Errorf("expected 'just-a-string', got %v", got)
	}
}
