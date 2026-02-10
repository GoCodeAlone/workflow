package workflow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

// TestE2E_Middleware_Auth verifies that the auth middleware blocks unauthenticated
// requests with 401 and allows requests with a valid Bearer token through.
func TestE2E_Middleware_Auth(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "auth-server", Type: "http.server", Config: map[string]interface{}{"address": addr}},
			{Name: "auth-router", Type: "http.router", DependsOn: []string{"auth-server"}},
			{Name: "auth-handler", Type: "http.handler", DependsOn: []string{"auth-router"}, Config: map[string]interface{}{"contentType": "application/json"}},
			{Name: "auth-mw", Type: "http.middleware.auth", Config: map[string]interface{}{"authType": "Bearer"}},
		},
		Workflows: map[string]interface{}{
			"http": map[string]interface{}{
				"server": "auth-server",
				"router": "auth-router",
				"routes": []interface{}{
					map[string]interface{}{
						"method":      "GET",
						"path":        "/api/protected",
						"handler":     "auth-handler",
						"middlewares": []interface{}{"auth-mw"},
					},
				},
			},
		},
		Triggers: map[string]interface{}{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	// Add a valid token provider to the auth middleware after build
	var authMW *module.AuthMiddleware
	for _, svc := range app.SvcRegistry() {
		if mw, ok := svc.(*module.AuthMiddleware); ok {
			authMW = mw
			break
		}
	}
	if authMW == nil {
		t.Fatal("AuthMiddleware not found in service registry")
	}
	authMW.AddProvider(map[string]map[string]interface{}{
		"valid-test-token": {"user": "testuser", "role": "admin"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)
	client := &http.Client{Timeout: 5 * time.Second}

	// Subtest 1: No Authorization header -> 401
	t.Run("no_auth_header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/protected", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
	})

	// Subtest 2: Wrong auth type -> 401
	t.Run("wrong_auth_type", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/protected", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
	})

	// Subtest 3: Invalid token -> 401
	t.Run("invalid_token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/protected", nil)
		req.Header.Set("Authorization", "Bearer bogus-token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
	})

	// Subtest 4: Valid token -> 200
	t.Run("valid_token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/protected", nil)
		req.Header.Set("Authorization", "Bearer valid-test-token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Log("E2E Middleware Auth: All auth scenarios verified")
}

// TestE2E_Middleware_RateLimit verifies rate limiting returns 429 after
// exhausting the burst allowance.
func TestE2E_Middleware_RateLimit(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	burstSize := 3

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "rl-server", Type: "http.server", Config: map[string]interface{}{"address": addr}},
			{Name: "rl-router", Type: "http.router", DependsOn: []string{"rl-server"}},
			{Name: "rl-handler", Type: "http.handler", DependsOn: []string{"rl-router"}, Config: map[string]interface{}{"contentType": "application/json"}},
			{Name: "rl-mw", Type: "http.middleware.ratelimit", Config: map[string]interface{}{
				"requestsPerMinute": float64(60),
				"burstSize":         float64(burstSize),
			}},
		},
		Workflows: map[string]interface{}{
			"http": map[string]interface{}{
				"server": "rl-server",
				"router": "rl-router",
				"routes": []interface{}{
					map[string]interface{}{
						"method":      "GET",
						"path":        "/api/limited",
						"handler":     "rl-handler",
						"middlewares": []interface{}{"rl-mw"},
					},
				},
			},
		},
		Triggers: map[string]interface{}{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Send burstSize+1 requests. The first burstSize should succeed (200),
	// the next should be rate-limited (429).
	for i := 0; i < burstSize; i++ {
		resp, err := client.Get(baseURL + "/api/limited")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}

	// Next request should be rate limited
	resp, err := client.Get(baseURL + "/api/limited")
	if err != nil {
		t.Fatalf("Request %d failed: %v", burstSize, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Request %d: expected 429, got %d", burstSize, resp.StatusCode)
	} else {
		t.Logf("  Got 429 on request %d (burst=%d)", burstSize, burstSize)
	}

	t.Logf("E2E Middleware RateLimit: Rate limiting verified with burstSize=%d", burstSize)
}

// TestE2E_Middleware_CORS verifies CORS headers on both preflight OPTIONS
// and regular GET requests through a real HTTP server.
func TestE2E_Middleware_CORS(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "cors-server", Type: "http.server", Config: map[string]interface{}{"address": addr}},
			{Name: "cors-router", Type: "http.router", DependsOn: []string{"cors-server"}},
			{Name: "cors-handler", Type: "http.handler", DependsOn: []string{"cors-router"}, Config: map[string]interface{}{"contentType": "application/json"}},
			{Name: "cors-mw", Type: "http.middleware.cors", Config: map[string]interface{}{
				"allowedOrigins": []interface{}{"http://allowed.example.com"},
				"allowedMethods": []interface{}{"GET", "POST"},
			}},
		},
		Workflows: map[string]interface{}{
			"http": map[string]interface{}{
				"server": "cors-server",
				"router": "cors-router",
				"routes": []interface{}{
					map[string]interface{}{
						"method":      "GET",
						"path":        "/api/cors-test",
						"handler":     "cors-handler",
						"middlewares": []interface{}{"cors-mw"},
					},
				},
			},
		},
		Triggers: map[string]interface{}{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)
	client := &http.Client{Timeout: 5 * time.Second}

	// Subtest 1: GET with allowed origin - CORS headers present
	t.Run("allowed_origin", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/cors-test", nil)
		req.Header.Set("Origin", "http://allowed.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		if acao != "http://allowed.example.com" {
			t.Errorf("Expected Access-Control-Allow-Origin 'http://allowed.example.com', got %q", acao)
		}
		acam := resp.Header.Get("Access-Control-Allow-Methods")
		if acam != "GET, POST" {
			t.Errorf("Expected Access-Control-Allow-Methods 'GET, POST', got %q", acam)
		}
	})

	// Subtest 2: GET with disallowed origin - no CORS headers
	t.Run("disallowed_origin", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/cors-test", nil)
		req.Header.Set("Origin", "http://evil.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		if acao != "" {
			t.Errorf("Expected no Access-Control-Allow-Origin header, got %q", acao)
		}
	})

	// Subtest 3: Verify Access-Control-Allow-Headers is set for allowed origin
	t.Run("allowed_headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/cors-test", nil)
		req.Header.Set("Origin", "http://allowed.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		acah := resp.Header.Get("Access-Control-Allow-Headers")
		if acah != "Content-Type, Authorization" {
			t.Errorf("Expected Access-Control-Allow-Headers 'Content-Type, Authorization', got %q", acah)
		}
	})

	// Subtest 4: OPTIONS preflight on a dedicated OPTIONS route
	// The standard router registers routes per method, so an OPTIONS preflight
	// must be registered separately. We test that CORS + preflight works when
	// the route accepts OPTIONS.
	t.Run("preflight_with_options_route", func(t *testing.T) {
		// Set up a second server with an OPTIONS route to test preflight
		pfPort := getFreePort(t)
		pfAddr := fmt.Sprintf(":%d", pfPort)
		pfBaseURL := fmt.Sprintf("http://127.0.0.1:%d", pfPort)

		pfCfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "pf-server", Type: "http.server", Config: map[string]interface{}{"address": pfAddr}},
				{Name: "pf-router", Type: "http.router", DependsOn: []string{"pf-server"}},
				{Name: "pf-handler", Type: "http.handler", DependsOn: []string{"pf-router"}, Config: map[string]interface{}{"contentType": "application/json"}},
				{Name: "pf-cors", Type: "http.middleware.cors", Config: map[string]interface{}{
					"allowedOrigins": []interface{}{"http://allowed.example.com"},
					"allowedMethods": []interface{}{"GET", "POST", "OPTIONS"},
				}},
			},
			Workflows: map[string]interface{}{
				"http": map[string]interface{}{
					"server": "pf-server",
					"router": "pf-router",
					"routes": []interface{}{
						map[string]interface{}{
							"method":      "GET",
							"path":        "/api/pf-test",
							"handler":     "pf-handler",
							"middlewares": []interface{}{"pf-cors"},
						},
						map[string]interface{}{
							"method":      "OPTIONS",
							"path":        "/api/pf-test",
							"handler":     "pf-handler",
							"middlewares": []interface{}{"pf-cors"},
						},
					},
				},
			},
			Triggers: map[string]interface{}{},
		}

		pfLogger := &mockLogger{}
		pfApp := modular.NewStdApplication(modular.NewStdConfigProvider(nil), pfLogger)
		pfEngine := NewStdEngine(pfApp, pfLogger)
		pfEngine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

		if err := pfEngine.BuildFromConfig(pfCfg); err != nil {
			t.Fatalf("BuildFromConfig failed: %v", err)
		}

		pfCtx, pfCancel := context.WithCancel(context.Background())
		defer pfCancel()

		if err := pfEngine.Start(pfCtx); err != nil {
			t.Fatalf("Engine start failed: %v", err)
		}
		defer pfEngine.Stop(context.Background())

		waitForServer(t, pfBaseURL, 5*time.Second)

		pfClient := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("OPTIONS", pfBaseURL+"/api/pf-test", nil)
		req.Header.Set("Origin", "http://allowed.example.com")
		resp, err := pfClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 for preflight, got %d", resp.StatusCode)
		}
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		if acao != "http://allowed.example.com" {
			t.Errorf("Expected CORS origin header on preflight, got %q", acao)
		}
	})

	t.Log("E2E Middleware CORS: Allowed, disallowed, headers, and preflight scenarios verified")
}

// TestE2E_Middleware_RequestID verifies the RequestID middleware adds an
// X-Request-ID header to every response, and preserves a client-supplied one.
func TestE2E_Middleware_RequestID(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// RequestIDMiddleware does not implement the HTTPMiddleware.Process interface,
	// so it cannot be wired through the normal route-middleware config path.
	// Instead, we set up a server manually and wrap the handler using its
	// Middleware() method to prove it works in a real HTTP round-trip.
	reqIDMW := module.NewRequestIDMiddleware("reqid-mw")

	simpleHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reqID := module.GetRequestID(r.Context())
		fmt.Fprintf(w, `{"requestId":%q}`, reqID)
	})

	handler := reqIDMW.Middleware()(simpleHandler)

	mux := http.NewServeMux()
	mux.Handle("GET /api/reqid", handler)
	server := &http.Server{Addr: addr, Handler: mux}
	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	waitForServer(t, baseURL, 5*time.Second)
	client := &http.Client{Timeout: 5 * time.Second}

	// Subtest 1: No X-Request-ID sent - server generates one
	t.Run("generated_id", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/api/reqid")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		reqID := resp.Header.Get("X-Request-ID")
		if reqID == "" {
			t.Error("Expected X-Request-ID header in response, got empty string")
		}
		t.Logf("  Generated X-Request-ID: %s", reqID)
	})

	// Subtest 2: Client sends X-Request-ID - server echoes it back
	t.Run("preserved_id", func(t *testing.T) {
		clientID := "client-supplied-id-12345"
		req, _ := http.NewRequest("GET", baseURL+"/api/reqid", nil)
		req.Header.Set("X-Request-ID", clientID)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		reqID := resp.Header.Get("X-Request-ID")
		if reqID != clientID {
			t.Errorf("Expected preserved request ID %q, got %q", clientID, reqID)
		}
	})

	t.Log("E2E Middleware RequestID: Generated and preserved ID scenarios verified")
}

// TestE2E_Middleware_FullChain wires auth, rate-limit, CORS, and logging
// middlewares into a single route and verifies they all cooperate.
func TestE2E_Middleware_FullChain(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	burstSize := 5

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "chain-server", Type: "http.server", Config: map[string]interface{}{"address": addr}},
			{Name: "chain-router", Type: "http.router", DependsOn: []string{"chain-server"}},
			{Name: "chain-handler", Type: "http.handler", DependsOn: []string{"chain-router"}, Config: map[string]interface{}{"contentType": "application/json"}},
			{Name: "chain-cors", Type: "http.middleware.cors", Config: map[string]interface{}{
				"allowedOrigins": []interface{}{"http://app.example.com"},
				"allowedMethods": []interface{}{"GET", "POST"},
			}},
			{Name: "chain-rl", Type: "http.middleware.ratelimit", Config: map[string]interface{}{
				"requestsPerMinute": float64(60),
				"burstSize":         float64(burstSize),
			}},
			{Name: "chain-auth", Type: "http.middleware.auth", Config: map[string]interface{}{"authType": "Bearer"}},
			{Name: "chain-log", Type: "http.middleware.logging", Config: map[string]interface{}{"logLevel": "info"}},
		},
		Workflows: map[string]interface{}{
			"http": map[string]interface{}{
				"server": "chain-server",
				"router": "chain-router",
				"routes": []interface{}{
					map[string]interface{}{
						"method":  "GET",
						"path":    "/api/chained",
						"handler": "chain-handler",
						// Order: CORS first (outermost), then rate-limit, then auth, then logging (innermost)
						"middlewares": []interface{}{"chain-cors", "chain-rl", "chain-auth", "chain-log"},
					},
				},
			},
		},
		Triggers: map[string]interface{}{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	// Add a valid token provider to auth middleware
	var authMW *module.AuthMiddleware
	for _, svc := range app.SvcRegistry() {
		if mw, ok := svc.(*module.AuthMiddleware); ok {
			authMW = mw
			break
		}
	}
	if authMW == nil {
		t.Fatal("AuthMiddleware not found in service registry")
	}
	authMW.AddProvider(map[string]map[string]interface{}{
		"chain-token": {"user": "chainuser"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. No auth + no origin -> CORS headers missing, 401 from auth
	t.Run("no_auth_no_origin", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/chained", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401 (auth blocks), got %d", resp.StatusCode)
		}
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		if acao != "" {
			t.Errorf("Expected no CORS header (no Origin sent), got %q", acao)
		}
	})

	// 2. Valid auth + allowed origin -> 200 with CORS headers
	t.Run("valid_auth_with_cors", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/chained", nil)
		req.Header.Set("Authorization", "Bearer chain-token")
		req.Header.Set("Origin", "http://app.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
		}
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		if acao != "http://app.example.com" {
			t.Errorf("Expected CORS origin header, got %q", acao)
		}
	})

	// 3. Exhaust rate limit, then verify 429 even with valid auth.
	// Prior subtests already consumed some tokens from the same IP.
	// Send remaining + 1 more to trigger 429.
	t.Run("rate_limit_after_burst", func(t *testing.T) {
		var got429 bool
		for i := 0; i < burstSize+5; i++ {
			req, _ := http.NewRequest("GET", baseURL+"/api/chained", nil)
			req.Header.Set("Authorization", "Bearer chain-token")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				got429 = true
				t.Logf("  Got 429 on request %d (burst=%d)", i, burstSize)
				break
			}
		}
		if !got429 {
			t.Errorf("Expected 429 after exhausting burst=%d tokens", burstSize)
		}
	})

	t.Log("E2E Middleware FullChain: CORS + RateLimit + Auth + Logging all cooperating")
}
