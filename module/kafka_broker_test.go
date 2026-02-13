package module

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKafkaBrokerName(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	if b.Name() != "kafka-test" {
		t.Errorf("expected name 'kafka-test', got %q", b.Name())
	}
}

func TestKafkaBrokerModuleInterface(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := b.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := b.ProvidesServices()
	if len(services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(services))
	}
	if services[0].Name != "kafka-test" {
		t.Errorf("expected service name 'kafka-test', got %q", services[0].Name)
	}
	if services[1].Name != "kafka-test.producer" {
		t.Errorf("expected service name 'kafka-test.producer', got %q", services[1].Name)
	}
	if services[2].Name != "kafka-test.consumer" {
		t.Errorf("expected service name 'kafka-test.consumer', got %q", services[2].Name)
	}

	// Test RequiresServices
	deps := b.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestKafkaBrokerInterfaceCompliance(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Verify it implements MessageBroker
	var _ MessageBroker = b

	// Verify producer and consumer are non-nil
	if b.Producer() == nil {
		t.Error("Producer should not be nil")
	}
	if b.Consumer() == nil {
		t.Error("Consumer should not be nil")
	}
}

func TestKafkaBrokerConfig(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Test defaults
	if len(b.brokers) != 1 || b.brokers[0] != "localhost:9092" {
		t.Errorf("expected default brokers ['localhost:9092'], got %v", b.brokers)
	}
	if b.groupID != "workflow-group" {
		t.Errorf("expected default groupID 'workflow-group', got %q", b.groupID)
	}

	// Test setters
	b.SetBrokers([]string{"broker1:9092", "broker2:9092"})
	if len(b.brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(b.brokers))
	}

	b.SetGroupID("custom-group")
	if b.groupID != "custom-group" {
		t.Errorf("expected groupID 'custom-group', got %q", b.groupID)
	}
}

func TestKafkaBrokerSubscribeBeforeStart(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	app, _ := NewTestApplication()
	_ = b.Init(app)

	handler := &SimpleMessageHandler{name: "test", logger: &noopLogger{}}

	err := b.Subscribe("test-topic", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if _, ok := b.handlers["test-topic"]; !ok {
		t.Error("handler should be registered in handlers map")
	}
}

func TestKafkaBrokerProducerWithoutStart(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	err := b.Producer().SendMessage("test", []byte("hello"))
	if err == nil {
		t.Error("expected error when sending without connection")
	}
}

func TestKafkaBrokerUnsubscribe(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	app, _ := NewTestApplication()
	_ = b.Init(app)

	handler := &SimpleMessageHandler{name: "test", logger: &noopLogger{}}
	_ = b.Subscribe("test-topic", handler)

	err := b.Consumer().Unsubscribe("test-topic")
	if err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	if _, ok := b.handlers["test-topic"]; ok {
		t.Error("handler should be removed after unsubscribe")
	}
}

func TestKafkaBrokerHealthStatus_Default(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Before Start, broker should be unhealthy (zero value = false)
	result := b.HealthStatus()
	if result.Status != "degraded" {
		t.Errorf("expected status 'degraded' before start, got %q", result.Status)
	}
}

func TestKafkaBrokerHealthStatus_SetHealthy(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	b.setHealthy("connected")

	result := b.HealthStatus()
	if result.Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", result.Status)
	}
	if result.Message != "connected" {
		t.Errorf("expected message 'connected', got %q", result.Message)
	}
}

func TestKafkaBrokerHealthStatus_SetUnhealthy(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	b.setHealthy("connected")
	b.setUnhealthy("consumer error: connection refused")

	result := b.HealthStatus()
	if result.Status != "degraded" {
		t.Errorf("expected status 'degraded', got %q", result.Status)
	}
	if result.Message != "consumer error: connection refused" {
		t.Errorf("expected error message, got %q", result.Message)
	}
}

func TestKafkaBrokerImplementsHealthCheckable(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	var _ HealthCheckable = b
}

func TestKafkaBrokerHealthDiscoveredByHealthChecker(t *testing.T) {
	app, _ := NewTestApplication()

	// Create and init broker
	b := NewKafkaBroker("event-broker")
	if err := b.Init(app); err != nil {
		t.Fatalf("broker Init failed: %v", err)
	}
	// Register services manually (normally done by framework)
	for _, svc := range b.ProvidesServices() {
		if err := app.RegisterService(svc.Name, svc.Instance); err != nil {
			t.Fatalf("RegisterService %q failed: %v", svc.Name, err)
		}
	}

	// Create and init health checker
	hc := NewHealthChecker("test-health")
	if err := hc.Init(app); err != nil {
		t.Fatalf("health checker Init failed: %v", err)
	}

	// Discover health-checkable services
	hc.DiscoverHealthCheckables()

	// Broker is not started, so should be degraded
	handler := hc.HealthHandler()
	req := httpTestRequest(t, "GET", "/health")
	rec := httpTestRecorder()
	handler.ServeHTTP(rec, req)

	resp := decodeJSONResponse(t, rec)
	if resp["status"] != "degraded" {
		t.Errorf("expected overall status 'degraded' when kafka not started, got %v", resp["status"])
	}

	// Now mark broker healthy
	b.setHealthy("connected")

	rec = httpTestRecorder()
	handler.ServeHTTP(rec, httpTestRequest(t, "GET", "/health"))
	resp = decodeJSONResponse(t, rec)

	if resp["status"] != "healthy" {
		t.Errorf("expected overall status 'healthy' when kafka connected, got %v", resp["status"])
	}

	// Verify kafka check appears in checks
	checks, ok := resp["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'checks' field in response")
	}
	// The broker registers with its name "event-broker" as the service name
	found := false
	for name := range checks {
		if name == "event-broker" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'event-broker' in health checks, got keys: %v", checks)
	}
}

// httpTestRequest creates an httptest.NewRequest, only importing is needed at top
func httpTestRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest(method, path, nil)
}

// httpTestRecorder creates an httptest.NewRecorder
func httpTestRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

// decodeJSONResponse decodes the recorder body into a map
func decodeJSONResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}
