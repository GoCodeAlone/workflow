package llm

import (
	"encoding/json"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// ToolDefinition describes a tool the LLM can call during workflow generation.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// BuiltinModuleTypes returns the list of available module types and their descriptions.
var BuiltinModuleTypes = map[string]string{
	"http.server":               "HTTP server with address config",
	"http.router":               "HTTP router, depends on server",
	"http.handler":              "Generic HTTP handler with contentType",
	"api.handler":               "REST API handler with resourceName",
	"http.middleware.auth":      "Authentication middleware",
	"http.middleware.logging":   "Logging middleware with logLevel",
	"http.middleware.ratelimit": "Rate limiter with requestsPerMinute and burstSize",
	"http.middleware.cors":      "CORS middleware with allowedOrigins and allowedMethods",
	"messaging.broker":          "In-memory message broker",
	"messaging.handler":         "Message subscription handler",
	"statemachine.engine":       "State machine engine",
	"state.tracker":             "State tracker for resources",
	"state.connector":           "Connects state machines to resources",
	"event.processor":           "Complex event pattern processor",
	"httpserver.modular":        "Modular HTTP server",
	"httpclient.modular":        "Modular HTTP client",
	"chimux.router":             "Chi-based HTTP router",
	"scheduler.modular":         "Cron-based scheduler",
	"eventbus.modular":          "Event bus pub/sub",
	"eventlogger.modular":       "Event logger",
	"cache.modular":             "Cache module",
	"database.modular":          "Database module",
	"auth.modular":              "Auth module",
	"jsonschema.modular":        "JSON schema validation",
	"reverseproxy":              "Reverse proxy",
	"http.proxy":                "Reverse proxy (alias)",
}

// Tools returns the tool definitions for the Claude API.
func Tools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_components",
			Description: "Lists all available built-in module types and their descriptions.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_component_schema",
			Description: "Returns the configuration schema for a specific module type.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"module_type": map[string]interface{}{
						"type":        "string",
						"description": "The module type to get the schema for (e.g., 'http.server', 'messaging.broker')",
					},
				},
				"required": []string{"module_type"},
			},
		},
		{
			Name:        "validate_config",
			Description: "Validates a workflow configuration YAML string. Returns validation errors or confirms the config is valid.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config_yaml": map[string]interface{}{
						"type":        "string",
						"description": "The workflow configuration as a YAML string",
					},
				},
				"required": []string{"config_yaml"},
			},
		},
		{
			Name:        "get_example_workflow",
			Description: "Returns an example workflow configuration for a given category.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "The category of example: 'http', 'messaging', 'statemachine', 'event', 'trigger'",
					},
				},
				"required": []string{"category"},
			},
		},
	}
}

// HandleToolCall executes a tool call and returns the result as a string.
func HandleToolCall(name string, input json.RawMessage) (string, error) {
	switch name {
	case "list_components":
		return handleListComponents()
	case "get_component_schema":
		return handleGetComponentSchema(input)
	case "validate_config":
		return handleValidateConfig(input)
	case "get_example_workflow":
		return handleGetExampleWorkflow(input)
	default:
		return "", nil
	}
}

func handleListComponents() (string, error) {
	result, err := json.MarshalIndent(BuiltinModuleTypes, "", "  ")
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func handleGetComponentSchema(input json.RawMessage) (string, error) {
	var params struct {
		ModuleType string `json:"module_type"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	schemas := map[string]interface{}{
		"http.server": map[string]interface{}{
			"address": "string - listen address (e.g., ':8080')",
		},
		"http.handler": map[string]interface{}{
			"contentType": "string - response content type (default: 'application/json')",
		},
		"api.handler": map[string]interface{}{
			"resourceName": "string - REST resource name",
		},
		"http.middleware.auth": map[string]interface{}{
			"secretKey": "string - JWT secret key",
			"authType":  "string - auth type (default: 'Bearer')",
		},
		"http.middleware.logging": map[string]interface{}{
			"logLevel": "string - log level (default: 'info')",
		},
		"http.middleware.ratelimit": map[string]interface{}{
			"requestsPerMinute": "int - max requests per minute (default: 60)",
			"burstSize":         "int - burst size (default: 10)",
		},
		"http.middleware.cors": map[string]interface{}{
			"allowedOrigins": "[]string - allowed origins (default: ['*'])",
			"allowedMethods": "[]string - allowed methods (default: ['GET','POST','PUT','DELETE','OPTIONS'])",
		},
		"messaging.broker": map[string]interface{}{
			"description": "string - broker description",
		},
		"messaging.handler": map[string]interface{}{
			"description": "string - handler description",
		},
		"statemachine.engine": map[string]interface{}{
			"description": "string - engine description",
		},
		"event.processor": map[string]interface{}{
			"bufferSize":      "int - event buffer size",
			"cleanupInterval": "string - cleanup interval duration",
		},
	}

	schema, ok := schemas[params.ModuleType]
	if !ok {
		if _, exists := BuiltinModuleTypes[params.ModuleType]; exists {
			return `{"note": "This module type uses default configuration or is configured through the modular framework."}`, nil
		}
		return `{"error": "Unknown module type"}`, nil
	}

	result, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func handleValidateConfig(input json.RawMessage) (string, error) {
	var params struct {
		ConfigYAML string `json:"config_yaml"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal([]byte(params.ConfigYAML), &cfg); err != nil {
		return `{"valid": false, "error": "` + err.Error() + `"}`, nil
	}

	var errors []string

	if len(cfg.Modules) == 0 {
		errors = append(errors, "no modules defined")
	}

	// Check for duplicate module names
	seen := make(map[string]bool)
	for _, mod := range cfg.Modules {
		if mod.Name == "" {
			errors = append(errors, "module with empty name found")
		}
		if mod.Type == "" {
			errors = append(errors, "module '"+mod.Name+"' has empty type")
		}
		if seen[mod.Name] {
			errors = append(errors, "duplicate module name: "+mod.Name)
		}
		seen[mod.Name] = true
	}

	// Check dependency references
	for _, mod := range cfg.Modules {
		for _, dep := range mod.DependsOn {
			if !seen[dep] {
				errors = append(errors, "module '"+mod.Name+"' depends on unknown module '"+dep+"'")
			}
		}
	}

	if len(errors) > 0 {
		result, _ := json.Marshal(map[string]interface{}{
			"valid":  false,
			"errors": errors,
		})
		return string(result), nil
	}

	return `{"valid": true}`, nil
}

func handleGetExampleWorkflow(input json.RawMessage) (string, error) {
	var params struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	examples := map[string]string{
		"http": `modules:
  - name: httpServer
    type: http.server
    config:
      address: ":8080"
  - name: httpRouter
    type: http.router
    dependsOn:
      - httpServer
  - name: userService
    type: http.handler
    config:
      contentType: "application/json"
    dependsOn:
      - httpRouter
workflows:
  http:
    routes:
      - method: GET
        path: /api/users
        handler: userService`,

		"messaging": `modules:
  - name: messageBroker
    type: messaging.broker
  - name: eventHandler
    type: messaging.handler
    dependsOn:
      - messageBroker
workflows:
  messaging:
    subscriptions:
      - topic: events
        handler: eventHandler`,

		"statemachine": `modules:
  - name: orderEngine
    type: statemachine.engine
  - name: validationHandler
    type: messaging.handler
workflows:
  statemachine:
    engine: orderEngine
    definitions:
      - name: order-workflow
        initialState: "new"
        states:
          new:
            description: "New order"
            isFinal: false
          processing:
            description: "Processing"
            isFinal: false
          completed:
            description: "Done"
            isFinal: true
        transitions:
          start:
            fromState: "new"
            toState: "processing"
          finish:
            fromState: "processing"
            toState: "completed"
    hooks:
      - workflowType: "order-workflow"
        transitions: ["start"]
        handler: "validationHandler"`,

		"event": `modules:
  - name: eventBroker
    type: messaging.broker
  - name: eventProcessor
    type: event.processor
    config:
      bufferSize: 1000
      cleanupInterval: "5m"
  - name: alertHandler
    type: messaging.handler
workflows:
  event:
    processor: eventProcessor
    patterns:
      - patternId: "failure-burst"
        eventTypes: ["service.error"]
        windowTime: "5m"
        condition: "count"
        minOccurs: 5
    handlers:
      - patternId: "failure-burst"
        handler: alertHandler`,

		"trigger": `triggers:
  http:
    routes:
      - path: "/api/workflows/start"
        method: "POST"
        workflow: "my-workflow"
        action: "begin"
  schedule:
    jobs:
      - cron: "0 * * * *"
        workflow: "my-workflow"
        action: "hourly-check"
  event:
    subscriptions:
      - topic: "events"
        event: "item.created"
        workflow: "my-workflow"
        action: "process-item"`,
	}

	example, ok := examples[params.Category]
	if !ok {
		return `{"error": "Unknown category. Available: http, messaging, statemachine, event, trigger"}`, nil
	}
	return example, nil
}
