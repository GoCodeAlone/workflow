package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHealthChecker(t *testing.T) {
	h := NewHealthChecker("test-health")
	if h.Name() != "test-health" {
		t.Errorf("expected name 'test-health', got %q", h.Name())
	}
}

func TestHealthChecker_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	h := NewHealthChecker("test-health")
	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestHealthChecker_HealthHandler_Healthy(t *testing.T) {
	h := NewHealthChecker("test-health")
	h.RegisterCheck("db", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "healthy", Message: "connected"}
	})

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", resp["status"])
	}
}

func TestHealthChecker_HealthHandler_Unhealthy(t *testing.T) {
	h := NewHealthChecker("test-health")
	h.RegisterCheck("db", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "unhealthy", Message: "connection lost"}
	})

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "unhealthy" {
		t.Errorf("expected status 'unhealthy', got %v", resp["status"])
	}
}

func TestHealthChecker_HealthHandler_Degraded(t *testing.T) {
	h := NewHealthChecker("test-health")
	h.RegisterCheck("db", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "healthy"}
	})
	h.RegisterCheck("cache", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "degraded", Message: "slow"}
	})

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "degraded" {
		t.Errorf("expected status 'degraded', got %v", resp["status"])
	}
}

func TestHealthChecker_ReadyHandler_NotStarted(t *testing.T) {
	h := NewHealthChecker("test-health")

	handler := h.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestHealthChecker_ReadyHandler_StartedHealthy(t *testing.T) {
	h := NewHealthChecker("test-health")
	h.SetStarted(true)
	h.RegisterCheck("db", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "healthy"}
	})

	handler := h.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ready" {
		t.Errorf("expected status 'ready', got %q", resp["status"])
	}
}

func TestHealthChecker_ReadyHandler_StartedUnhealthy(t *testing.T) {
	h := NewHealthChecker("test-health")
	h.SetStarted(true)
	h.RegisterCheck("db", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "unhealthy"}
	})

	handler := h.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestHealthChecker_LiveHandler(t *testing.T) {
	h := NewHealthChecker("test-health")

	handler := h.LiveHandler()
	req := httptest.NewRequest("GET", "/live", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "alive" {
		t.Errorf("expected status 'alive', got %q", resp["status"])
	}
}

func TestHealthChecker_ProvidesServices(t *testing.T) {
	h := NewHealthChecker("test-health")
	svcs := h.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "health.checker" {
		t.Errorf("expected service name 'health.checker', got %q", svcs[0].Name)
	}
}

func TestHealthChecker_RequiresServices(t *testing.T) {
	h := NewHealthChecker("test-health")
	deps := h.RequiresServices()
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestHealthChecker_PersistenceCheck_Healthy(t *testing.T) {
	h := NewHealthChecker("test-health")

	// Simulate registering a persistence check that succeeds
	h.RegisterCheck("persistence.store", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "healthy", Message: "database connected"}
	})

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", resp["status"])
	}

	checks := resp["checks"].(map[string]interface{})
	psCheck := checks["persistence.store"].(map[string]interface{})
	if psCheck["status"] != "healthy" {
		t.Errorf("expected persistence check status 'healthy', got %v", psCheck["status"])
	}
	if psCheck["message"] != "database connected" {
		t.Errorf("expected message 'database connected', got %v", psCheck["message"])
	}
}

func TestHealthChecker_PersistenceCheck_Degraded(t *testing.T) {
	h := NewHealthChecker("test-health")

	// Simulate registering a persistence check that fails (degraded, not unhealthy)
	h.RegisterCheck("persistence.store", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "degraded", Message: "database unreachable: connection refused"}
	})

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Degraded returns HTTP 200 (not 503) so Docker healthcheck passes
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for degraded, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "degraded" {
		t.Errorf("expected overall status 'degraded', got %v", resp["status"])
	}

	checks := resp["checks"].(map[string]interface{})
	psCheck := checks["persistence.store"].(map[string]interface{})
	if psCheck["status"] != "degraded" {
		t.Errorf("expected persistence check status 'degraded', got %v", psCheck["status"])
	}
}

func TestHealthChecker_PersistenceCheck_WithOtherChecks(t *testing.T) {
	h := NewHealthChecker("test-health")

	// Other check is healthy, persistence is degraded
	h.RegisterCheck("other", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "healthy"}
	})
	h.RegisterCheck("persistence.store", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Status: "degraded", Message: "database unreachable: timeout"}
	})

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Overall should be degraded because persistence is degraded
	if resp["status"] != "degraded" {
		t.Errorf("expected overall status 'degraded', got %v", resp["status"])
	}
}

func TestHealthChecker_HealthHandler_NoChecks(t *testing.T) {
	h := NewHealthChecker("test-health")

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Errorf("expected status 'healthy' with no checks, got %v", resp["status"])
	}
}

// mockHealthCheckable implements HealthCheckable for testing.
type mockHealthCheckable struct {
	status  string
	message string
}

func (m *mockHealthCheckable) HealthStatus() HealthCheckResult {
	return HealthCheckResult{Status: m.status, Message: m.message}
}

func TestHealthChecker_DiscoverHealthCheckables(t *testing.T) {
	app := CreateIsolatedApp(t)

	// Register a mock HealthCheckable service
	mock := &mockHealthCheckable{status: "healthy", message: "all good"}
	if err := app.RegisterService("my-service", mock); err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	h := NewHealthChecker("test-health")
	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	h.DiscoverHealthCheckables()

	// Check that the health handler now includes my-service
	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", resp["status"])
	}

	checks, ok := resp["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'checks' field in response")
	}
	if _, exists := checks["my-service"]; !exists {
		t.Errorf("expected 'my-service' in health checks, got: %v", checks)
	}
}

func TestHealthChecker_DiscoverHealthCheckables_Degraded(t *testing.T) {
	app := CreateIsolatedApp(t)

	mock := &mockHealthCheckable{status: "degraded", message: "connection lost"}
	if err := app.RegisterService("kafka-broker", mock); err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	h := NewHealthChecker("test-health")
	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	h.DiscoverHealthCheckables()

	handler := h.HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should still return 200 (degraded is not unhealthy)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for degraded status, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "degraded" {
		t.Errorf("expected overall status 'degraded', got %v", resp["status"])
	}
}

func TestHealthChecker_DiscoverHealthCheckables_NoApp(t *testing.T) {
	h := NewHealthChecker("test-health")
	// Should not panic when app is nil
	h.DiscoverHealthCheckables()
}
