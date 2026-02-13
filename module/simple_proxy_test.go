package module

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimpleProxy_Name(t *testing.T) {
	p := NewSimpleProxy("test-proxy")
	if p.Name() != "test-proxy" {
		t.Errorf("expected name 'test-proxy', got %q", p.Name())
	}
}

func TestSimpleProxy_SetTargets(t *testing.T) {
	p := NewSimpleProxy("proxy")

	err := p.SetTargets(map[string]string{
		"/api/auth":   "http://localhost:8081",
		"/api/orders": "http://localhost:8082",
	})
	if err != nil {
		t.Fatalf("SetTargets failed: %v", err)
	}

	if len(p.targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(p.targets))
	}
	if len(p.sortedPrefixes) != 2 {
		t.Errorf("expected 2 sorted prefixes, got %d", len(p.sortedPrefixes))
	}
}

func TestSimpleProxy_SetTargetsInvalidURL(t *testing.T) {
	p := NewSimpleProxy("proxy")

	err := p.SetTargets(map[string]string{
		"/api": "://invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestSimpleProxy_Handle(t *testing.T) {
	// Create a backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "test")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response: " + r.URL.Path))
	}))
	defer backend.Close()

	p := NewSimpleProxy("proxy")
	err := p.SetTargets(map[string]string{
		"/api": backend.URL,
	})
	if err != nil {
		t.Fatalf("SetTargets failed: %v", err)
	}

	// Test proxied request
	req := httptest.NewRequest("GET", "/api/products", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Backend") != "test" {
		t.Error("expected X-Backend header from backend")
	}
	if string(body) != "backend response: /api/products" {
		t.Errorf("unexpected body: %s", string(body))
	}
}

func TestSimpleProxy_HandleNoMatch(t *testing.T) {
	p := NewSimpleProxy("proxy")
	err := p.SetTargets(map[string]string{
		"/api": "http://localhost:9999",
	})
	if err != nil {
		t.Fatalf("SetTargets failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/unknown/path", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestSimpleProxy_HandleBackendUnavailable(t *testing.T) {
	p := NewSimpleProxy("proxy")
	err := p.SetTargets(map[string]string{
		"/api/orders": "http://127.0.0.1:59999",
	})
	if err != nil {
		t.Fatalf("SetTargets failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/orders/123", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nbody: %s", err, string(body))
	}

	if errResp["error"] != "backend unavailable" {
		t.Errorf("expected error 'backend unavailable', got %q", errResp["error"])
	}
	if errResp["backend"] != "127.0.0.1:59999" {
		t.Errorf("expected backend '127.0.0.1:59999', got %q", errResp["backend"])
	}
	if errResp["path"] != "/api/orders/123" {
		t.Errorf("expected path '/api/orders/123', got %q", errResp["path"])
	}
}

func TestSimpleProxy_LongestPrefixMatch(t *testing.T) {
	// Two backends for different path depths
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend-A"))
	}))
	defer backendA.Close()

	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend-B"))
	}))
	defer backendB.Close()

	p := NewSimpleProxy("proxy")
	err := p.SetTargets(map[string]string{
		"/api":        backendA.URL,
		"/api/orders": backendB.URL,
	})
	if err != nil {
		t.Fatalf("SetTargets failed: %v", err)
	}

	// /api/orders/123 should match /api/orders (longer prefix) -> backend-B
	req := httptest.NewRequest("GET", "/api/orders/123", nil)
	w := httptest.NewRecorder()
	p.Handle(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	if string(body) != "backend-B" {
		t.Errorf("expected backend-B, got %s", string(body))
	}

	// /api/products should match /api -> backend-A
	req2 := httptest.NewRequest("GET", "/api/products", nil)
	w2 := httptest.NewRecorder()
	p.Handle(w2, req2)

	body2, _ := io.ReadAll(w2.Result().Body)
	if string(body2) != "backend-A" {
		t.Errorf("expected backend-A, got %s", string(body2))
	}
}

func TestSimpleProxy_ProvidesServices(t *testing.T) {
	p := NewSimpleProxy("my-proxy")
	svcs := p.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "my-proxy" {
		t.Errorf("expected service name 'my-proxy', got %q", svcs[0].Name)
	}
}

func TestSimpleProxy_RequiresServices(t *testing.T) {
	p := NewSimpleProxy("proxy")
	deps := p.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(deps))
	}
}
