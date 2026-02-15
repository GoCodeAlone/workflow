package module

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommandHandler_Name(t *testing.T) {
	h := NewCommandHandler("test-commands")
	if h.Name() != "test-commands" {
		t.Errorf("expected name 'test-commands', got %q", h.Name())
	}
}

func TestCommandHandler_Init(t *testing.T) {
	h := NewCommandHandler("test-commands")
	if err := h.Init(nil); err != nil {
		t.Errorf("Init should return nil, got %v", err)
	}
}

func TestCommandHandler_ProvidesServices(t *testing.T) {
	h := NewCommandHandler("test-commands")
	svcs := h.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "test-commands" {
		t.Errorf("expected service name 'test-commands', got %q", svcs[0].Name)
	}
	if svcs[0].Instance != h {
		t.Error("expected service instance to be the handler itself")
	}
}

func TestCommandHandler_RequiresServices_NoDelegate(t *testing.T) {
	h := NewCommandHandler("test-commands")
	if deps := h.RequiresServices(); deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestCommandHandler_RequiresServices_WithDelegate(t *testing.T) {
	h := NewCommandHandler("test-commands")
	h.SetDelegate("my-service")
	deps := h.RequiresServices()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].Name != "my-service" {
		t.Errorf("expected dependency name 'my-service', got %q", deps[0].Name)
	}
}

func TestCommandHandler_DispatchSuccess(t *testing.T) {
	h := NewCommandHandler("test-commands")
	h.RegisterCommand("reload", func(_ context.Context, _ *http.Request) (any, error) {
		return map[string]string{"status": "reloaded"}, nil
	})

	req := httptest.NewRequest("POST", "/api/v1/admin/engine/reload", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["status"] != "reloaded" {
		t.Errorf("expected status=reloaded, got %v", result)
	}
}

func TestCommandHandler_DispatchNotFound(t *testing.T) {
	h := NewCommandHandler("test-commands")

	req := httptest.NewRequest("POST", "/api/v1/admin/engine/unknown", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestCommandHandler_DispatchError(t *testing.T) {
	h := NewCommandHandler("test-commands")
	h.RegisterCommand("broken", func(_ context.Context, _ *http.Request) (any, error) {
		return nil, errors.New("command failed")
	})

	req := httptest.NewRequest("POST", "/api/v1/admin/engine/broken", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["error"] != "command failed" {
		t.Errorf("expected error 'command failed', got %v", result)
	}
}

func TestCommandHandler_NilResult(t *testing.T) {
	h := NewCommandHandler("test-commands")
	h.RegisterCommand("delete", func(_ context.Context, _ *http.Request) (any, error) {
		return nil, nil
	})

	req := httptest.NewRequest("DELETE", "/api/v1/admin/components/delete", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for nil result, got %d", rr.Code)
	}
}

func TestCommandHandler_DelegateUsed(t *testing.T) {
	h := NewCommandHandler("test")
	delegateCalled := false

	// Manually set the delegate handler (simulates resolved delegate)
	h.delegateHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delegateCalled = true
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"delegate": "true"})
	})

	req := httptest.NewRequest("PUT", "/api/v1/admin/workflows/abc-123", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if !delegateCalled {
		t.Error("expected delegate to be called for unmatched command")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from delegate, got %d", rr.Code)
	}
}

func TestCommandHandler_DelegateNotUsedWhenCommandMatches(t *testing.T) {
	h := NewCommandHandler("test")
	delegateCalled := false
	h.delegateHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delegateCalled = true
		w.WriteHeader(http.StatusOK)
	})
	h.RegisterCommand("reload", func(_ context.Context, _ *http.Request) (any, error) {
		return "matched", nil
	})

	req := httptest.NewRequest("POST", "/api/v1/admin/engine/reload", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if delegateCalled {
		t.Error("delegate should not be called when command matches")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCommandHandler_Handle(t *testing.T) {
	h := NewCommandHandler("test")
	h.RegisterCommand("validate", func(_ context.Context, _ *http.Request) (any, error) {
		return map[string]bool{"valid": true}, nil
	})

	req := httptest.NewRequest("POST", "/api/v1/admin/engine/validate", nil)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
