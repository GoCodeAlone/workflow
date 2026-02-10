package handlers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// mockApp implements modular.Application with real GetService support
type mockApp struct {
	services       map[string]interface{}
	config         modular.ConfigProvider
	logger         modular.Logger
	configSections map[string]modular.ConfigProvider
}

func newMockApp() *mockApp {
	return &mockApp{
		services:       make(map[string]interface{}),
		config:         &mock.ConfigProvider{ConfigData: make(map[string]interface{})},
		logger:         &mock.Logger{LogEntries: make([]string, 0)},
		configSections: make(map[string]modular.ConfigProvider),
	}
}

func (a *mockApp) GetService(name string, target interface{}) error {
	svc, ok := a.services[name]
	if !ok {
		return nil
	}
	// Use reflect to set the target pointer
	targetVal := reflect.ValueOf(target)
	if targetVal.Kind() == reflect.Ptr && !targetVal.IsNil() {
		targetVal.Elem().Set(reflect.ValueOf(svc))
	}
	return nil
}

func (a *mockApp) RegisterService(name string, service interface{}) error {
	a.services[name] = service
	return nil
}

func (a *mockApp) SvcRegistry() modular.ServiceRegistry {
	return a.services
}

func (a *mockApp) Init() error                                                    { return nil }
func (a *mockApp) Start() error                                                   { return nil }
func (a *mockApp) Stop() error                                                    { return nil }
func (a *mockApp) Run() error                                                     { return nil }
func (a *mockApp) Logger() modular.Logger                                         { return a.logger }
func (a *mockApp) SetLogger(l modular.Logger)                                     { a.logger = l }
func (a *mockApp) ConfigProvider() modular.ConfigProvider                         { return a.config }
func (a *mockApp) ConfigSections() map[string]modular.ConfigProvider              { return a.configSections }
func (a *mockApp) RegisterConfigSection(s string, cp modular.ConfigProvider)      { a.configSections[s] = cp }
func (a *mockApp) GetConfigSection(s string) (modular.ConfigProvider, error)      { return a.configSections[s], nil }
func (a *mockApp) RegisterModule(m modular.Module)                                {}
func (a *mockApp) IsVerboseConfig() bool                                          { return false }
func (a *mockApp) SetVerboseConfig(bool)                                          {}
func (a *mockApp) GetServicesByModule(string) []string                            { return nil }
func (a *mockApp) GetServiceEntry(string) (*modular.ServiceRegistryEntry, bool)   { return nil, false }
func (a *mockApp) GetServicesByInterface(reflect.Type) []*modular.ServiceRegistryEntry { return nil }
func (a *mockApp) StartTime() time.Time                                           { return time.Time{} }
func (a *mockApp) GetModule(string) modular.Module                                { return nil }
func (a *mockApp) GetAllModules() map[string]modular.Module                       { return nil }
func (a *mockApp) OnConfigLoaded(func(modular.Application) error)                 {}

// controllableMockConnector is a mock connector with controllable behavior
type controllableMockConnector struct {
	name         string
	connected    bool
	connectErr   error
	executeErr   error
	executeCount int
	executeFn    func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error)
}

func (c *controllableMockConnector) GetName() string { return c.name }
func (c *controllableMockConnector) Connect(ctx context.Context) error {
	if c.connectErr != nil {
		return c.connectErr
	}
	c.connected = true
	return nil
}
func (c *controllableMockConnector) Disconnect(ctx context.Context) error {
	c.connected = false
	return nil
}
func (c *controllableMockConnector) IsConnected() bool { return c.connected }
func (c *controllableMockConnector) Execute(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	c.executeCount++
	if c.executeFn != nil {
		return c.executeFn(ctx, action, params)
	}
	if c.executeErr != nil {
		return nil, c.executeErr
	}
	return map[string]interface{}{"result": "ok"}, nil
}

// --- ConfigureWorkflow Tests ---

func TestConfigureWorkflow_HTTPConnectorWithBasicAuth(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL":           "http://example.com",
					"authType":          "basic",
					"username":          "user",
					"password":          "pass",
					"timeoutSeconds":    float64(10),
					"requestsPerMinute": float64(60),
					"headers": map[string]interface{}{
						"X-Custom": "value",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	conn, err := registry.GetConnector("api-conn")
	if err != nil {
		t.Fatalf("expected connector to be registered, got error: %v", err)
	}
	if conn.GetName() != "api-conn" {
		t.Errorf("expected name 'api-conn', got '%s'", conn.GetName())
	}
}

func TestConfigureWorkflow_HTTPConnectorWithBearerAuth(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "rest",
				"config": map[string]interface{}{
					"baseURL":  "http://example.com",
					"authType": "bearer",
					"token":    "my-token",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestConfigureWorkflow_HTTPConnectorTypeAPI(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "api",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestConfigureWorkflow_WebhookConnector(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "webhook-conn",
				"type": "webhook",
				"config": map[string]interface{}{
					"path": "/webhooks/test",
					"port": float64(9090),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	conn, err := registry.GetConnector("webhook-conn")
	if err != nil {
		t.Fatalf("expected connector to be registered: %v", err)
	}
	if conn.GetName() != "webhook-conn" {
		t.Errorf("expected name 'webhook-conn', got '%s'", conn.GetName())
	}
}

func TestConfigureWorkflow_WebhookConnectorDefaultPort(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "webhook-conn",
				"type": "webhook",
				"config": map[string]interface{}{
					"path": "/hook",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestConfigureWorkflow_DatabaseConnector(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "db-conn",
				"type": "database",
				"config": map[string]interface{}{
					"driver":       "sqlite3",
					"dsn":          ":memory:",
					"maxOpenConns": float64(10),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	conn, err := registry.GetConnector("db-conn")
	if err != nil {
		t.Fatalf("expected connector to be registered: %v", err)
	}
	if conn.GetName() != "db-conn" {
		t.Errorf("expected name 'db-conn', got '%s'", conn.GetName())
	}
}

func TestConfigureWorkflow_NotAMap(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	err := h.ConfigureWorkflow(app, "not-a-map")
	if err == nil || !strings.Contains(err.Error(), "invalid integration workflow configuration format") {
		t.Fatalf("expected invalid format error, got: %v", err)
	}
}

func TestConfigureWorkflow_MissingRegistryName(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	err := h.ConfigureWorkflow(app, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "registry name not specified") {
		t.Fatalf("expected missing registry error, got: %v", err)
	}
}

func TestConfigureWorkflow_RegistryServiceNotFound(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "missing-registry",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got: %v", err)
	}
}

func TestConfigureWorkflow_ServiceNotIntegrationRegistry(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	app.services["bad-registry"] = "not-a-registry"
	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "bad-registry",
	})
	if err == nil || !strings.Contains(err.Error(), "is not an IntegrationRegistry") {
		t.Fatalf("expected not IntegrationRegistry error, got: %v", err)
	}
}

func TestConfigureWorkflow_NoConnectors(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry":   "test-registry",
		"connectors": []interface{}{},
	})
	if err == nil || !strings.Contains(err.Error(), "no connectors defined") {
		t.Fatalf("expected no connectors error, got: %v", err)
	}
}

func TestConfigureWorkflow_MissingConnectorName(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"type": "http",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "connector name not specified") {
		t.Fatalf("expected missing name error, got: %v", err)
	}
}

func TestConfigureWorkflow_MissingConnectorType(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "my-conn",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "connector type not specified") {
		t.Fatalf("expected missing type error, got: %v", err)
	}
}

func TestConfigureWorkflow_UnsupportedConnectorType(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "my-conn",
				"type": "mqtt",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported connector type") {
		t.Fatalf("expected unsupported type error, got: %v", err)
	}
}

func TestConfigureWorkflow_MissingHTTPBaseURL(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name":   "api-conn",
				"type":   "http",
				"config": map[string]interface{}{},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "baseURL not specified") {
		t.Fatalf("expected missing baseURL error, got: %v", err)
	}
}

func TestConfigureWorkflow_MissingWebhookPath(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name":   "webhook-conn",
				"type":   "webhook",
				"config": map[string]interface{}{},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "path not specified") {
		t.Fatalf("expected missing path error, got: %v", err)
	}
}

func TestConfigureWorkflow_MissingDatabaseDriverDSN(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name":   "db-conn",
				"type":   "database",
				"config": map[string]interface{}{},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "driver and dsn must be specified") {
		t.Fatalf("expected missing driver/dsn error, got: %v", err)
	}
}

func TestConfigureWorkflow_InvalidConnectorConfig(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			"not-a-map",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid connector configuration") {
		t.Fatalf("expected invalid connector config error, got: %v", err)
	}
}

func TestConfigureWorkflow_StepsMissingName(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
		"steps": []interface{}{
			map[string]interface{}{},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "step name not specified") {
		t.Fatalf("expected missing step name error, got: %v", err)
	}
}

func TestConfigureWorkflow_StepsMissingConnector(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
		"steps": []interface{}{
			map[string]interface{}{
				"name": "step1",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "connector not specified for step") {
		t.Fatalf("expected missing connector error, got: %v", err)
	}
}

func TestConfigureWorkflow_StepsMissingAction(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
		"steps": []interface{}{
			map[string]interface{}{
				"name":      "step1",
				"connector": "api-conn",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "action not specified for step") {
		t.Fatalf("expected missing action error, got: %v", err)
	}
}

func TestConfigureWorkflow_StepsConnectorNotFound(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
		"steps": []interface{}{
			map[string]interface{}{
				"name":      "step1",
				"connector": "nonexistent",
				"action":    "GET /data",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "connector 'nonexistent' not found") {
		t.Fatalf("expected connector not found error, got: %v", err)
	}
}

func TestConfigureWorkflow_InvalidStepConfig(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
		"steps": []interface{}{
			"not-a-map",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid step configuration") {
		t.Fatalf("expected invalid step config error, got: %v", err)
	}
}

func TestConfigureWorkflow_ValidSteps(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"registry": "test-registry",
		"connectors": []interface{}{
			map[string]interface{}{
				"name": "api-conn",
				"type": "http",
				"config": map[string]interface{}{
					"baseURL": "http://example.com",
				},
			},
		},
		"steps": []interface{}{
			map[string]interface{}{
				"name":      "step1",
				"connector": "api-conn",
				"action":    "GET /data",
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- ExecuteIntegrationWorkflow Tests ---

func TestExecuteIntegrationWorkflow_RetrySuccess(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	callCount := 0
	conn := &controllableMockConnector{
		name:      "retry-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("temporary error")
			}
			return map[string]interface{}{"success": true}, nil
		},
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:       "retry-step",
			Connector:  "retry-conn",
			Action:     "do-something",
			RetryCount: 3,
			RetryDelay: "1ms",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	stepResult, ok := result["retry-step"].(map[string]interface{})
	if !ok {
		t.Fatal("expected retry-step result")
	}
	if stepResult["success"] != true {
		t.Errorf("expected success=true, got %v", stepResult["success"])
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
	}
}

func TestExecuteIntegrationWorkflow_RetryExhaustedWithOnError(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  true,
		executeErr: fmt.Errorf("permanent error"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:       "fail-step",
			Connector:  "fail-conn",
			Action:     "do-something",
			RetryCount: 2,
			RetryDelay: "1ms",
			OnError:    "continue",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("expected OnError to handle failure, got: %v", err)
	}
	if result["fail-step_error"] == nil {
		t.Error("expected fail-step_error in results")
	}
}

func TestExecuteIntegrationWorkflow_RetryExhaustedNoOnError(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  true,
		executeErr: fmt.Errorf("permanent error"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:       "fail-step",
			Connector:  "fail-conn",
			Action:     "do-something",
			RetryCount: 1,
			RetryDelay: "1ms",
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "error executing step 'fail-step'") {
		t.Errorf("expected step error message, got: %v", err)
	}
}

func TestExecuteIntegrationWorkflow_RetryDefaultDelay(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	callCount := 0
	conn := &controllableMockConnector{
		name:      "retry-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("temporary error")
			}
			return map[string]interface{}{"ok": true}, nil
		},
	}
	registry.RegisterConnector(conn)

	// Empty RetryDelay should use default
	steps := []IntegrationStep{
		{
			Name:       "retry-step",
			Connector:  "retry-conn",
			Action:     "do-something",
			RetryCount: 1,
			RetryDelay: "",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.ExecuteIntegrationWorkflow(ctx, registry, steps, nil)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
}

func TestExecuteIntegrationWorkflow_ContextCancelledDuringRetry(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  true,
		executeErr: fmt.Errorf("permanent error"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:       "cancel-step",
			Connector:  "fail-conn",
			Action:     "do-something",
			RetryCount: 10,
			RetryDelay: "10s",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to trigger context cancellation during retry wait
	cancel()

	_, err := h.ExecuteIntegrationWorkflow(ctx, registry, steps, nil)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestExecuteIntegrationWorkflow_ConnectFails(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  false,
		connectErr: fmt.Errorf("connection refused"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "fail-conn",
			Action:    "do-something",
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil || !strings.Contains(err.Error(), "connector not connected") {
		t.Fatalf("expected connect error, got: %v", err)
	}
}

func TestExecuteIntegrationWorkflow_InitialContextValues(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	conn := &controllableMockConnector{
		name:      "test-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-conn",
			Action:    "do-something",
		},
	}

	initialCtx := map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, initialCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key1"] != "value1" {
		t.Errorf("expected key1='value1', got '%v'", result["key1"])
	}
	if result["key2"] != "value2" {
		t.Errorf("expected key2='value2', got '%v'", result["key2"])
	}
}

func TestExecuteIntegrationWorkflow_VariableSubstitutionResolved(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	var capturedParams map[string]interface{}
	conn := &controllableMockConnector{
		name:      "test-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
			capturedParams = params
			return map[string]interface{}{"val": "resolved"}, nil
		},
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "test-conn",
			Action:    "action1",
		},
		{
			Name:      "step2",
			Connector: "test-conn",
			Action:    "action2",
			Input: map[string]interface{}{
				"ref":       "${step1}",
				"unresolved": "${nonexistent}",
				"static":    "plain",
				"short":     "${x",
				"number":    42,
			},
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// step1 result should have been substituted into step2's input
	if capturedParams["static"] != "plain" {
		t.Errorf("expected static='plain', got '%v'", capturedParams["static"])
	}
	// ${step1} should resolve to step1's result map
	if capturedParams["ref"] == nil {
		t.Error("expected ref to be resolved from step1 result")
	}
	// ${nonexistent} should remain as-is
	if capturedParams["unresolved"] != "${nonexistent}" {
		t.Errorf("expected unresolved to keep original value, got '%v'", capturedParams["unresolved"])
	}
	// short string "${x" should not be treated as variable (length <= 3)
	if capturedParams["short"] != "${x" {
		t.Errorf("expected short to be kept as-is, got '%v'", capturedParams["short"])
	}
	if capturedParams["number"] != 42 {
		t.Errorf("expected number=42, got '%v'", capturedParams["number"])
	}
	_ = result
}

func TestExecuteIntegrationWorkflow_MultiStepWithMock(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	conn := &controllableMockConnector{
		name:      "multi-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
			switch action {
			case "fetch":
				return map[string]interface{}{"data": "fetched"}, nil
			case "process":
				return map[string]interface{}{"processed": true}, nil
			default:
				return nil, fmt.Errorf("unknown action")
			}
		},
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{Name: "s1", Connector: "multi-conn", Action: "fetch"},
		{Name: "s2", Connector: "multi-conn", Action: "process", OnSuccess: "done"},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["s1"] == nil {
		t.Error("expected s1 result")
	}
	if result["s2"] == nil {
		t.Error("expected s2 result")
	}
}

// --- ExecuteWorkflow Tests ---

func TestExecuteWorkflow_WithSteps(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	conn := &controllableMockConnector{
		name:      "test-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]interface{}{
		"steps": []interface{}{
			map[string]interface{}{
				"connector": "test-conn",
				"action":    "do-something",
				"input":     map[string]interface{}{"key": "val"},
			},
		},
	}

	result, err := h.ExecuteWorkflow(ctx, "integration", "test-registry", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecuteWorkflow_SingleStepFromAction(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	conn := &controllableMockConnector{
		name:      "my-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]interface{}{
		"connector": "my-conn",
	}

	result, err := h.ExecuteWorkflow(ctx, "integration", "test-registry:do-action", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecuteWorkflow_RegistryNotFound(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "integration", "missing-registry", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected registry not found error, got: %v", err)
	}
}

func TestExecuteWorkflow_NotIntegrationRegistry(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	app.services["bad-svc"] = "not-a-registry"

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "integration", "bad-svc", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "is not an IntegrationRegistry") {
		t.Fatalf("expected not IntegrationRegistry error, got: %v", err)
	}
}

func TestExecuteWorkflow_NoStepsNoAction(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	// action is the same as registryName when no colon, so action becomes ""
	// after splitting. But here action == registryName == "test-registry" (no colon),
	// so action remains "test-registry" and there are no steps... it would try single step path
	// which needs "connector" in data. Let's test the "no steps and no action" path differently.
	// We need the colon-split to produce an empty action.
	_, err := h.ExecuteWorkflow(ctx, "integration", "test-registry:", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "no steps provided and no action specified") {
		t.Fatalf("expected no steps/action error, got: %v", err)
	}
}

func TestExecuteWorkflow_MissingConnectorInSingleStep(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]interface{}{
		// No "connector" key
	}

	_, err := h.ExecuteWorkflow(ctx, "integration", "test-registry:action", data)
	if err == nil || !strings.Contains(err.Error(), "connector not specified") {
		t.Fatalf("expected connector not specified error, got: %v", err)
	}
}

func TestExecuteWorkflow_StepsWithOptionalFields(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	conn := &controllableMockConnector{
		name:      "test-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]interface{}{
		"steps": []interface{}{
			map[string]interface{}{
				"connector":  "test-conn",
				"action":     "do-something",
				"input":      map[string]interface{}{"key": "val"},
				"transform":  "jq .data",
				"onSuccess":  "next",
				"onError":    "handle",
				"retryCount": float64(3),
				"retryDelay": "2s",
			},
		},
	}

	result, err := h.ExecuteWorkflow(ctx, "integration", "test-registry", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecuteWorkflow_ColonSeparatedAction(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("my-registry")
	app.services["my-registry"] = registry

	conn := &controllableMockConnector{
		name:      "conn1",
		connected: true,
	}
	registry.RegisterConnector(conn)

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]interface{}{
		"connector": "conn1",
	}

	result, err := h.ExecuteWorkflow(ctx, "integration", "my-registry:some-action", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
