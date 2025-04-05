package handlers

import (
	"fmt"
	"github.com/GoCodeAlone/modular/module"
)

// HTTPWorkflowHandler handles HTTP-based workflows
type HTTPWorkflowHandler struct{}

// NewHTTPWorkflowHandler creates a new HTTP workflow handler
func NewHTTPWorkflowHandler() *HTTPWorkflowHandler {
	return &HTTPWorkflowHandler{}
}

// CanHandle returns true if this handler can process the given workflow type
func (h *HTTPWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "http"
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *HTTPWorkflowHandler) ConfigureWorkflow(registry *module.Registry, workflowConfig interface{}) error {
	// Convert the generic config to HTTP-specific config
	httpConfig, ok := workflowConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid HTTP workflow configuration format")
	}

	// Extract routes from the configuration
	routesConfig, ok := httpConfig["routes"].([]interface{})
	if !ok {
		return fmt.Errorf("routes not found in HTTP workflow configuration")
	}

	// Find router module and HTTP server module
	var router module.HTTPRouter
	var server module.HTTPServer

	registry.Each(func(name string, mod module.Module) {
		if r, ok := mod.(module.HTTPRouter); ok {
			router = r
		}
		if s, ok := mod.(module.HTTPServer); ok {
			server = s
		}
	})

	if router == nil {
		return fmt.Errorf("no HTTP router module found")
	}

	if server == nil {
		return fmt.Errorf("no HTTP server module found")
	}

	// Connect router to server
	server.AddRouter(router)

	// Configure each route
	for _, routeConfig := range routesConfig {
		// Process route configuration
		// This would be similar to the previous implementation but adapted to use the generic config structure
	}

	return nil
}
