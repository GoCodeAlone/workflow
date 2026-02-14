package debug

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupTestHandler() (*Handler, *http.ServeMux) {
	d := New()
	h := NewHandler(d)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestHandlerGetState(t *testing.T) {
	_, mux := setupTestHandler()

	req := httptest.NewRequest("GET", "/api/debug/state", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var state ExecutionState
	if err := json.NewDecoder(w.Body).Decode(&state); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if state.Status != "idle" {
		t.Errorf("expected idle status, got %s", state.Status)
	}
}

func TestHandlerAddAndListBreakpoints(t *testing.T) {
	_, mux := setupTestHandler()

	// Add a breakpoint
	body := `{"type":"module","target":"test-mod"}`
	req := httptest.NewRequest("POST", "/api/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var addResp addBreakpointResponse
	if err := json.NewDecoder(w.Body).Decode(&addResp); err != nil {
		t.Fatalf("failed to decode add response: %v", err)
	}
	if addResp.ID == "" {
		t.Fatal("expected non-empty breakpoint ID")
	}

	// List breakpoints
	req = httptest.NewRequest("GET", "/api/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var bps []*Breakpoint
	if err := json.NewDecoder(w.Body).Decode(&bps); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
}

func TestHandlerAddBreakpointValidation(t *testing.T) {
	_, mux := setupTestHandler()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"missing type", `{"target":"x"}`, http.StatusBadRequest},
		{"missing target", `{"type":"module"}`, http.StatusBadRequest},
		{"invalid type", `{"type":"invalid","target":"x"}`, http.StatusBadRequest},
		{"invalid json", `not json`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/debug/breakpoints", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandlerRemoveBreakpoint(t *testing.T) {
	_, mux := setupTestHandler()

	// Add a breakpoint
	body := `{"type":"module","target":"test-mod"}`
	req := httptest.NewRequest("POST", "/api/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var addResp addBreakpointResponse
	_ = json.NewDecoder(w.Body).Decode(&addResp)

	// Remove it
	req = httptest.NewRequest("DELETE", "/api/debug/breakpoints/"+addResp.ID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Remove nonexistent
	req = httptest.NewRequest("DELETE", "/api/debug/breakpoints/nonexistent", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandlerStepWhenNotPaused(t *testing.T) {
	_, mux := setupTestHandler()

	req := httptest.NewRequest("POST", "/api/debug/step", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestHandlerContinueWhenNotPaused(t *testing.T) {
	_, mux := setupTestHandler()

	req := httptest.NewRequest("POST", "/api/debug/continue", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestHandlerReset(t *testing.T) {
	_, mux := setupTestHandler()

	// Add a breakpoint first
	body := `{"type":"module","target":"test-mod"}`
	req := httptest.NewRequest("POST", "/api/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Reset
	req = httptest.NewRequest("POST", "/api/debug/reset", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify breakpoints cleared
	req = httptest.NewRequest("GET", "/api/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var bps []*Breakpoint
	_ = json.NewDecoder(w.Body).Decode(&bps)
	if len(bps) != 0 {
		t.Errorf("expected 0 breakpoints after reset, got %d", len(bps))
	}
}
