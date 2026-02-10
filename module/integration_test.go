package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPIntegrationConnector(t *testing.T) {
	c := NewHTTPIntegrationConnector("test-api", "http://example.com")
	if c.GetName() != "test-api" {
		t.Errorf("expected name 'test-api', got %q", c.GetName())
	}
	if c.IsConnected() {
		t.Error("expected not connected initially")
	}
	if c.baseURL != "http://example.com" {
		t.Errorf("expected baseURL 'http://example.com', got %q", c.baseURL)
	}
	if c.authType != "none" {
		t.Errorf("expected authType 'none', got %q", c.authType)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", c.timeout)
	}
}

func TestHTTPIntegrationConnector_SetBasicAuth(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	c.SetBasicAuth("user", "pass")
	if c.authType != "basic" {
		t.Errorf("expected authType 'basic', got %q", c.authType)
	}
	if c.username != "user" {
		t.Errorf("expected username 'user', got %q", c.username)
	}
	if c.password != "pass" {
		t.Errorf("expected password 'pass', got %q", c.password)
	}
}

func TestHTTPIntegrationConnector_SetBearerAuth(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	c.SetBearerAuth("my-token")
	if c.authType != "bearer" {
		t.Errorf("expected authType 'bearer', got %q", c.authType)
	}
	if c.authToken != "my-token" {
		t.Errorf("expected authToken 'my-token', got %q", c.authToken)
	}
}

func TestHTTPIntegrationConnector_SetHeader(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	c.SetHeader("X-Custom", "value1")
	c.SetDefaultHeader("X-Other", "value2")
	if c.headers["X-Custom"] != "value1" {
		t.Errorf("expected header X-Custom=value1, got %q", c.headers["X-Custom"])
	}
	if c.headers["X-Other"] != "value2" {
		t.Errorf("expected header X-Other=value2, got %q", c.headers["X-Other"])
	}
}

func TestHTTPIntegrationConnector_SetTimeout(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	c.SetTimeout(5 * time.Second)
	if c.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", c.timeout)
	}
}

func TestHTTPIntegrationConnector_SetRateLimit(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	c.SetRateLimit(60)
	if c.rateLimiter == nil {
		t.Fatal("expected rateLimiter to be set")
	}
	c.rateLimiter.Stop()

	// Test zero/negative value
	c.SetRateLimit(0)
	if c.rateLimiter == nil {
		t.Fatal("expected rateLimiter to be set even for 0 rate")
	}
	c.rateLimiter.Stop()
}

func TestHTTPIntegrationConnector_ConnectDisconnect(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	ctx := context.Background()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if !c.IsConnected() {
		t.Error("expected connected after Connect")
	}
	// Connect should set default Content-Type
	if c.headers["Content-Type"] != "application/json" {
		t.Errorf("expected default Content-Type header, got %q", c.headers["Content-Type"])
	}

	if err := c.Disconnect(ctx); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}
	if c.IsConnected() {
		t.Error("expected not connected after Disconnect")
	}
}

func TestHTTPIntegrationConnector_DisconnectWithRateLimiter(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	ctx := context.Background()
	c.SetRateLimit(60)

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if err := c.Disconnect(ctx); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}
	if c.rateLimiter != nil {
		t.Error("expected rateLimiter to be nil after Disconnect")
	}
}

func TestHTTPIntegrationConnector_ExecuteNotConnected(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	ctx := context.Background()

	_, err := c.Execute(ctx, "GET /test", nil)
	if err == nil || err.Error() != "connector not connected" {
		t.Errorf("expected 'connector not connected' error, got %v", err)
	}
}

func TestHTTPIntegrationConnector_ExecuteInvalidAction(t *testing.T) {
	c := NewHTTPIntegrationConnector("test", "http://example.com")
	ctx := context.Background()
	_ = c.Connect(ctx)

	_, err := c.Execute(ctx, "invalid", nil)
	if err == nil {
		t.Fatal("expected error for invalid action format")
	}
}

func TestHTTPIntegrationConnector_ExecuteGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		// Verify query params
		if r.URL.Query().Get("key") != "value" {
			t.Errorf("expected query param key=value, got %q", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"result": "ok"})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "GET /data", map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("expected result 'ok', got %v", result["result"])
	}
	if result["statusCode"] != 200 {
		t.Errorf("expected statusCode 200, got %v", result["statusCode"])
	}
}

func TestHTTPIntegrationConnector_ExecutePOST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "123", "name": body["name"]})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "POST /items", map[string]interface{}{"name": "test-item"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["id"] != "123" {
		t.Errorf("expected id '123', got %v", result["id"])
	}
}

func TestHTTPIntegrationConnector_ExecuteWithBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"auth": "ok"})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	c.SetBasicAuth("admin", "secret")
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "GET /secure", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["auth"] != "ok" {
		t.Errorf("expected auth ok, got %v", result["auth"])
	}
}

func TestHTTPIntegrationConnector_ExecuteWithBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"auth": "bearer-ok"})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	c.SetBearerAuth("my-token")
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "GET /secure", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["auth"] != "bearer-ok" {
		t.Errorf("expected auth bearer-ok, got %v", result["auth"])
	}
}

func TestHTTPIntegrationConnector_ExecuteCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "my-value" {
			t.Errorf("expected X-Custom header, got %q", r.Header.Get("X-Custom"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	c.SetHeader("X-Custom", "my-value")
	ctx := context.Background()
	_ = c.Connect(ctx)

	_, err := c.Execute(ctx, "GET /test", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestHTTPIntegrationConnector_ExecuteErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "not found"})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "GET /missing", nil)
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
	if result["error"] != "not found" {
		t.Errorf("expected error body, got %v", result)
	}
}

func TestHTTPIntegrationConnector_ExecuteNonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain text response"))
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "GET /text", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["raw"] != "plain text response" {
		t.Errorf("expected raw response, got %v", result["raw"])
	}
}

func TestHTTPIntegrationConnector_ExecuteEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test", server.URL)
	ctx := context.Background()
	_ = c.Connect(ctx)

	result, err := c.Execute(ctx, "DELETE /item", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["statusCode"] != 204 {
		t.Errorf("expected statusCode 204, got %v", result["statusCode"])
	}
}

// -- WebhookIntegrationConnector tests --

func TestNewWebhookIntegrationConnector(t *testing.T) {
	c := NewWebhookIntegrationConnector("webhook", "/hooks/test", 0)
	if c.GetName() != "webhook" {
		t.Errorf("expected name 'webhook', got %q", c.GetName())
	}
	if c.path != "/hooks/test" {
		t.Errorf("expected path '/hooks/test', got %q", c.path)
	}
	if c.IsConnected() {
		t.Error("expected not connected initially")
	}
}

func TestNewWebhookIntegrationConnector_PathPrefix(t *testing.T) {
	c := NewWebhookIntegrationConnector("webhook", "hooks/test", 0)
	if c.path != "/hooks/test" {
		t.Errorf("expected path with leading slash, got %q", c.path)
	}
}

func TestWebhookIntegrationConnector_Execute(t *testing.T) {
	c := NewWebhookIntegrationConnector("webhook", "/hooks", 0)
	result, err := c.Execute(context.Background(), "anything", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["status"] != "webhook connectors do not support active execution" {
		t.Errorf("expected passive status message, got %v", result)
	}
}

func TestWebhookIntegrationConnector_RegisterEventHandler(t *testing.T) {
	c := NewWebhookIntegrationConnector("webhook", "/hooks", 0)
	called := false
	c.RegisterEventHandler("test.event", func(ctx context.Context, data map[string]interface{}) error {
		called = true
		return nil
	})
	if _, exists := c.handlers["test.event"]; !exists {
		t.Error("expected handler to be registered")
	}
	_ = called // handler not called yet
}

func TestWebhookIntegrationConnector_DisconnectNoServer(t *testing.T) {
	c := NewWebhookIntegrationConnector("webhook", "/hooks", 0)
	err := c.Disconnect(context.Background())
	if err != nil {
		t.Fatalf("Disconnect without server should not error, got %v", err)
	}
}

// -- StdIntegrationRegistry tests --

func TestNewIntegrationRegistry(t *testing.T) {
	r := NewIntegrationRegistry("test-registry")
	if r.Name() != "test-registry" {
		t.Errorf("expected name 'test-registry', got %q", r.Name())
	}
}

func TestStdIntegrationRegistry_RegisterAndGetConnector(t *testing.T) {
	r := NewIntegrationRegistry("registry")
	c := NewHTTPIntegrationConnector("api-conn", "http://example.com")
	r.RegisterConnector(c)

	got, err := r.GetConnector("api-conn")
	if err != nil {
		t.Fatalf("GetConnector failed: %v", err)
	}
	if got.GetName() != "api-conn" {
		t.Errorf("expected connector 'api-conn', got %q", got.GetName())
	}
}

func TestStdIntegrationRegistry_GetConnectorNotFound(t *testing.T) {
	r := NewIntegrationRegistry("registry")
	_, err := r.GetConnector("missing")
	if err == nil {
		t.Fatal("expected error for missing connector")
	}
}

func TestStdIntegrationRegistry_ListConnectors(t *testing.T) {
	r := NewIntegrationRegistry("registry")
	r.RegisterConnector(NewHTTPIntegrationConnector("a", "http://a.com"))
	r.RegisterConnector(NewHTTPIntegrationConnector("b", "http://b.com"))

	names := r.ListConnectors()
	if len(names) != 2 {
		t.Fatalf("expected 2 connectors, got %d", len(names))
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["a"] || !nameSet["b"] {
		t.Errorf("expected connectors a and b, got %v", names)
	}
}

func TestStdIntegrationRegistry_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	r := NewIntegrationRegistry("int-registry")
	if err := r.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestStdIntegrationRegistry_StartAndStop(t *testing.T) {
	r := NewIntegrationRegistry("registry")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	c := NewHTTPIntegrationConnector("test-conn", server.URL)
	r.RegisterConnector(c)

	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !c.IsConnected() {
		t.Error("expected connector to be connected after Start")
	}

	if err := r.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if c.IsConnected() {
		t.Error("expected connector to be disconnected after Stop")
	}
}

func TestStdIntegrationRegistry_StartFailure(t *testing.T) {
	r := NewIntegrationRegistry("registry")
	// A failing connector
	fc := &failingConnector{name: "fail-conn"}
	r.RegisterConnector(fc)

	err := r.Start()
	if err == nil {
		t.Fatal("expected error from Start with failing connector")
	}
}

type failingConnector struct {
	name string
}

func (c *failingConnector) Connect(ctx context.Context) error {
	return fmt.Errorf("connection refused")
}
func (c *failingConnector) Disconnect(ctx context.Context) error     { return nil }
func (c *failingConnector) GetName() string                          { return c.name }
func (c *failingConnector) IsConnected() bool                        { return false }
func (c *failingConnector) Execute(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("not connected")
}
