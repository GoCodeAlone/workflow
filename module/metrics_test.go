package module

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewMetricsCollector(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	if m.Name() != "test-metrics" {
		t.Errorf("expected name 'test-metrics', got %q", m.Name())
	}
	if m.registry == nil {
		t.Fatal("expected registry to be initialized")
	}
}

func TestMetricsCollector_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewMetricsCollector("test-metrics")
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestMetricsCollector_RecordWorkflowExecution(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	// Should not panic
	m.RecordWorkflowExecution("http", "process", "success")
	m.RecordWorkflowExecution("http", "process", "error")
}

func TestMetricsCollector_RecordWorkflowDuration(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	m.RecordWorkflowDuration("http", "process", 150*time.Millisecond)
}

func TestMetricsCollector_RecordHTTPRequest(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	m.RecordHTTPRequest("GET", "/api/test", 200, 50*time.Millisecond)
	m.RecordHTTPRequest("POST", "/api/test", 500, 100*time.Millisecond)
}

func TestMetricsCollector_RecordModuleOperation(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	m.RecordModuleOperation("my-module", "init", "success")
}

func TestMetricsCollector_SetActiveWorkflows(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	m.SetActiveWorkflows("http", 5)
	m.SetActiveWorkflows("http", 3)
}

func TestMetricsCollector_Handler(t *testing.T) {
	m := NewMetricsCollector("test-metrics")

	// Record some metrics first
	m.RecordWorkflowExecution("http", "process", "success")
	m.RecordHTTPRequest("GET", "/test", 200, 10*time.Millisecond)

	handler := m.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "workflow_executions_total") {
		t.Error("expected metrics output to contain workflow_executions_total")
	}
	if !strings.Contains(bodyStr, "http_requests_total") {
		t.Error("expected metrics output to contain http_requests_total")
	}
}

func TestMetricsCollector_ProvidesServices(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "metrics.collector" {
		t.Errorf("expected service name 'metrics.collector', got %q", svcs[0].Name)
	}
}

func TestMetricsCollector_RequiresServices(t *testing.T) {
	m := NewMetricsCollector("test-metrics")
	deps := m.RequiresServices()
	if deps != nil {
		t.Errorf("expected no dependencies, got %v", deps)
	}
}
