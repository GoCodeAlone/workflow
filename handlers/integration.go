package handlers

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// Standard handler name constants
const (
	IntegrationWorkflowHandlerName = "workflow.handler.integration"
	defaultWebhookPort             = 8080
	defaultRetryDelay              = time.Second
)

// IntegrationWorkflowConfig represents an integration workflow configuration
type IntegrationWorkflowConfig struct {
	Registry   string                 `json:"registry" yaml:"registry"`
	Connectors []IntegrationConnector `json:"connectors" yaml:"connectors"`
	Steps      []IntegrationStep      `json:"steps" yaml:"steps"`
}

// IntegrationConnector represents a connector configuration for an integration workflow.
type IntegrationConnector struct {
	Name   string         `json:"name" yaml:"name"`
	Type   string         `json:"type" yaml:"type"`
	Config map[string]any `json:"config" yaml:"config"`
}

// IntegrationStep represents a step in an integration workflow, referencing a named connector.
type IntegrationStep struct {
	Name       string         `json:"name" yaml:"name"`
	Connector  string         `json:"connector" yaml:"connector"`
	Action     string         `json:"action" yaml:"action"`
	Input      map[string]any `json:"input,omitempty" yaml:"input,omitempty"`
	Transform  string         `json:"transform,omitempty" yaml:"transform,omitempty"`
	OnSuccess  string         `json:"onSuccess,omitempty" yaml:"onSuccess,omitempty"`
	OnError    string         `json:"onError,omitempty" yaml:"onError,omitempty"`
	RetryCount int            `json:"retryCount,omitempty" yaml:"retryCount,omitempty"`
	RetryDelay string         `json:"retryDelay,omitempty" yaml:"retryDelay,omitempty"`
}

// IntegrationWorkflowHandler handles integration workflows by wiring connectors and executing step sequences.
type IntegrationWorkflowHandler struct {
	name         string
	namespace    module.ModuleNamespaceProvider
	eventEmitter *module.WorkflowEventEmitter
}

// SetEventEmitter sets the workflow event emitter for step-level lifecycle events.
func (h *IntegrationWorkflowHandler) SetEventEmitter(emitter *module.WorkflowEventEmitter) {
	h.eventEmitter = emitter
}

// NewIntegrationWorkflowHandler creates a new integration workflow handler
func NewIntegrationWorkflowHandler() *IntegrationWorkflowHandler {
	return NewIntegrationWorkflowHandlerWithNamespace(nil)
}

// NewIntegrationWorkflowHandlerWithNamespace creates a new integration workflow handler with namespace support
func NewIntegrationWorkflowHandlerWithNamespace(namespace module.ModuleNamespaceProvider) *IntegrationWorkflowHandler {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = module.NewStandardNamespace("", "")
	}

	return &IntegrationWorkflowHandler{
		name:      namespace.FormatName(IntegrationWorkflowHandlerName),
		namespace: namespace,
	}
}

// Name returns the name of this handler
func (h *IntegrationWorkflowHandler) Name() string {
	return h.name
}

// Init initializes the handler
func (h *IntegrationWorkflowHandler) Init(registry modular.ServiceRegistry) error {
	// Register ourselves in the service registry
	registry[h.name] = h
	return nil
}

// Start starts the handler
func (h *IntegrationWorkflowHandler) Start(ctx context.Context) error {
	return nil // Nothing to start
}

// Stop stops the handler
func (h *IntegrationWorkflowHandler) Stop(ctx context.Context) error {
	return nil // Nothing to stop
}

// CanHandle returns true if this handler can process the given workflow type
func (h *IntegrationWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "integration"
}

// connectorConfigureFunc is a factory function that creates an IntegrationConnector from a name and config map.
type connectorConfigureFunc func(name string, cfg map[string]any) (module.IntegrationConnector, error)

func configureHTTPConnector(name string, config map[string]any) (module.IntegrationConnector, error) {
	baseURL, _ := config["baseURL"].(string)
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL not specified for HTTP connector '%s'", name)
	}
	httpConn := module.NewHTTPIntegrationConnector(name, baseURL)

	authType, _ := config["authType"].(string)
	switch authType {
	case "basic":
		username, _ := config["username"].(string)
		password, _ := config["password"].(string)
		httpConn.SetBasicAuth(username, password)
	case "bearer":
		token, _ := config["token"].(string)
		httpConn.SetBearerAuth(token)
	}

	headers, _ := config["headers"].(map[string]any)
	for key, val := range headers {
		if valStr, ok := val.(string); ok {
			httpConn.SetHeader(key, valStr)
		}
	}

	if timeout, ok := config["timeoutSeconds"].(float64); ok {
		httpConn.SetTimeout(time.Duration(timeout) * time.Second)
	}

	if rateLimit, ok := config["requestsPerMinute"].(float64); ok {
		httpConn.SetRateLimit(int(rateLimit))
	}

	if allowPrivate, ok := config["allowPrivateIPs"].(bool); ok && allowPrivate {
		httpConn.SetAllowPrivateIPs(true)
	}

	return httpConn, nil
}

func configureWebhookConnector(name string, config map[string]any) (module.IntegrationConnector, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path not specified for webhook connector '%s'", name)
	}

	port := defaultWebhookPort
	if portVal, ok := config["port"].(float64); ok {
		port = int(portVal)
	}

	return module.NewWebhookIntegrationConnector(name, path, port), nil
}

func configureDatabaseConnector(name string, config map[string]any) (module.IntegrationConnector, error) {
	driver, _ := config["driver"].(string)
	dsn, _ := config["dsn"].(string)
	if driver == "" || dsn == "" {
		return nil, fmt.Errorf("driver and dsn must be specified for database connector '%s'", name)
	}
	dbConfig := module.DatabaseConfig{
		Driver: driver,
		DSN:    dsn,
	}
	if maxOpen, ok := config["maxOpenConns"].(float64); ok {
		dbConfig.MaxOpenConns = int(maxOpen)
	}
	return module.NewDatabaseIntegrationConnector(name, module.NewWorkflowDatabase(name+"-db", dbConfig)), nil
}

// connectorFactories maps connector type strings to their factory functions.
var connectorFactories = map[string]connectorConfigureFunc{
	"http":     configureHTTPConnector,
	"rest":     configureHTTPConnector,
	"api":      configureHTTPConnector,
	"webhook":  configureWebhookConnector,
	"database": configureDatabaseConnector,
}

// parseConnectorConfigs parses the connectors config slice, creates each connector via the
// appropriate factory, and registers it with intRegistry.
func parseConnectorConfigs(connectorsConfig []any, intRegistry module.IntegrationRegistry) error {
	if len(connectorsConfig) == 0 {
		return fmt.Errorf("no connectors defined in integration workflow")
	}

	for i, cc := range connectorsConfig {
		connMap, ok := cc.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid connector configuration at index %d", i)
		}

		name, _ := connMap["name"].(string)
		if name == "" {
			return fmt.Errorf("connector name not specified at index %d", i)
		}

		connType, _ := connMap["type"].(string)
		if connType == "" {
			return fmt.Errorf("connector type not specified for connector '%s'", name)
		}

		config, _ := connMap["config"].(map[string]any)

		factory, ok := connectorFactories[connType]
		if !ok {
			return fmt.Errorf("unsupported connector type '%s' for connector '%s'", connType, name)
		}

		connector, err := factory(name, config)
		if err != nil {
			return err
		}

		intRegistry.RegisterConnector(connector)
	}

	return nil
}

// parseStepConfigs validates the steps config slice against the registered connectors.
func parseStepConfigs(stepsConfig []any, intRegistry module.IntegrationRegistry) error {
	for i, sc := range stepsConfig {
		stepMap, ok := sc.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid step configuration at index %d", i)
		}

		name, _ := stepMap["name"].(string)
		if name == "" {
			return fmt.Errorf("step name not specified at index %d", i)
		}

		connectorName, _ := stepMap["connector"].(string)
		if connectorName == "" {
			return fmt.Errorf("connector not specified for step '%s'", name)
		}

		// Verify connector exists
		if _, err := intRegistry.GetConnector(connectorName); err != nil {
			return fmt.Errorf("connector '%s' not found for step '%s': %w", connectorName, name, err)
		}

		action, _ := stepMap["action"].(string)
		if action == "" {
			return fmt.Errorf("action not specified for step '%s'", name)
		}
	}

	return nil
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *IntegrationWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	// Convert the generic config to integration-specific config
	intConfig, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid integration workflow configuration format")
	}

	// Extract registry name
	registryName, _ := intConfig["registry"].(string)
	if registryName == "" {
		return fmt.Errorf("registry name not specified in integration workflow")
	}

	// Apply namespace to registry name if needed
	if h.namespace != nil {
		registryName = h.namespace.ResolveDependency(registryName)
	}

	// Get the integration registry
	var registrySvc any
	if err := app.GetService(registryName, &registrySvc); err != nil {
		app.Logger().Warn("integration registry service lookup warning", "registry", registryName, "error", err)
	}
	if registrySvc == nil {
		return fmt.Errorf("integration registry service '%s' not found", registryName)
	}

	intRegistry, ok := registrySvc.(module.IntegrationRegistry)
	if !ok {
		return fmt.Errorf("service '%s' is not an IntegrationRegistry", registryName)
	}

	connectorsConfig, _ := intConfig["connectors"].([]any)
	if err := parseConnectorConfigs(connectorsConfig, intRegistry); err != nil {
		return err
	}

	stepsConfig, _ := intConfig["steps"].([]any)
	if len(stepsConfig) > 0 {
		if err := parseStepConfigs(stepsConfig, intRegistry); err != nil {
			return err
		}
	}

	return nil
}

// executeStepWithRetry executes a single step action against the connector, retrying up to
// step.RetryCount times with step.RetryDelay between attempts.
func executeStepWithRetry(ctx context.Context, connector module.IntegrationConnector, step *IntegrationStep, params map[string]any) (map[string]any, error) {
	result, err := connector.Execute(ctx, step.Action, params)
	if err == nil || step.RetryCount == 0 {
		return result, err
	}

	retryDelay := defaultRetryDelay
	if step.RetryDelay != "" {
		if parsedDelay, parseErr := time.ParseDuration(step.RetryDelay); parseErr == nil {
			retryDelay = parsedDelay
		}
	}

	for i := 0; i < step.RetryCount; i++ {
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		result, err = connector.Execute(ctx, step.Action, params)
		if err == nil {
			return result, nil
		}
	}

	return result, err
}

// ExecuteIntegrationWorkflow executes a sequence of integration steps
func (h *IntegrationWorkflowHandler) ExecuteIntegrationWorkflow(
	ctx context.Context,
	registry module.IntegrationRegistry,
	steps []IntegrationStep,
	initialContext map[string]any,
) (map[string]any, error) {
	results := make(map[string]any)
	// Add initial context values to results
	maps.Copy(results, initialContext)

	// Execute steps sequentially
	for i := range steps {
		step := &steps[i]
		stepStartTime := time.Now()
		if h.eventEmitter != nil {
			h.eventEmitter.EmitStepStarted(ctx, "integration", step.Name, step.Connector, step.Action)
		}

		// Get the connector for this step
		connector, err := registry.GetConnector(step.Connector)
		if err != nil {
			return results, fmt.Errorf("error getting connector '%s': %w", step.Connector, err)
		}
		if connector == nil {
			return results, fmt.Errorf("connector '%s' not found", step.Connector)
		}

		// Ensure the connector is connected
		if !connector.IsConnected() {
			if err = connector.Connect(ctx); err != nil {
				return results, fmt.Errorf("error executing step '%s': connector not connected: %w", step.Name, err)
			}

			// Double check it's now connected
			if !connector.IsConnected() {
				return results, fmt.Errorf("error executing step '%s': connector not connected after connection attempt", step.Name)
			}
		}

		// Process input parameters - could handle variable substitution here
		params := make(map[string]any)
		for k, v := range step.Input {
			// Simple variable substitution from previous steps
			if strVal, ok := v.(string); ok && len(strVal) > 3 && strVal[0:2] == "${" && strVal[len(strVal)-1] == '}' {
				// Extract the variable name, e.g., ${step1.value} -> step1.value
				varName := strVal[2 : len(strVal)-1]

				// Check if it's a reference to a previous step result
				if result, ok := results[varName]; ok {
					params[k] = result
				} else {
					// If not found, keep the original value
					params[k] = v
				}
			} else {
				// Use the value as is
				params[k] = v
			}
		}

		stepResult, err := executeStepWithRetry(ctx, connector, step, params)
		if err != nil {
			if step.OnError != "" {
				// Could invoke error handler here
				// For now, just continue and store the error in results
				results[step.Name+"_error"] = err.Error()
				continue
			}
			// No error handler, return the error
			if h.eventEmitter != nil {
				h.eventEmitter.EmitStepFailed(ctx, "integration", step.Name, step.Connector, step.Action, time.Since(stepStartTime), err)
			}
			return results, fmt.Errorf("error executing step '%s': %w", step.Name, err)
		}

		// Store the result
		results[step.Name] = stepResult

		if h.eventEmitter != nil {
			h.eventEmitter.EmitStepCompleted(ctx, "integration", step.Name, step.Connector, step.Action, time.Since(stepStartTime), stepResult)
		}

		// Handle success path if specified
		if step.OnSuccess != "" {
			// Could invoke success handler here
			// For now, we just continue with the next step
			// Note: Logging would require access to logger (stepIndex: %d, onSuccess: %s)
			_ = step.OnSuccess // Mark as used to satisfy linter
		}
	}

	return results, nil
}

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *IntegrationWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	// For integration workflows, the action should specify which workflow to run
	// Find the integration registry
	registryName := action
	// If the action contains a colon, it specifies registry:workflow
	if parts := strings.Split(action, ":"); len(parts) > 1 {
		registryName = parts[0]
		action = parts[1]
	}

	// Get the registry from the service registry
	appHelper := GetServiceHelper(ctx.Value(applicationContextKey).(modular.Application))
	registrySvc := appHelper.Service(registryName)
	if registrySvc == nil {
		return nil, fmt.Errorf("integration registry '%s' not found", registryName)
	}

	registry, ok := registrySvc.(module.IntegrationRegistry)
	if !ok {
		return nil, fmt.Errorf("service '%s' is not an IntegrationRegistry", registryName)
	}

	// Parse steps from the data if provided
	var steps []IntegrationStep
	if stepsData, ok := data["steps"].([]any); ok {
		for i, stepData := range stepsData {
			stepMap, ok := stepData.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid step data at index %d", i)
			}

			step := IntegrationStep{
				Name:       fmt.Sprintf("%s-%d", action, i),
				RetryCount: 0,
				RetryDelay: "1s",
			}
			if connectorVal, ok := stepMap["connector"].(string); ok {
				step.Connector = connectorVal
			}
			if actionVal, ok := stepMap["action"].(string); ok {
				step.Action = actionVal
			}

			// Extract optional fields if present
			if input, ok := stepMap["input"].(map[string]any); ok {
				step.Input = input
			}
			if transform, ok := stepMap["transform"].(string); ok {
				step.Transform = transform
			}
			if onSuccess, ok := stepMap["onSuccess"].(string); ok {
				step.OnSuccess = onSuccess
			}
			if onError, ok := stepMap["onError"].(string); ok {
				step.OnError = onError
			}
			if retryCount, ok := stepMap["retryCount"].(float64); ok {
				step.RetryCount = int(retryCount)
			}
			if retryDelay, ok := stepMap["retryDelay"].(string); ok {
				step.RetryDelay = retryDelay
			}

			steps = append(steps, step)
		}
	} else if action != "" {
		// Create a single step based on action
		connector, ok := data["connector"].(string)
		if !ok {
			return nil, fmt.Errorf("connector not specified")
		}

		step := IntegrationStep{
			Name:      "step-0",
			Connector: connector,
			Action:    action,
			Input:     data,
		}
		steps = append(steps, step)
	} else {
		return nil, fmt.Errorf("no steps provided and no action specified")
	}

	// Execute the integration workflow with the steps
	return h.ExecuteIntegrationWorkflow(ctx, registry, steps, data)
}
