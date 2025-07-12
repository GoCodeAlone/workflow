package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// Standard handler name constants
const (
	IntegrationWorkflowHandlerName = "workflow.handler.integration"
)

// IntegrationWorkflowConfig represents an integration workflow configuration
type IntegrationWorkflowConfig struct {
	Registry   string                 `json:"registry" yaml:"registry"`
	Connectors []IntegrationConnector `json:"connectors" yaml:"connectors"`
	Steps      []IntegrationStep      `json:"steps" yaml:"steps"`
}

// IntegrationConnector represents a connector configuration
type IntegrationConnector struct {
	Name   string                 `json:"name" yaml:"name"`
	Type   string                 `json:"type" yaml:"type"`
	Config map[string]interface{} `json:"config" yaml:"config"`
}

// IntegrationStep represents a step in an integration workflow
type IntegrationStep struct {
	Name       string                 `json:"name" yaml:"name"`
	Connector  string                 `json:"connector" yaml:"connector"`
	Action     string                 `json:"action" yaml:"action"`
	Input      map[string]interface{} `json:"input,omitempty" yaml:"input,omitempty"`
	Transform  string                 `json:"transform,omitempty" yaml:"transform,omitempty"`
	OnSuccess  string                 `json:"onSuccess,omitempty" yaml:"onSuccess,omitempty"`
	OnError    string                 `json:"onError,omitempty" yaml:"onError,omitempty"`
	RetryCount int                    `json:"retryCount,omitempty" yaml:"retryCount,omitempty"`
	RetryDelay string                 `json:"retryDelay,omitempty" yaml:"retryDelay,omitempty"`
}

// IntegrationWorkflowHandler handles integration workflows
type IntegrationWorkflowHandler struct {
	name      string
	namespace module.ModuleNamespaceProvider
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

// ConfigureWorkflow sets up the workflow from configuration
func (h *IntegrationWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
	// Convert the generic config to integration-specific config
	intConfig, ok := workflowConfig.(map[string]interface{})
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
	var registrySvc interface{}
	_ = app.GetService(registryName, &registrySvc)
	if registrySvc == nil {
		return fmt.Errorf("integration registry service '%s' not found", registryName)
	}

	intRegistry, ok := registrySvc.(module.IntegrationRegistry)
	if !ok {
		return fmt.Errorf("service '%s' is not an IntegrationRegistry", registryName)
	}

	// Configure connectors
	connectorsConfig, _ := intConfig["connectors"].([]interface{})
	if len(connectorsConfig) == 0 {
		return fmt.Errorf("no connectors defined in integration workflow")
	}

	for i, cc := range connectorsConfig {
		connMap, ok := cc.(map[string]interface{})
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

		config, _ := connMap["config"].(map[string]interface{})

		// Create and configure the connector based on type
		var connector module.IntegrationConnector
		switch connType {
		case "http", "rest", "api":
			// HTTP connector
			baseURL, _ := config["baseURL"].(string)
			if baseURL == "" {
				return fmt.Errorf("baseURL not specified for HTTP connector '%s'", name)
			}
			httpConn := module.NewHTTPIntegrationConnector(name, baseURL)

			// Configure authentication
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

			// Configure headers
			headers, _ := config["headers"].(map[string]interface{})
			for key, val := range headers {
				if valStr, ok := val.(string); ok {
					httpConn.SetHeader(key, valStr)
				}
			}

			// Configure timeout
			if timeout, ok := config["timeoutSeconds"].(float64); ok {
				httpConn.SetTimeout(time.Duration(timeout) * time.Second)
			}

			// Configure rate limiting
			if rateLimit, ok := config["requestsPerMinute"].(float64); ok {
				httpConn.SetRateLimit(int(rateLimit))
			}

			connector = httpConn
		case "webhook":
			// Webhook connector
			path, _ := config["path"].(string)
			if path == "" {
				return fmt.Errorf("path not specified for webhook connector '%s'", name)
			}

			port := 8080 // Default port
			if portVal, ok := config["port"].(float64); ok {
				port = int(portVal)
			}

			webhookConn := module.NewWebhookIntegrationConnector(name, path, port)

			// If there are predefined handlers, we'd configure them here
			// This is a simplified version; in a full implementation we'd want to
			// support mapping webhook events to internal handlers or message queues

			connector = webhookConn
		default:
			return fmt.Errorf("unsupported connector type '%s' for connector '%s'", connType, name)
		}

		// Register the connector
		intRegistry.RegisterConnector(connector)
	}

	// Configure workflow steps
	stepsConfig, _ := intConfig["steps"].([]interface{})
	if len(stepsConfig) > 0 {
		// Process steps configuration
		// In a full implementation, we'd create a workflow executor that runs these steps
		// For now, we'll just validate the configuration
		for i, sc := range stepsConfig {
			stepMap, ok := sc.(map[string]interface{})
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
			_, err := intRegistry.GetConnector(connectorName)
			if err != nil {
				return fmt.Errorf("connector '%s' not found for step '%s': %w", connectorName, name, err)
			}

			action, _ := stepMap["action"].(string)
			if action == "" {
				return fmt.Errorf("action not specified for step '%s'", name)
			}
		}
	}

	return nil
}

// ExecuteIntegrationWorkflow executes a sequence of integration steps
func (h *IntegrationWorkflowHandler) ExecuteIntegrationWorkflow(
	ctx context.Context,
	registry module.IntegrationRegistry,
	steps []IntegrationStep,
	initialContext map[string]interface{},
) (map[string]interface{}, error) {
	results := make(map[string]interface{})
	// Add initial context values to results
	for k, v := range initialContext {
		results[k] = v
	}

	// Execute steps sequentially
	for _, step := range steps {
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
		params := make(map[string]interface{})
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

		// Execute the step
		stepResult, err := connector.Execute(ctx, step.Action, params)
		if err != nil {
			// Handle retry logic if configured
			if step.RetryCount > 0 {
				// Simple retry implementation
				var retryErr error
				var retryResult map[string]interface{}

				// Parse retry delay
				retryDelay := time.Second // Default 1 second
				if step.RetryDelay != "" {
					if parsedDelay, parseErr := time.ParseDuration(step.RetryDelay); parseErr == nil {
						retryDelay = parsedDelay
					}
				}

				// Try retries
				for i := 0; i < step.RetryCount; i++ {
					// Wait before retrying
					select {
					case <-time.After(retryDelay):
						// Continue with retry
					case <-ctx.Done():
						// Context canceled or timed out
						return results, ctx.Err()
					}

					// Retry execution
					retryResult, retryErr = connector.Execute(ctx, step.Action, params)
					if retryErr == nil {
						// Success on retry
						stepResult = retryResult
						err = nil
						break
					}
				}
			}

			// If still error after retries, handle error path
			if err != nil {
				if step.OnError != "" {
					// Could invoke error handler here
					// For now, just continue and store the error in results
					results[step.Name+"_error"] = err.Error()
					continue
				}
				// No error handler, return the error
				return results, fmt.Errorf("error executing step '%s': %w", step.Name, err)
			}
		}

		// Store the result
		results[step.Name] = stepResult

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
func (h *IntegrationWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
	// For integration workflows, the action should specify which workflow to run
	// Find the integration registry
	registryName := action
	// If the action contains a colon, it specifies registry:workflow
	if parts := strings.Split(action, ":"); len(parts) > 1 {
		registryName = parts[0]
		action = parts[1]
	}

	// Get the registry from the service registry
	appHelper := GetServiceHelper(ctx.Value("application").(modular.Application))
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
	if stepsData, ok := data["steps"].([]interface{}); ok {
		for i, stepData := range stepsData {
			stepMap, ok := stepData.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid step data at index %d", i)
			}

			step := IntegrationStep{
				Name:       fmt.Sprintf("%s-%d", action, i),
				Connector:  stepMap["connector"].(string),
				Action:     stepMap["action"].(string),
				RetryCount: 0,
				RetryDelay: "1s",
			}

			// Extract optional fields if present
			if input, ok := stepMap["input"].(map[string]interface{}); ok {
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
