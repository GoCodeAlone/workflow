package module

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// -- RateLimitMiddleware tests --

func TestNewRateLimitMiddleware(t *testing.T) {
	m := NewRateLimitMiddleware("rate-limit", 60, 10)
	if m.Name() != "rate-limit" {
		t.Errorf("expected name 'rate-limit', got %q", m.Name())
	}
	if m.requestsPerMinute != 60 {
		t.Errorf("expected 60 rpm, got %d", m.requestsPerMinute)
	}
	if m.burstSize != 10 {
		t.Errorf("expected burst 10, got %d", m.burstSize)
	}
}

func TestRateLimitMiddleware_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewRateLimitMiddleware("rate-limit", 60, 10)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestRateLimitMiddleware_Process_AllowsRequests(t *testing.T) {
	m := NewRateLimitMiddleware("rate-limit", 60, 5)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 5 requests should succeed (burst size)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestRateLimitMiddleware_Process_RateLimits(t *testing.T) {
	m := NewRateLimitMiddleware("rate-limit", 60, 2)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust burst
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_Process_DifferentClients(t *testing.T) {
	m := NewRateLimitMiddleware("rate-limit", 60, 1)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First client uses their burst
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("client 1 first request: expected 200, got %d", rec1.Code)
	}

	// Second client should still get through (separate bucket)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:5678"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("client 2 first request: expected 200, got %d", rec2.Code)
	}
}

func TestRateLimitMiddleware_ProvidesServices(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "rl" {
		t.Errorf("expected service name 'rl', got %q", svcs[0].Name)
	}
}

func TestRateLimitMiddleware_RequiresServices(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)
	if m.RequiresServices() != nil {
		t.Error("expected nil dependencies")
	}
}

// -- LoggingMiddleware tests --

func TestNewLoggingMiddleware(t *testing.T) {
	m := NewLoggingMiddleware("logger", "INFO")
	if m.Name() != "logger" {
		t.Errorf("expected name 'logger', got %q", m.Name())
	}
}

func TestLoggingMiddleware_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewLoggingMiddleware("logger", "INFO")
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if m.logger == nil {
		t.Error("expected logger to be set after Init")
	}
}

func TestLoggingMiddleware_Process(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewLoggingMiddleware("logger", "INFO")
	_ = m.Init(app)

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

func TestLoggingMiddleware_ProvidesServices(t *testing.T) {
	m := NewLoggingMiddleware("log-mw", "DEBUG")
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "log-mw" {
		t.Errorf("expected service name 'log-mw', got %q", svcs[0].Name)
	}
}

func TestLoggingMiddleware_RequiresServices(t *testing.T) {
	m := NewLoggingMiddleware("logger", "INFO")
	if m.RequiresServices() != nil {
		t.Error("expected nil dependencies")
	}
}

// -- CORSMiddleware tests --

func TestNewCORSMiddleware(t *testing.T) {
	m := NewCORSMiddleware("cors", []string{"http://localhost:3000"}, []string{"GET", "POST"})
	if m.Name() != "cors" {
		t.Errorf("expected name 'cors', got %q", m.Name())
	}
}

func TestCORSMiddleware_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	m := NewCORSMiddleware("cors", nil, nil)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestCORSMiddleware_Process_AllowedOrigin(t *testing.T) {
	m := NewCORSMiddleware("cors", []string{"http://localhost:3000"}, []string{"GET", "POST"})

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("expected CORS origin header, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "GET, POST" {
		t.Errorf("expected CORS methods header, got %q", rec.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestCORSMiddleware_Process_DisallowedOrigin(t *testing.T) {
	m := NewCORSMiddleware("cors", []string{"http://localhost:3000"}, []string{"GET"})

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS header for disallowed origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_Process_WildcardOrigin(t *testing.T) {
	m := NewCORSMiddleware("cors", []string{"*"}, []string{"GET"})

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://anything.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://anything.com" {
		t.Errorf("expected CORS header with wildcard, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_Process_Preflight(t *testing.T) {
	m := NewCORSMiddleware("cors", []string{"*"}, []string{"GET", "POST"})

	nextCalled := false
	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for preflight, got %d", rec.Code)
	}
	if nextCalled {
		t.Error("expected next handler NOT to be called for preflight")
	}
}

func TestCORSMiddleware_ProvidesServices(t *testing.T) {
	m := NewCORSMiddleware("cors-mw", nil, nil)
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "cors-mw" {
		t.Errorf("expected service name 'cors-mw', got %q", svcs[0].Name)
	}
}

func TestCORSMiddleware_RequiresServices(t *testing.T) {
	m := NewCORSMiddleware("cors", nil, nil)
	if m.RequiresServices() != nil {
		t.Error("expected nil dependencies")
	}
}

func TestRateLimitMiddleware_Start(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)
	if err := m.Start(nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestRateLimitMiddleware_Stop(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)
	if err := m.Stop(nil); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestCORSMiddleware_Start(t *testing.T) {
	m := NewCORSMiddleware("cors", nil, nil)
	if err := m.Start(nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestCORSMiddleware_Stop(t *testing.T) {
	m := NewCORSMiddleware("cors", nil, nil)
	if err := m.Stop(nil); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("expected min(3, 5) = 3")
	}
	if min(7, 2) != 2 {
		t.Error("expected min(7, 2) = 2")
	}
	if min(4, 4) != 4 {
		t.Error("expected min(4, 4) = 4")
	}
}
