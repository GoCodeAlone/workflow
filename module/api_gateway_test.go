package module

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewAPIGateway(t *testing.T) {
	gw := NewAPIGateway("test-gw")
	if gw.Name() != "test-gw" {
		t.Errorf("expected name %q, got %q", "test-gw", gw.Name())
	}

	svcs := gw.ProvidesServices()
	if len(svcs) != 1 || svcs[0].Name != "test-gw" {
		t.Errorf("unexpected services: %v", svcs)
	}

	deps := gw.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %v", deps)
	}
}

func TestAPIGateway_SetRoutes(t *testing.T) {
	gw := NewAPIGateway("gw")
	err := gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api/v1", Backend: "http://localhost:8080", Methods: []string{"GET"}},
		{PathPrefix: "/api/v2", Backend: "http://localhost:9090"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	routes := gw.Routes()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
}

func TestAPIGateway_SetRoutes_InvalidBackend(t *testing.T) {
	gw := NewAPIGateway("gw")
	err := gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: "://invalid"},
	})
	if err == nil {
		t.Fatal("expected error for invalid backend URL")
	}
}

func TestAPIGateway_NoMatchReturns404(t *testing.T) {
	gw := NewAPIGateway("gw")
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: "http://localhost:8080"},
	})

	req := httptest.NewRequest("GET", "/unknown", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAPIGateway_MethodNotAllowed(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL, Methods: []string{"GET"}},
	})

	req := httptest.NewRequest("POST", "/api/test", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestAPIGateway_ProxiesToBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL},
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "hello from backend" {
		t.Errorf("expected %q, got %q", "hello from backend", w.Body.String())
	}
}

func TestAPIGateway_StripPrefix(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api/v1", Backend: backend.URL, StripPrefix: true},
	})

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if receivedPath != "/users" {
		t.Errorf("expected stripped path %q, got %q", "/users", receivedPath)
	}
}

func TestAPIGateway_AuthRequired(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	gw.SetAuth(&AuthConfig{Type: "bearer", Header: "Authorization"})
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL, Auth: true},
	})

	// Without auth header
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	// With auth header
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w = httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with auth, got %d", w.Code)
	}
}

func TestAPIGateway_AuthAPIKey(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	gw.SetAuth(&AuthConfig{Type: "api_key", Header: "X-API-Key"})
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL, Auth: true},
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-API-Key", "my-key")
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPIGateway_CORS(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	gw.SetCORS(&CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Authorization"},
		MaxAge:       3600,
	})
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL},
	})

	// Preflight request
	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected CORS origin header")
	}
	if w.Header().Get("Access-Control-Max-Age") != "3600" {
		t.Errorf("expected max-age 3600, got %q", w.Header().Get("Access-Control-Max-Age"))
	}
}

func TestAPIGateway_InstanceRateLimit_SetRateLimit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	gw.SetRateLimit(&RateLimitConfig{RequestsPerMinute: 60, BurstSize: 2})
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL},
	})

	// First two should succeed (burst=2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		gw.Handle(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d expected 200, got %d", i+1, w.Code)
		}
	}

	// Third should be rate limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	gw.Handle(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestAPIGateway_InstanceRateLimit_WithRateLimit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw", WithRateLimit(&RateLimitConfig{RequestsPerMinute: 60, BurstSize: 1}))
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backend.URL},
	})

	// First should succeed (burst=1)
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	w := httptest.NewRecorder()
	gw.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("first request expected 200, got %d", w.Code)
	}

	// Second should be rate limited
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	w = httptest.NewRecorder()
	gw.Handle(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestAPIGateway_InstanceRateLimiters_AreIsolated(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &RateLimitConfig{RequestsPerMinute: 60, BurstSize: 1}
	gw1 := NewAPIGateway("gw1", WithRateLimit(cfg))
	gw2 := NewAPIGateway("gw2", WithRateLimit(cfg))
	_ = gw1.SetRoutes([]GatewayRoute{{PathPrefix: "/api", Backend: backend.URL}})
	_ = gw2.SetRoutes([]GatewayRoute{{PathPrefix: "/api", Backend: backend.URL}})

	// Exhaust gw1's burst for this client
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	w := httptest.NewRecorder()
	gw1.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("gw1 first request expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	w = httptest.NewRecorder()
	gw1.Handle(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("gw1 second request expected 429, got %d", w.Code)
	}

	// gw2 should be unaffected — its burst is independent
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.3:1234"
	w = httptest.NewRecorder()
	gw2.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("gw2 should be isolated from gw1; expected 200, got %d", w.Code)
	}
}

func TestAPIGateway_PerRouteRateLimit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := NewAPIGateway("gw")
	_ = gw.SetRoutes([]GatewayRoute{
		{
			PathPrefix: "/api",
			Backend:    backend.URL,
			RateLimit:  &RateLimitConfig{RequestsPerMinute: 60, BurstSize: 1},
		},
	})

	// First succeeds
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.1:5678"
	w := httptest.NewRecorder()
	gw.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("first request expected 200, got %d", w.Code)
	}

	// Second rate limited
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.1:5678"
	w = httptest.NewRecorder()
	gw.Handle(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestAPIGateway_LongestPrefixMatch(t *testing.T) {
	var receivedBackend string
	backendShort := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		receivedBackend = "short"
		w.WriteHeader(http.StatusOK)
	}))
	defer backendShort.Close()

	backendLong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		receivedBackend = "long"
		w.WriteHeader(http.StatusOK)
	}))
	defer backendLong.Close()

	gw := NewAPIGateway("gw")
	_ = gw.SetRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: backendShort.URL},
		{PathPrefix: "/api/v2", Backend: backendLong.URL},
	})

	req := httptest.NewRequest("GET", "/api/v2/users", nil)
	w := httptest.NewRecorder()
	gw.Handle(w, req)

	if receivedBackend != "long" {
		t.Errorf("expected longest prefix match to 'long', got %q", receivedBackend)
	}
}

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remote   string
		expected string
	}{
		{
			name:     "X-Forwarded-For",
			headers:  map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
			remote:   "9.10.11.12:1234",
			expected: "1.2.3.4",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "1.2.3.4"},
			remote:   "9.10.11.12:1234",
			expected: "1.2.3.4",
		},
		{
			name:     "RemoteAddr with port",
			headers:  map[string]string{},
			remote:   "192.168.1.1:5678",
			expected: "192.168.1.1",
		},
		{
			name:     "RemoteAddr without port",
			headers:  map[string]string{},
			remote:   "192.168.1.1",
			expected: "192.168.1.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			got := extractClientIP(req)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestGatewayTimeout(t *testing.T) {
	tests := []struct {
		timeout  string
		def      string
		expected string
	}{
		{"10s", "30s", "10s"},
		{"", "30s", "30s"},
		{"invalid", "30s", "30s"},
	}

	for _, tc := range tests {
		route := &GatewayRoute{Timeout: tc.timeout}
		defDur, _ := time.ParseDuration(tc.def)
		expected, _ := time.ParseDuration(tc.expected)
		got := GatewayTimeout(route, defDur)
		if got != expected {
			t.Errorf("GatewayTimeout(%q, %v) = %v, want %v", tc.timeout, defDur, got, expected)
		}
	}
}

func TestAWSAPIGateway_Basic(t *testing.T) {
	aws := NewAWSAPIGateway("aws-gw")
	if aws.Name() != "aws-gw" {
		t.Errorf("expected name %q, got %q", "aws-gw", aws.Name())
	}

	aws.SetConfig("us-east-1", "abc123", "prod")
	if aws.Region() != "us-east-1" {
		t.Errorf("expected region %q, got %q", "us-east-1", aws.Region())
	}
	if aws.APIID() != "abc123" {
		t.Errorf("expected api_id %q, got %q", "abc123", aws.APIID())
	}
	if aws.Stage() != "prod" {
		t.Errorf("expected stage %q, got %q", "prod", aws.Stage())
	}
}

func TestAWSAPIGateway_SyncRoutesStub(t *testing.T) {
	t.Skip("requires real AWS credentials and API Gateway")
	aws := NewAWSAPIGateway("aws-gw")
	aws.SetConfig("us-east-1", "abc123", "prod")

	err := aws.SyncRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: "http://localhost:8080", Methods: []string{"GET"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAWSAPIGateway_SyncRoutesRequiresAPIID(t *testing.T) {
	aws := NewAWSAPIGateway("aws-gw")
	// Don't set api_id

	err := aws.SyncRoutes([]GatewayRoute{
		{PathPrefix: "/api", Backend: "http://localhost:8080"},
	})
	if err == nil {
		t.Fatal("expected error when api_id is empty")
	}
}
