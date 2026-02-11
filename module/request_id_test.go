package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRequestIDMiddleware(t *testing.T) {
	m := NewRequestIDMiddleware("test-request-id")
	if m.Name() != "test-request-id" {
		t.Errorf("expected name 'test-request-id', got %q", m.Name())
	}
	if m.headerName != "X-Request-ID" {
		t.Errorf("expected default header 'X-Request-ID', got %q", m.headerName)
	}
}

func TestRequestIDMiddleware_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewRequestIDMiddleware("test-request-id")
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestRequestIDMiddleware_Middleware_GeneratesUUID(t *testing.T) {
	m := NewRequestIDMiddleware("test-request-id")
	var capturedID string

	handler := m.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Error("expected a generated request ID, got empty string")
	}

	responseID := rec.Header().Get("X-Request-ID")
	if responseID == "" {
		t.Error("expected X-Request-ID in response header")
	}
	if responseID != capturedID {
		t.Errorf("response header ID %q != context ID %q", responseID, capturedID)
	}
}

func TestRequestIDMiddleware_Middleware_ReadsExistingHeader(t *testing.T) {
	m := NewRequestIDMiddleware("test-request-id")
	var capturedID string

	handler := m.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if capturedID != "my-custom-id-123" {
		t.Errorf("expected 'my-custom-id-123', got %q", capturedID)
	}

	responseID := rec.Header().Get("X-Request-ID")
	if responseID != "my-custom-id-123" {
		t.Errorf("expected response header 'my-custom-id-123', got %q", responseID)
	}
}

func TestGetRequestID_EmptyContext(t *testing.T) {
	id := GetRequestID(context.Background())
	if id != "" {
		t.Errorf("expected empty string for context without request ID, got %q", id)
	}
}

func TestRequestIDMiddleware_ProvidesServices(t *testing.T) {
	m := NewRequestIDMiddleware("test-request-id")
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "test-request-id" {
		t.Errorf("expected service name 'test-request-id', got %q", svcs[0].Name)
	}
}
