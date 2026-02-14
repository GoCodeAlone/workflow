package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

// HTTPRouteConfig represents a route configuration in HTTP workflow
type HTTPRouteConfig struct {
	Method      string         `json:"method" yaml:"method"`
	Path        string         `json:"path" yaml:"path"`
	Handler     string         `json:"handler" yaml:"handler"`
	Middlewares []string       `json:"middlewares,omitempty" yaml:"middlewares,omitempty"`
	Config      map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// HTTPWorkflowHandler handles HTTP-based workflows
type HTTPWorkflowHandler struct{}

// NewHTTPWorkflowHandler creates a new HTTP workflow handler
func NewHTTPWorkflowHandler() *HTTPWorkflowHandler {
	return &HTTPWorkflowHandler{}
}

// CanHandle returns true if this handler can process the given workflow type.
// It matches "http" and any key prefixed with "http-" (e.g. "http-admin"),
// allowing multiple independent HTTP workflow sections in a single config.
func (h *HTTPWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "http" || strings.HasPrefix(workflowType, "http-")
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *HTTPWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	// Convert the generic config to HTTP-specific config
	httpConfig, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid HTTP workflow configuration format")
	}

	// Extract routes from the configuration
	routesConfig, ok := httpConfig["routes"].([]any)
	if !ok {
		return fmt.Errorf("routes not found in HTTP workflow configuration")
	}

	// Find router and server modules dynamically by looking for the first services
	// that implement the required interfaces
	var router workflowmodule.HTTPRouter
	var server workflowmodule.HTTPServer

	// Extract explicit names if provided in config
	explicitRouterName, _ := httpConfig["router"].(string)
	explicitServerName, _ := httpConfig["server"].(string)

	// First try with explicitly configured names if provided
	if explicitRouterName != "" {
		if err := app.GetService(explicitRouterName, &router); err != nil || router == nil {
			return fmt.Errorf("explicit router '%s' not found", explicitRouterName)
		}
	}

	if explicitServerName != "" {
		if err := app.GetService(explicitServerName, &server); err != nil || server == nil {
			return fmt.Errorf("explicit server '%s' not found", explicitServerName)
		}
	}

	// If not found by explicit names, try to find by scanning all services
	if router == nil || server == nil {
		for _, svc := range app.SvcRegistry() {
			// First try to find a router if we don't have one yet
			if router == nil {
				if r, ok := svc.(workflowmodule.HTTPRouter); ok {
					router = r
				}
			}

			// Then try to find a server if we don't have one yet
			if server == nil {
				if s, ok := svc.(workflowmodule.HTTPServer); ok {
					server = s
				}
			}

			// If we have both, break out of the loop
			if router != nil && server != nil {
				break
			}
		}
	}

	// If we still don't have a router, try the default name as a fallback
	if router == nil {
		if err := app.GetService("httpRouter", &router); err != nil {
			// Log the error but continue since this might be expected
			app.Logger().Debug("Failed to get httpRouter service: %v", err)
		}
	}

	// If we still don't have a server, try the default name as a fallback
	if server == nil {
		if err := app.GetService("httpServer", &server); err != nil {
			// Log the error but continue since this might be expected
			app.Logger().Debug("Failed to get httpServer service: %v", err)
		}
	}

	// Verify we found both required services
	if router == nil {
		return fmt.Errorf("no HTTP router service found - ensure a router module is configured")
	}

	if server == nil {
		return fmt.Errorf("no HTTP server service found - ensure a server module is configured")
	}

	// Connect router to server
	server.AddRouter(router)

	// Configure each route
	for i, rc := range routesConfig {
		routeMap, ok := rc.(map[string]any)
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
		var httpHandler workflowmodule.HTTPHandler
		err := app.GetService(handlerName, &httpHandler)
		if err != nil {
			return fmt.Errorf("handler service '%s' not found for route %s %s. Error: %w", handlerName, method, path, err)
		}

		// Process middleware if specified
		var middlewares []workflowmodule.HTTPMiddleware
		if middlewareNames, ok := routeMap["middlewares"].([]any); ok {
			for j, middlewareName := range middlewareNames {
				mwName, ok := middlewareName.(string)
				if !ok {
					return fmt.Errorf("invalid middleware name at index %d for route %s %s", j, method, path)
				}

				// Get middleware service by name
				var middleware workflowmodule.HTTPMiddleware
				err = app.GetService(mwName, &middleware)
				if err != nil || middleware == nil {
					return fmt.Errorf("middleware service '%s' not found for route %s %s", mwName, method, path)
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

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *HTTPWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	// For HTTP workflows, executing the workflow means making sure the server is running
	// and optionally checking the status or running health checks.

	// Get the application from context
	var app modular.Application
	if appVal := ctx.Value(applicationContextKey); appVal != nil {
		app = appVal.(modular.Application)
	} else {
		return nil, fmt.Errorf("application context not available")
	}

	// Parse the action - it can be a command like "status", "check", "routes", etc.
	command := action
	if command == "" {
		command = "status" // default command
	}

	// Find HTTP server and router to check
	var server workflowmodule.HTTPServer
	var router workflowmodule.HTTPRouter
	var serverName, routerName string

	// Look for server and router in all services
	for name, svc := range app.SvcRegistry() {
		if server == nil {
			if s, ok := svc.(workflowmodule.HTTPServer); ok {
				server = s
				serverName = name
			}
		}

		if router == nil {
			if r, ok := svc.(workflowmodule.HTTPRouter); ok {
				router = r
				routerName = name
			}
		}

		if server != nil && router != nil {
			break
		}
	}

	// Look for explicitly specified server and router names in the data
	if explicit, ok := data["server"].(string); ok && explicit != "" {
		var serverSvc any
		if err := app.GetService(explicit, &serverSvc); err == nil && serverSvc != nil {
			if s, ok := serverSvc.(workflowmodule.HTTPServer); ok {
				server = s
				serverName = explicit
			}
		}
	}

	if explicit, ok := data["router"].(string); ok && explicit != "" {
		var routerSvc any
		if err := app.GetService(explicit, &routerSvc); err == nil && routerSvc != nil {
			if r, ok := routerSvc.(workflowmodule.HTTPRouter); ok {
				router = r
				routerName = explicit
			}
		}
	}

	if server == nil {
		return nil, fmt.Errorf("no HTTP server found in the application services")
	}

	if router == nil {
		return nil, fmt.Errorf("no HTTP router found in the application services")
	}

	// Execute the requested command
	result := map[string]any{
		"server": serverName,
		"router": routerName,
	}

	switch command {
	case "status":
		// Simply report if the server is available
		result["status"] = "running"

	case "routes":
		// Try to get route information if available
		/*if stdRouter, ok := router.(*workflowmodule.StandardHTTPRouter); ok {
			routes := stdRouter.GetRoutes()
			routeInfo := make([]map[string]string, 0, len(routes))

			for _, r := range routes {
				routeInfo = append(routeInfo, map[string]string{
					"method": r.Method,
					"path":   r.Path,
					"handler": r.HandlerName,
				})
			}

			result["routes"] = routeInfo
			result["routeCount"] = len(routes)
		} else {*/
		result["routes"] = "information not available for this router type"
		//}

	case "check":
		// More detailed health check could be added here
		result["healthStatus"] = "healthy"

	case "start":
		// Ensure the server is started
		// This is mostly a no-op since the server should already be running
		result["action"] = "server already running"

	default:
		return nil, fmt.Errorf("unknown HTTP workflow command: %s", command)
	}

	return result, nil
}
