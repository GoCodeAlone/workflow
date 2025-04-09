package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

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
	err := app.GetService("httpRouter", &router)
	if err != nil {
		return fmt.Errorf("error getting HTTP router service: %v", err)
	}

	err = app.GetService("httpServer", &server)
	if err != nil {
		return fmt.Errorf("error getting HTTP server service: %v", err)
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
		var httpHandler workflowmodule.HTTPHandler
		err = app.GetService(handlerName, &httpHandler)
		if err != nil {
			return fmt.Errorf("handler service '%s' not found for route %s %s. Error: %w", handlerName, method, path, err)
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

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *HTTPWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
	// For HTTP workflows, the action should specify the handler to invoke
	// and data contains the HTTP request parameters

	// Get the application from context
	var app modular.Application
	if appVal := ctx.Value("application"); appVal != nil {
		app = appVal.(modular.Application)
	} else {
		return nil, fmt.Errorf("application context not available")
	}

	// Parse the handler and path from the action
	// Format: handler:path or just handler
	handlerName := action
	path := "/"

	if parts := strings.Split(action, ":"); len(parts) > 1 {
		handlerName = parts[0]
		path = parts[1]
	}

	// Get handler from data if not in action
	if handlerName == "" {
		handlerName, _ = data["handler"].(string)
	}

	if handlerName == "" {
		return nil, fmt.Errorf("HTTP handler not specified")
	}

	// Get the HTTP handler
	var handlerSvc interface{}
	err := app.GetService(handlerName, &handlerSvc)
	if err != nil || handlerSvc == nil {
		return nil, fmt.Errorf("HTTP handler '%s' not found: %v", handlerName, err)
	}

	httpHandler, ok := handlerSvc.(workflowmodule.HTTPHandler)
	if !ok {
		return nil, fmt.Errorf("service '%s' is not an HTTPHandler", handlerName)
	}

	// Create a mock HTTP request and response
	method := "GET"
	if m, ok := data["method"].(string); ok {
		method = m
	}

	// Extract query parameters
	query := make(map[string][]string)
	if params, ok := data["params"].(map[string]interface{}); ok {
		for k, v := range params {
			if str, ok := v.(string); ok {
				query[k] = []string{str}
			} else if strs, ok := v.([]string); ok {
				query[k] = strs
			} else if strs, ok := v.([]interface{}); ok {
				values := make([]string, 0, len(strs))
				for _, item := range strs {
					if str, ok := item.(string); ok {
						values = append(values, str)
					}
				}
				if len(values) > 0 {
					query[k] = values
				}
			}
		}
	}

	// Create the URL with query
	urlValues := url.Values(query)
	urlStr := "http://localhost" + path
	if len(query) > 0 {
		urlStr += "?" + urlValues.Encode()
	}

	// Create a request with the path and query
	req, err := http.NewRequestWithContext(ctx, method, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add body if present
	if body, ok := data["body"]; ok {
		var bodyBytes []byte
		switch b := body.(type) {
		case string:
			bodyBytes = []byte(b)
		case []byte:
			bodyBytes = b
		default:
			// Try to marshal as JSON
			bodyBytes, err = json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			// Set content type to JSON if not explicitly set
			if req.Header.Get("Content-Type") == "" {
				req.Header.Set("Content-Type", "application/json")
			}
		}

		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
	}

	// Add headers
	if headers, ok := data["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if str, ok := v.(string); ok {
				req.Header.Add(k, str)
			}
		}
	}

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Execute the handler
	httpHandler.Handle(rr, req)

	// Get the response
	resp := rr.Result()
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Try to parse JSON response
	var respData interface{}
	if err := json.Unmarshal(respBody, &respData); err != nil {
		// If not JSON, return as string
		respData = string(respBody)
	}

	// Return the result
	result := map[string]interface{}{
		"success":      true,
		"handler":      handlerName,
		"statusCode":   resp.StatusCode,
		"status":       resp.Status,
		"contentType":  resp.Header.Get("Content-Type"),
		"responseData": respData,
	}

	// Add headers if any
	if len(resp.Header) > 0 {
		headers := make(map[string]string)
		for k, v := range resp.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		result["headers"] = headers
	}

	return result, nil
}
