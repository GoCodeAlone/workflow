package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOTelMiddleware(t *testing.T) {
	m := NewOTelMiddleware("otel-mw", "workflow-http")
	if m.Name() != "otel-mw" {
		t.Errorf("expected name 'otel-mw', got %q", m.Name())
	}
}

func TestOTelMiddleware_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewOTelMiddleware("otel-mw", "workflow-http")
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestOTelMiddleware_Process_CallsNext(t *testing.T) {
	m := NewOTelMiddleware("otel-mw", "workflow-http")

	nextCalled := false
	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("expected next handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestOTelMiddleware_ProvidesServices(t *testing.T) {
	m := NewOTelMiddleware("otel-mw", "workflow-http")
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "otel-mw" {
		t.Errorf("expected service name 'otel-mw', got %q", svcs[0].Name)
	}
}

func TestOTelMiddleware_RequiresServices(t *testing.T) {
	m := NewOTelMiddleware("otel-mw", "workflow-http")
	if m.RequiresServices() != nil {
		t.Error("expected nil dependencies")
	}
}

func TestOTelMiddleware_Start(t *testing.T) {
	m := NewOTelMiddleware("otel-mw", "workflow-http")
	if err := m.Start(context.TODO()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestOTelMiddleware_Stop(t *testing.T) {
	m := NewOTelMiddleware("otel-mw", "workflow-http")
	if err := m.Stop(context.TODO()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
