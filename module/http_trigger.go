package module

import (
	"context"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/modular"
)

const (
	// HTTPTriggerName is the standard name for HTTP triggers
	HTTPTriggerName = "trigger.http"
)

// HTTPTriggerConfig represents the configuration for an HTTP trigger
type HTTPTriggerConfig struct {
	Routes []HTTPTriggerRoute `json:"routes" yaml:"routes"`
}

// HTTPTriggerRoute represents a single HTTP route configuration
type HTTPTriggerRoute struct {
	Path     string                 `json:"path" yaml:"path"`
	Method   string                 `json:"method" yaml:"method"`
	Workflow string                 `json:"workflow" yaml:"workflow"`
	Action   string                 `json:"action" yaml:"action"`
	Params   map[string]interface{} `json:"params,omitempty" yaml:"params,omitempty"`
}

// HTTPTrigger implements a trigger that starts workflows from HTTP requests
type HTTPTrigger struct {
	name      string
	namespace ModuleNamespaceProvider
	routes    []HTTPTriggerRoute
	router    HTTPRouter
	engine    WorkflowEngine
}

// WorkflowEngine defines the interface for triggering workflows
type WorkflowEngine interface {
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) error
}

// NewHTTPTrigger creates a new HTTP trigger
func NewHTTPTrigger() *HTTPTrigger {
	return NewHTTPTriggerWithNamespace(nil)
}

// NewHTTPTriggerWithNamespace creates a new HTTP trigger with namespace support
func NewHTTPTriggerWithNamespace(namespace ModuleNamespaceProvider) *HTTPTrigger {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}

	return &HTTPTrigger{
		name:      namespace.FormatName(HTTPTriggerName),
		namespace: namespace,
		routes:    make([]HTTPTriggerRoute, 0),
	}
}

// Name returns the name of this trigger
func (t *HTTPTrigger) Name() string {
	return t.name
}

// Init initializes the trigger
func (t *HTTPTrigger) Init(app modular.Application) error {
	return app.RegisterService(t.name, t)
}

// Start starts the trigger
func (t *HTTPTrigger) Start(ctx context.Context) error {
	// If no router is set, we can't start
	if t.router == nil {
		return fmt.Errorf("HTTP router not configured for HTTP trigger")
	}

	// If no engine is set, we can't start
	if t.engine == nil {
		return fmt.Errorf("workflow engine not configured for HTTP trigger")
	}

	// Register all routes with the router
	for _, route := range t.routes {
		t.router.AddRoute(route.Method, route.Path, t.createHandler(route))
	}

	return nil
}

// Stop stops the trigger
func (t *HTTPTrigger) Stop(ctx context.Context) error {
	// Nothing to do here as the HTTP server will be stopped elsewhere
	return nil
}

// Configure sets up the trigger from configuration
func (t *HTTPTrigger) Configure(app modular.Application, triggerConfig interface{}) error {
	// Convert the generic config to HTTP trigger config
	config, ok := triggerConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid HTTP trigger configuration format")
	}

	// Extract routes from configuration
	routesConfig, ok := config["routes"].([]interface{})
	if !ok {
		return fmt.Errorf("routes not found in HTTP trigger configuration")
	}

	// Find the HTTP router
	var router HTTPRouter
	routerNames := []string{"httpRouter", "api-router"}

	for _, name := range routerNames {
		var svc interface{}
		if err := app.GetService(name, &svc); err == nil && svc != nil {
			if r, ok := svc.(HTTPRouter); ok {
				router = r
				break
			}
		}
	}

	if router == nil {
		return fmt.Errorf("HTTP router not found")
	}

	// Find the workflow engine
	var engine WorkflowEngine
	engineNames := []string{"workflowEngine", "engine"}

	for _, name := range engineNames {
		var svc interface{}
		if err := app.GetService(name, &svc); err == nil && svc != nil {
			if e, ok := svc.(WorkflowEngine); ok {
				engine = e
				break
			}
		}
	}

	if engine == nil {
		return fmt.Errorf("workflow engine not found")
	}

	// Store router and engine references
	t.router = router
	t.engine = engine

	// Parse routes
	for i, rc := range routesConfig {
		routeMap, ok := rc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid route configuration at index %d", i)
		}

		path, _ := routeMap["path"].(string)
		method, _ := routeMap["method"].(string)
		workflow, _ := routeMap["workflow"].(string)
		action, _ := routeMap["action"].(string)

		if path == "" || method == "" || workflow == "" || action == "" {
			return fmt.Errorf("incomplete route configuration at index %d: path, method, workflow and action are required", i)
		}

		// Get optional params
		params, _ := routeMap["params"].(map[string]interface{})

		// Add the route
		t.routes = append(t.routes, HTTPTriggerRoute{
			Path:     path,
			Method:   method,
			Workflow: workflow,
			Action:   action,
			Params:   params,
		})
	}

	return nil
}

// createHandler creates an HTTP handler for a specific route
func (t *HTTPTrigger) createHandler(route HTTPTriggerRoute) HTTPHandler {
	// Create a handler function that will be called when a request is received
	handlerFn := func(w http.ResponseWriter, r *http.Request) {
		// Extract path parameters from the context (would have been set by the router)
		params := make(map[string]string)
		if routeParams, ok := r.Context().Value("params").(map[string]string); ok {
			params = routeParams
		}

		// Create a context for the workflow
		ctx := r.Context()

		// Extract data from the request to pass to the workflow
		data := make(map[string]interface{})

		// Add URL params from context
		for k, v := range params {
			data[k] = v
		}

		// Add query params
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				data[k] = v[0]
			}
		}

		// Add any static params from the route configuration
		for k, v := range route.Params {
			data[k] = v
		}

		// Call the workflow engine to trigger the workflow
		err := t.engine.TriggerWorkflow(ctx, route.Workflow, route.Action, data)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error triggering workflow: %v", err), http.StatusInternalServerError)
			return
		}

		// Return a success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status": "workflow triggered"}`))
	}

	// Create an HTTP handler using the standard adapter
	return &StandardHTTPHandler{handlerFn}
}

// StandardHTTPHandler adapts a function to the HTTPHandler interface
type StandardHTTPHandler struct {
	handlerFunc func(http.ResponseWriter, *http.Request)
}

// Handle implements the HTTPHandler interface
func (h *StandardHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.handlerFunc(w, r)
}

// ServeHTTP implements the http.Handler interface (for compatibility)
func (h *StandardHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, params map[string]string) {
	h.handlerFunc(w, r)
}
