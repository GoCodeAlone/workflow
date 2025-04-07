package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// Define the engine variable with the correct type
var engine *workflow.Engine

// IntegrationRegistry is an alias for module.IntegrationRegistry
type IntegrationRegistry = module.IntegrationRegistry

// TestIntegrationWorkflow tests the integration workflow handler
func TestIntegrationWorkflow(t *testing.T) {
	// Initialize the engine variable
	mockLogger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), mockLogger)
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Create workflow engine
	engine = workflow.NewEngine(app, mockLogger)

	// Register workflow handlers
	engine.RegisterWorkflowHandler(NewIntegrationWorkflowHandler())

	// Create mock HTTP server for testing the HTTP connector
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check request path
		if r.URL.Path == "/customers/123" {
			// Return mock customer data
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"123","name":"Test Customer","email":"test@example.com"}`))
			return
		}

		if r.URL.Path == "/send" {
			// Verify it's a POST request to the email endpoint
			if r.Method != "POST" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			// Parse the request body
			var emailReq map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&emailReq); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Return success
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"sent","id":"email-123"}`))
			return
		}

		// Default response for unknown paths
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// Create and configure the integration registry
	intRegistry := module.NewIntegrationRegistry("test-integration-registry")

	// Pre-create and connect the HTTP connectors with the mock server URL
	crmConnector := module.NewHTTPIntegrationConnector("crm-connector", mockServer.URL)
	emailConnector := module.NewHTTPIntegrationConnector("email-connector", mockServer.URL)

	// Set default headers
	crmConnector.SetDefaultHeader("Content-Type", "application/json")
	emailConnector.SetDefaultHeader("Content-Type", "application/json")

	// Connect the connectors BEFORE registering them
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := crmConnector.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect crm-connector: %v", err)
	}

	if err := emailConnector.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect email-connector: %v", err)
	}

	// Register the connectors with the registry
	intRegistry.RegisterConnector(crmConnector)
	intRegistry.RegisterConnector(emailConnector)

	// Register the registry with the application
	app.SvcRegistry()["test-integration-registry"] = intRegistry

	// Create the adapter for the module system
	registryAdapter := NewIntegrationRegistryAdapter(intRegistry, nil)
	app.RegisterModule(registryAdapter)

	// Create a minimal integration workflow configuration
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "test-integration-registry",
				Type: "integration.registry",
				Config: map[string]interface{}{
					"description": "Test integration registry",
				},
			},
		},
		Workflows: map[string]interface{}{
			"integration": map[string]interface{}{
				"registry": "test-integration-registry",
				"connectors": []interface{}{
					map[string]interface{}{
						"name": "crm-connector",
						"type": "http",
						"config": map[string]interface{}{
							"baseURL": mockServer.URL,
							"headers": map[string]interface{}{
								"Content-Type": "application/json",
							},
						},
					},
					map[string]interface{}{
						"name": "email-connector",
						"type": "http",
						"config": map[string]interface{}{
							"baseURL": mockServer.URL,
							"headers": map[string]interface{}{
								"Content-Type": "application/json",
							},
						},
					},
				},
				"steps": []interface{}{
					map[string]interface{}{
						"name":      "get-customer",
						"connector": "crm-connector",
						"action":    "GET /customers/123",
					},
					map[string]interface{}{
						"name":      "send-email",
						"connector": "email-connector",
						"action":    "POST /send",
						"input": map[string]interface{}{
							"to":       "${get-customer.email}",
							"subject":  "Test Email",
							"template": "welcome",
						},
					},
				},
			},
		},
	}

	// Add the integration registry module factory to the engine
	engine.AddModuleType("integration.registry", func(name string, config map[string]interface{}) modular.Module {
		// Return our already created and connected registry
		return registryAdapter
	})

	// Build workflow
	err = engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build workflow: %v", err)
	}

	// Start engine
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
	}

	// Execute the integration workflow
	handler := NewIntegrationWorkflowHandler()

	// Make sure our connectors are connected
	connectors := []string{"crm-connector", "email-connector"}
	for _, name := range connectors {
		connector, err := intRegistry.GetConnector(name)
		if err != nil {
			t.Fatalf("Failed to get connector %s: %v", name, err)
		}

		// Verify connector is registered and connected
		t.Logf("Connector %s is ready: %v", name, connector != nil)
	}

	// Now execute the workflow with our connected connectors
	results, err := handler.ExecuteIntegrationWorkflow(
		ctx,
		intRegistry,
		[]IntegrationStep{
			{
				Name:      "get-customer",
				Connector: "crm-connector",
				Action:    "GET /customers/123",
			},
			{
				Name:      "send-email",
				Connector: "email-connector",
				Action:    "POST /send",
				Input: map[string]interface{}{
					"to":       "test@example.com",
					"subject":  "Test Email",
					"template": "welcome",
				},
			},
		},
		map[string]interface{}{},
	)

	if err != nil {
		t.Fatalf("Failed to execute integration workflow: %v", err)
	}

	// Verify the results
	if results["get-customer"] == nil {
		t.Errorf("Expected get-customer result to be present")
	}

	if results["send-email"] == nil {
		t.Errorf("Expected send-email result to be present")
	}

	// Get the first step result and check details
	customerResult, ok := results["get-customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected customer result to be a map")
	}

	// Check customer data
	if customerResult["id"] != "123" {
		t.Errorf("Expected customer ID '123', got '%v'", customerResult["id"])
	}

	if customerResult["name"] != "Test Customer" {
		t.Errorf("Expected customer name 'Test Customer', got '%v'", customerResult["name"])
	}

	// Check email result
	emailResult, ok := results["send-email"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected email result to be a map")
	}

	if emailResult["status"] != "sent" {
		t.Errorf("Expected email status 'sent', got '%v'", emailResult["status"])
	}

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}
}

// integrationModuleAdapter adapts an integration handler for modular.Module interface
type integrationModuleAdapter struct {
	registry  *IntegrationWorkflowHandler
	namespace module.ModuleNamespaceProvider
}

// NewIntegrationModuleAdapter creates a new adapter with namespace support
func NewIntegrationModuleAdapter(registry *IntegrationWorkflowHandler, namespace module.ModuleNamespaceProvider) *integrationModuleAdapter {
	if namespace == nil {
		namespace = module.NewStandardNamespace("", "")
	}
	return &integrationModuleAdapter{
		registry:  registry,
		namespace: namespace,
	}
}

// Init initializes the adapter with the application
func (a *integrationModuleAdapter) Init(app modular.Application) error {
	// Pass the application's service registry directly
	return a.registry.Init(app.SvcRegistry())
}

func (a *integrationModuleAdapter) Name() string {
	return a.registry.Name()
}

func (a *integrationModuleAdapter) Start(ctx context.Context) error {
	return a.registry.Start(ctx)
}

func (a *integrationModuleAdapter) Stop(ctx context.Context) error {
	return a.registry.Stop(ctx)
}

func (a *integrationModuleAdapter) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        a.registry.Name(),
			Description: "Integration Workflow Handler",
			Instance:    a.registry,
		},
	}
}

func (a *integrationModuleAdapter) RequiresServices() []modular.ServiceDependency {
	return nil
}

// integrationRegistryAdapter adapts an integration registry for modular.Module interface
type integrationRegistryAdapter struct {
	registry  IntegrationRegistry
	namespace module.ModuleNamespaceProvider
}

// NewIntegrationRegistryAdapter creates a new adapter with namespace support
func NewIntegrationRegistryAdapter(registry IntegrationRegistry, namespace module.ModuleNamespaceProvider) *integrationRegistryAdapter {
	if namespace == nil {
		namespace = module.NewStandardNamespace("", "")
	}
	return &integrationRegistryAdapter{
		registry:  registry,
		namespace: namespace,
	}
}

// Init initializes the adapter with the application
func (a *integrationRegistryAdapter) Init(app modular.Application) error {
	return a.registry.Init(app.SvcRegistry())
}

func (a *integrationRegistryAdapter) Name() string {
	return a.registry.Name()
}

func (a *integrationRegistryAdapter) Start(ctx context.Context) error {
	return nil
}

func (a *integrationRegistryAdapter) Stop(ctx context.Context) error {
	return nil
}

func (a *integrationRegistryAdapter) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        a.registry.Name(),
			Description: "Integration Registry",
			Instance:    a.registry,
		},
	}
}

func (a *integrationRegistryAdapter) RequiresServices() []modular.ServiceDependency {
	return nil
}

// MockIntegrationRegistry is a mock implementation of module.IntegrationRegistry for testing
type MockIntegrationRegistry struct {
	GetIntegrationFn func(name string) (interface{}, error)
	registrations    map[string]module.IntegrationConnector
	name             string
}

// NewMockIntegrationRegistry creates a new mock integration registry
func NewMockIntegrationRegistry(name string) *MockIntegrationRegistry {
	return &MockIntegrationRegistry{
		name:          name,
		registrations: make(map[string]module.IntegrationConnector),
	}
}

// Name returns the name of the registry
func (r *MockIntegrationRegistry) Name() string {
	return r.name
}

// Init initializes the registry with the application
func (r *MockIntegrationRegistry) Init(app modular.Application) error {
	return app.RegisterService(r.name, r)
}

// Start implements both module.Module and module.IntegrationRegistry interfaces
func (r *MockIntegrationRegistry) Start(ctx context.Context) error {
	return nil
}

// Stop implements both module.Module and module.IntegrationRegistry interfaces
func (r *MockIntegrationRegistry) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices implements module.Module interface
func (r *MockIntegrationRegistry) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        r.name,
			Description: "Mock Integration Registry",
			Instance:    r,
		},
	}
}

// RequiresServices implements module.Module interface
func (r *MockIntegrationRegistry) RequiresServices() []modular.ServiceDependency {
	return nil
}

// GetIntegration returns an integration by name
func (r *MockIntegrationRegistry) GetIntegration(name string) (interface{}, error) {
	if r.GetIntegrationFn != nil {
		return r.GetIntegrationFn(name)
	}
	if integration, exists := r.registrations[name]; exists {
		return integration, nil
	}
	return nil, nil
}

// RegisterConnector adds a connector to the registry
func (r *MockIntegrationRegistry) RegisterConnector(connector module.IntegrationConnector) {
	r.registrations[connector.GetName()] = connector
}

// GetConnector retrieves a connector by name
func (r *MockIntegrationRegistry) GetConnector(name string) (module.IntegrationConnector, error) {
	// Create a mock connector for testing
	connector := &MockIntegrationConnector{
		name: name,
		executeFn: func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
			if action == "GET /customers/123" {
				return map[string]interface{}{
					"id":    "123",
					"name":  "Test Customer",
					"email": "test@example.com",
				}, nil
			} else if action == "POST /send" {
				return map[string]interface{}{
					"status": "sent",
					"id":     "email-123",
				}, nil
			}
			return nil, fmt.Errorf("unsupported action: %s", action)
		},
	}
	return connector, nil
}

// ListConnectors returns all registered connector names
func (r *MockIntegrationRegistry) ListConnectors() []string {
	result := make([]string, 0, len(r.registrations))
	for name := range r.registrations {
		result = append(result, name)
	}
	return result
}

// MockIntegration is a mock implementation of an integration
type MockIntegration struct {
	Name string
}

// MockIntegrationConnector is a mock implementation of module.IntegrationConnector
type MockIntegrationConnector struct {
	name      string
	executeFn func(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error)
	connected bool
}

// Connect implements module.IntegrationConnector
func (c *MockIntegrationConnector) Connect(ctx context.Context) error {
	c.connected = true
	return nil
}

// Disconnect implements module.IntegrationConnector
func (c *MockIntegrationConnector) Disconnect(ctx context.Context) error {
	c.connected = false
	return nil
}

// IsConnected implements module.IntegrationConnector
func (c *MockIntegrationConnector) IsConnected() bool {
	return c.connected
}

// Execute implements module.IntegrationConnector
func (c *MockIntegrationConnector) Execute(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	if c.executeFn != nil {
		return c.executeFn(ctx, action, params)
	}
	return map[string]interface{}{"mock": true}, nil
}

// GetName implements module.IntegrationConnector
func (c *MockIntegrationConnector) GetName() string {
	return c.name
}
