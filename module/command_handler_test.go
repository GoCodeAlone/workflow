package module

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// mockPipelineRunner is a minimal PipelineRunner for handler tests.
type mockPipelineRunner struct {
	result map[string]any
	err    error
}

func (m *mockPipelineRunner) Run(_ context.Context, _ map[string]any) (map[string]any, error) {
	return m.result, m.err
}
func (m *mockPipelineRunner) SetLogger(_ *slog.Logger)                    {}
func (m *mockPipelineRunner) SetEventRecorder(_ interfaces.EventRecorder) {}

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

// TestCommandHandler_RoutePipeline_MockRunner verifies that a non-*Pipeline
// PipelineRunner is invoked via Run() and its result is JSON-encoded.
func TestCommandHandler_RoutePipeline_MockRunner(t *testing.T) {
	h := NewCommandHandler("test")
	mock := &mockPipelineRunner{result: map[string]any{"status": "processed"}}
	h.routePipelines["process"] = mock

	req := httptest.NewRequest("POST", "/api/v1/engine/process", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got["status"] != "processed" {
		t.Errorf("expected status=processed, got %v", got)
	}
}

// TestCommandHandler_RoutePipeline_MockRunner_ResponseHandled verifies that
// when the PipelineRunner.Run result contains _response_handled=true the
// handler does not write an additional JSON body.
func TestCommandHandler_RoutePipeline_MockRunner_ResponseHandled(t *testing.T) {
	h := NewCommandHandler("test")
	mock := &mockPipelineRunner{result: map[string]any{"_response_handled": true}}
	h.routePipelines["process"] = mock

	req := httptest.NewRequest("POST", "/api/v1/engine/process", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body when _response_handled=true, got %q", rr.Body.String())
	}
}

// TestCommandHandler_RoutePipeline_MockRunner_Error verifies that a Run() error
// returns a 500 with the error message in the JSON body.
func TestCommandHandler_RoutePipeline_MockRunner_Error(t *testing.T) {
	h := NewCommandHandler("test")
	mock := &mockPipelineRunner{err: errors.New("runner failed")}
	h.routePipelines["process"] = mock

	req := httptest.NewRequest("POST", "/api/v1/engine/process", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if got["error"] != "runner failed" {
		t.Errorf("expected error=runner failed, got %v", got)
	}
}

// TestCommandHandler_RoutePipeline_TypedNil verifies that a typed-nil *Pipeline
// stored as a PipelineRunner does not panic and falls through to 404.
func TestCommandHandler_RoutePipeline_TypedNil(t *testing.T) {
	h := NewCommandHandler("test")
	// Store a typed-nil *Pipeline as an interfaces.PipelineRunner.
	// pipeline != nil is true (interface has type info), concretePipeline == nil.
	var p *Pipeline
	h.routePipelines["process"] = p

	req := httptest.NewRequest("POST", "/api/v1/engine/process", nil)
	rr := httptest.NewRecorder()

	// Must not panic.
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for typed-nil pipeline, got %d", rr.Code)
	}
}

