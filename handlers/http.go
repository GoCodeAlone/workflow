package handlers

import (
	"fmt"

	"github.com/GoCodeAlone/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

// HTTPRouteConfig represents a route configuration in HTTP workflow
type HTTPRouteConfig struct {
	Method      string                 `json:"method" yaml:"method"`
	Path        string                 `json:"path" yaml:"path"`
	Handler     string                 `json:"handler" yaml:"handler"`
	Middlewares []string               `json:"middlewares,omitempty" yaml:"middlewares,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

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
func (h *HTTPWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
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

	// Find router and server modules
	var router workflowmodule.HTTPRouter
	var server workflowmodule.HTTPServer

	// Look for standard router and server implementations
	var routerSvc interface{}
	_ = app.GetService("httpRouter", &routerSvc)
	if routerSvc != nil {
		router, _ = routerSvc.(workflowmodule.HTTPRouter)
	}

	var serverSvc interface{}
	_ = app.GetService("httpServer", &serverSvc)
	if serverSvc != nil {
		server, _ = serverSvc.(workflowmodule.HTTPServer)
	}

	if router == nil {
		return fmt.Errorf("no HTTP router service found")
	}

	if server == nil {
		return fmt.Errorf("no HTTP server service found")
	}

	// Connect router to server
	server.AddRouter(router)

	// Configure each route
	for i, rc := range routesConfig {
		routeMap, ok := rc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid route configuration at index %d", i)
		}

		method, _ := routeMap["method"].(string)
		path, _ := routeMap["path"].(string)
		handlerName, _ := routeMap["handler"].(string)

		if method == "" || path == "" || handlerName == "" {
			return fmt.Errorf("incomplete route configuration at index %d: method, path and handler are required", i)
		}

		// Get handler service by name
		var handlerSvc interface{}
		_ = app.GetService(handlerName, &handlerSvc)
		if handlerSvc == nil {
			return fmt.Errorf("handler service '%s' not found for route %s %s", handlerName, method, path)
		}

		httpHandler, ok := handlerSvc.(workflowmodule.HTTPHandler)
		if !ok {
			return fmt.Errorf("service '%s' does not implement HTTPHandler interface", handlerName)
		}

		// Process middleware if specified
		var middlewares []workflowmodule.HTTPMiddleware
		if middlewareNames, ok := routeMap["middlewares"].([]interface{}); ok {
			for j, middlewareName := range middlewareNames {
				mwName, ok := middlewareName.(string)
				if !ok {
					return fmt.Errorf("invalid middleware name at index %d for route %s %s", j, method, path)
				}

				// Get middleware service by name
				var middlewareSvc interface{}
				_ = app.GetService(mwName, &middlewareSvc)
				if middlewareSvc == nil {
					return fmt.Errorf("middleware service '%s' not found for route %s %s", mwName, method, path)
				}

				middleware, ok := middlewareSvc.(workflowmodule.HTTPMiddleware)
				if !ok {
					return fmt.Errorf("service '%s' does not implement HTTPMiddleware interface", mwName)
				}

				middlewares = append(middlewares, middleware)
			}
		}

		// Add route to router with middleware if any
		if stdRouter, ok := router.(*workflowmodule.StandardHTTPRouter); ok && len(middlewares) > 0 {
			stdRouter.AddRouteWithMiddleware(method, path, httpHandler, middlewares)
		} else {
			// Fall back to standard route addition if no middleware or if router doesn't support middleware
			router.AddRoute(method, path, httpHandler)
		}
	}

	return nil
}
