package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	for i := range 5 {
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
	for range 2 {
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
	if err := m.Start(context.TODO()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestRateLimitMiddleware_Stop(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)
	if err := m.Stop(context.TODO()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestCORSMiddleware_Start(t *testing.T) {
	m := NewCORSMiddleware("cors", nil, nil)
	if err := m.Start(context.TODO()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestCORSMiddleware_Stop(t *testing.T) {
	m := NewCORSMiddleware("cors", nil, nil)
	if err := m.Stop(context.TODO()); err != nil {
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

// -- Token-based rate limiting tests --

func TestNewRateLimitMiddleware_DefaultStrategy(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)
	if m.Strategy() != RateLimitByIP {
		t.Errorf("expected default strategy IP, got %q", m.Strategy())
	}
}

func TestNewRateLimitMiddlewareWithStrategy_Token(t *testing.T) {
	m := NewRateLimitMiddlewareWithStrategy("rl", 60, 10, RateLimitByToken)
	if m.Strategy() != RateLimitByToken {
		t.Errorf("expected strategy Token, got %q", m.Strategy())
	}
}

func TestRateLimitMiddleware_TokenStrategy(t *testing.T) {
	m := NewRateLimitMiddlewareWithStrategy("rl", 60, 1, RateLimitByToken)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with token A should pass
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("Authorization", "Bearer token-a")
	req1.RemoteAddr = "192.168.1.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("token A first request: expected 200, got %d", rec1.Code)
	}

	// Second request with token A should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("Authorization", "Bearer token-a")
	req2.RemoteAddr = "192.168.1.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("token A second request: expected 429, got %d", rec2.Code)
	}

	// Request with token B should pass (separate bucket)
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.Header.Set("Authorization", "Bearer token-b")
	req3.RemoteAddr = "192.168.1.1:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("token B first request: expected 200, got %d", rec3.Code)
	}
}

func TestRateLimitMiddleware_TokenStrategy_FallbackToIP(t *testing.T) {
	m := NewRateLimitMiddlewareWithStrategy("rl", 60, 1, RateLimitByToken)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request with no token falls back to IP
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("no token first request: expected 200, got %d", rec.Code)
	}

	// Same IP should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.1:5678"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("no token second request: expected 429, got %d", rec2.Code)
	}
}

func TestRateLimitMiddleware_IPAndTokenStrategy(t *testing.T) {
	m := NewRateLimitMiddlewareWithStrategy("rl", 60, 1, RateLimitByIPAndToken)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP1 + token A
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("Authorization", "Bearer token-a")
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("ip1+tokenA: expected 200, got %d", rec1.Code)
	}

	// IP2 + same token A should pass (different combined key)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("Authorization", "Bearer token-a")
	req2.RemoteAddr = "10.0.0.2:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("ip2+tokenA: expected 200, got %d", rec2.Code)
	}
}

func TestRateLimitMiddleware_RetryAfterHeader(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 1)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust burst
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Rate limited request should include Retry-After
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "1" {
		t.Errorf("expected Retry-After header '1', got %q", rec2.Header().Get("Retry-After"))
	}
}

func TestRateLimitMiddleware_XForwardedFor(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 1)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request with X-Forwarded-For should use that IP
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "127.0.0.1:1234"
	req1.Header.Set("X-Forwarded-For", "203.0.113.50")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rec1.Code)
	}

	// Different X-Forwarded-For should be separate bucket
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("X-Forwarded-For", "203.0.113.51")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("different forwarded IP: expected 200, got %d", rec2.Code)
	}
}

func TestRateLimitMiddleware_SetTokenHeader(t *testing.T) {
	m := NewRateLimitMiddlewareWithStrategy("rl", 60, 1, RateLimitByToken)
	m.SetTokenHeader("X-API-Key")

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "my-key-123")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("custom token header: expected 200, got %d", rec.Code)
	}

	// Same key should be limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-API-Key", "my-key-123")
	req2.RemoteAddr = "10.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("custom token header repeated: expected 429, got %d", rec2.Code)
	}
}

func TestRateLimitMiddleware_CleanupStaleClients(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)

	// Manually add a stale client
	m.mu.Lock()
	m.clients["ip:stale-client"] = &client{
		tokens:        10.0,
		lastTimestamp: time.Now().Add(-1 * time.Hour), // 1 hour old
	}
	m.clients["ip:fresh-client"] = &client{
		tokens:        10.0,
		lastTimestamp: time.Now(),
	}
	m.mu.Unlock()

	m.cleanupStaleClients()

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.clients["ip:stale-client"]; exists {
		t.Error("expected stale client to be cleaned up")
	}
	if _, exists := m.clients["ip:fresh-client"]; !exists {
		t.Error("expected fresh client to still exist")
	}
}

func TestRateLimitMiddleware_StartStop_Lifecycle(t *testing.T) {
	m := NewRateLimitMiddleware("rl", 60, 10)

	if err := m.Start(context.TODO()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let cleanup run at least once
	time.Sleep(10 * time.Millisecond)

	if err := m.Stop(context.TODO()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// -- Hourly rate middleware tests --

func TestNewRateLimitMiddlewareWithHourlyRate_AllowsBurst(t *testing.T) {
	// 5 requests/hour, burst 5
	m := NewRateLimitMiddlewareWithHourlyRate("rl-hour", 5, 5)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 5 requests should succeed (burst exhausted)
	for i := range 5 {
		req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 6th request should be rate limited
	req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst exhausted, got %d", rec.Code)
	}
}

func TestNewRateLimitMiddlewareWithHourlyRate_PerIP(t *testing.T) {
	// 5 requests/hour, burst 2
	m := NewRateLimitMiddlewareWithHourlyRate("rl-hour", 5, 2)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust IP A burst
	for i := range 2 {
		req := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("IP A request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// IP A should be rate limited
	reqA := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
	reqA.RemoteAddr = "10.0.0.1:1234"
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusTooManyRequests {
		t.Errorf("IP A: expected 429, got %d", recA.Code)
	}

	// IP B should still be allowed (separate bucket)
	reqB := httptest.NewRequest("POST", "/api/v1/auth/register", nil)
	reqB.RemoteAddr = "10.0.0.2:5678"
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Errorf("IP B: expected 200, got %d", recB.Code)
	}
}

func TestNewRateLimitMiddlewareWithHourlyRate_RatePerMinute(t *testing.T) {
	// 60 requests/hour = 1 request/minute
	m := NewRateLimitMiddlewareWithHourlyRate("rl-hour", 60, 1)
	// ratePerMinute should be 1.0
	if m.ratePerMinute != 1.0 {
		t.Errorf("expected ratePerMinute=1.0, got %f", m.ratePerMinute)
	}
}

func TestNewRateLimitMiddlewareWithHourlyRate_FractionalRefill(t *testing.T) {
	// 3600 requests/hour -> ratePerMinute = 60.0, timePerToken = 1 second.
	// Using a high hourly rate keeps the sleep short while still exercising
	// the fractional refill path.
	m := NewRateLimitMiddlewareWithHourlyRate("rl-hour-fractional", 3600, 1)

	handler := m.Process(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should be allowed (uses the single burst token).
	req1 := httptest.NewRequest("GET", "/fractional", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}

	// Second immediate request must be rate-limited (burst exhausted, no refill yet).
	req2 := httptest.NewRequest("GET", "/fractional", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rec2.Code)
	}

	// Wait slightly longer than the time needed to refill one token.
	if m.ratePerMinute <= 0 {
		t.Fatalf("ratePerMinute must be positive, got %f", m.ratePerMinute)
	}
	timePerToken := time.Duration(float64(time.Minute) / m.ratePerMinute)
	time.Sleep(timePerToken + 100*time.Millisecond)

	// After waiting, exactly one additional request should be allowed.
	req3 := httptest.NewRequest("GET", "/fractional", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("third request after refill: expected 200, got %d", rec3.Code)
	}

	// An immediately following request must still be rate-limited.
	req4 := httptest.NewRequest("GET", "/fractional", nil)
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusTooManyRequests {
		t.Fatalf("fourth request after refill: expected 429, got %d", rec4.Code)
	}
}
