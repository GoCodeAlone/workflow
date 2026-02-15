package module

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryHandler_Name(t *testing.T) {
	h := NewQueryHandler("test-queries")
	if h.Name() != "test-queries" {
		t.Errorf("expected name 'test-queries', got %q", h.Name())
	}
}

func TestQueryHandler_Init(t *testing.T) {
	h := NewQueryHandler("test-queries")
	if err := h.Init(nil); err != nil {
		t.Errorf("Init should return nil, got %v", err)
	}
}

func TestQueryHandler_ProvidesServices(t *testing.T) {
	h := NewQueryHandler("test-queries")
	svcs := h.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "test-queries" {
		t.Errorf("expected service name 'test-queries', got %q", svcs[0].Name)
	}
	if svcs[0].Instance != h {
		t.Error("expected service instance to be the handler itself")
	}
}

func TestQueryHandler_RequiresServices_NoDelegate(t *testing.T) {
	h := NewQueryHandler("test-queries")
	if deps := h.RequiresServices(); deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestQueryHandler_RequiresServices_WithDelegate(t *testing.T) {
	h := NewQueryHandler("test-queries")
	h.SetDelegate("my-service")
	deps := h.RequiresServices()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].Name != "my-service" {
		t.Errorf("expected dependency name 'my-service', got %q", deps[0].Name)
	}
}

func TestQueryHandler_DispatchSuccess(t *testing.T) {
	h := NewQueryHandler("test-queries")
	h.RegisterQuery("config", func(_ context.Context, _ *http.Request) (any, error) {
		return map[string]string{"key": "value"}, nil
	})

	req := httptest.NewRequest("GET", "/api/v1/admin/engine/config", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result)
	}
}

func TestQueryHandler_DispatchNotFound(t *testing.T) {
	h := NewQueryHandler("test-queries")

	req := httptest.NewRequest("GET", "/api/v1/admin/engine/unknown", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestQueryHandler_DispatchError(t *testing.T) {
	h := NewQueryHandler("test-queries")
	h.RegisterQuery("broken", func(_ context.Context, _ *http.Request) (any, error) {
		return nil, errors.New("something went wrong")
	})

	req := httptest.NewRequest("GET", "/api/v1/admin/engine/broken", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["error"] != "something went wrong" {
		t.Errorf("expected error message, got %v", result)
	}
}

func TestQueryHandler_Handle(t *testing.T) {
	h := NewQueryHandler("test")
	h.RegisterQuery("status", func(_ context.Context, _ *http.Request) (any, error) {
		return map[string]string{"status": "ok"}, nil
	})

	req := httptest.NewRequest("GET", "/api/v1/admin/engine/status", nil)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestQueryHandler_TrailingSlash(t *testing.T) {
	h := NewQueryHandler("test")
	h.RegisterQuery("config", func(_ context.Context, _ *http.Request) (any, error) {
		return "ok", nil
	})

	req := httptest.NewRequest("GET", "/api/v1/admin/engine/config/", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with trailing slash, got %d", rr.Code)
	}
}

func TestQueryHandler_DelegateUsed(t *testing.T) {
	h := NewQueryHandler("test")
	delegateCalled := false

	// Manually set the delegate handler (simulates resolved delegate)
	h.delegateHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delegateCalled = true
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"delegate": "true"})
	})

	req := httptest.NewRequest("GET", "/api/v1/admin/companies/abc-123", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if !delegateCalled {
		t.Error("expected delegate to be called for unmatched query")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from delegate, got %d", rr.Code)
	}
}

func TestQueryHandler_DelegateNotUsedWhenQueryMatches(t *testing.T) {
	h := NewQueryHandler("test")
	delegateCalled := false
	h.delegateHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delegateCalled = true
		w.WriteHeader(http.StatusOK)
	})
	h.RegisterQuery("config", func(_ context.Context, _ *http.Request) (any, error) {
		return "matched", nil
	})

	req := httptest.NewRequest("GET", "/api/v1/admin/engine/config", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if delegateCalled {
		t.Error("delegate should not be called when query matches")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestLastPathSegment(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/v1/admin/engine/config", "config"},
		{"/api/v1/admin/engine/config/", "config"},
		{"/config", "config"},
		{"/", ""},
		{"config", "config"},
		{"", ""},
	}
	for _, tt := range tests {
		got := lastPathSegment(tt.path)
		if got != tt.expected {
			t.Errorf("lastPathSegment(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}
