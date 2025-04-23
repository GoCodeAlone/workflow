package module

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/GoCodeAlone/modular"
)

// Route represents an HTTP route
type Route struct {
	Method      string
	Path        string
	Handler     HTTPHandler
	Middlewares []HTTPMiddleware
}

// StandardHTTPRouter implements both HTTPRouter and http.Handler interfaces
type StandardHTTPRouter struct {
	name       string
	routes     []Route
	mu         sync.RWMutex
	serverDeps []string // Names of HTTP server modules this router depends on
	logger     modular.Logger
}

// NewStandardHTTPRouter creates a new HTTP router
func NewStandardHTTPRouter(name string) *StandardHTTPRouter {
	return &StandardHTTPRouter{
		name:       name,
		routes:     make([]Route, 0),
		serverDeps: []string{}, // Empty default dependency list
	}
}

// Name returns the unique identifier for this module
func (r *StandardHTTPRouter) Name() string {
	return r.name
}

// Constructor returns a function to construct this module with dependencies
func (r *StandardHTTPRouter) Constructor() modular.ModuleConstructor {
	return func(app modular.Application, services map[string]any) (modular.Module, error) {
		// Create a new router with the same name
		router := NewStandardHTTPRouter(r.name)

		// Find HTTP server in provided services and connect them
		for name, service := range services {
			if server, ok := service.(HTTPServer); ok {
				fmt.Printf("Router %s connecting to HTTP server %s\n", r.name, name)
				server.AddRouter(router)
				break
			}
		}

		return router, nil
	}
}

// Dependencies returns names of other modules this module depends on
func (r *StandardHTTPRouter) Dependencies() []string {
	return r.serverDeps
}

// SetServerDependencies sets which HTTP server modules this router depends on
func (r *StandardHTTPRouter) SetServerDependencies(serverNames []string) {
	r.serverDeps = serverNames
}

// Init initializes the module with the application context
func (r *StandardHTTPRouter) Init(app modular.Application) error {
	r.logger = app.Logger()
	return nil
}

// AddRoute adds a route to the router
func (r *StandardHTTPRouter) AddRoute(method, path string, handler HTTPHandler) {
	r.AddRouteWithMiddleware(method, path, handler, nil)
}

// AddRouteWithMiddleware adds a route with middleware to the router
func (r *StandardHTTPRouter) AddRouteWithMiddleware(method, path string, handler HTTPHandler, middlewares []HTTPMiddleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Ensure handler has a name for logging
	handlerName := "unknown"
	if named, ok := handler.(interface{ Name() string }); ok {
		handlerName = named.Name()
	}

	r.routes = append(r.routes, Route{
		Method:      method,
		Path:        path,
		Handler:     handler,
		Middlewares: middlewares,
	})

	// Use logger if available, otherwise fmt.Printf
	if r.logger != nil {
		r.logger.Info("Route added", "method", method, "path", path, "handler", handlerName)
	} else {
		fmt.Printf("Route added: %s %s -> %s\n", method, path, handlerName)
	}
}

// ServeHTTP implements the http.Handler interface with custom routing logic
func (r *StandardHTTPRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.logger != nil {
		r.logger.Info("Router ServeHTTP called", "method", req.Method, "path", req.URL.Path, "remoteAddr", req.RemoteAddr)
		r.logger.Debug("Router checking request", "method", req.Method, "path", req.URL.Path) // Add Debug log
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matchedRoute *Route
	var potentialRoutes []string // Log potential matches

	// Find the best matching route (exact path and method match)
	for i := range r.routes {
		route := r.routes[i] // Use index to get route safely

		// Log every route being checked for the specific request path
		if req.URL.Path == route.Path {
			potentialRoutes = append(potentialRoutes, fmt.Sprintf("%s %s", route.Method, route.Path))
		}

		// Check for exact path match
		if req.URL.Path == route.Path {
			// Check if method matches
			if req.Method == route.Method {
				matchedRoute = &route
				if r.logger != nil {
					r.logger.Debug("Router matched route", "method", route.Method, "path", route.Path) // Add Debug log
				}
				break // Found exact match, stop searching
			}
		}
		// Note: Prefix matching (e.g., for /static/) is not implemented here
		// but could be added if needed.
	}
	if r.logger != nil && len(potentialRoutes) > 0 {
		r.logger.Debug("Potential routes for path", "path", req.URL.Path, "routes", strings.Join(potentialRoutes, ", "))
	}

	// Execute the matched route's handler
	if matchedRoute != nil {
		handlerName := "unknown"
		if named, ok := matchedRoute.Handler.(interface{ Name() string }); ok {
			handlerName = named.Name()
		}
		if r.logger != nil {
			r.logger.Info("Routing request", "method", req.Method, "path", req.URL.Path, "handler", handlerName)
		}

		// Create handler chain starting with the final handler
		var finalHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			matchedRoute.Handler.Handle(w, r)
		})

		// Apply middlewares in reverse order so they execute in the configured order
		if matchedRoute.Middlewares != nil {
			for i := len(matchedRoute.Middlewares) - 1; i >= 0; i-- {
				finalHandler = matchedRoute.Middlewares[i].Process(finalHandler)
			}
		}

		// Execute the full handler chain (middlewares + final handler)
		finalHandler.ServeHTTP(w, req)
		return // Request handled
	}

	// No matching route found
	if r.logger != nil {
		r.logger.Warn("No matching route found for request", "method", req.Method, "path", req.URL.Path)
	}
	http.NotFound(w, req)
}

// Start logs the router initialization.
func (r *StandardHTTPRouter) Start(ctx context.Context) error {
	if r.logger != nil {
		r.logger.Info("HTTP Router started", "name", r.name, "routes_configured", len(r.routes))
	}
	// No need to create or manage an internal serveMux anymore
	return nil
}

// Stop is a no-op for router (implements Stoppable interface)
func (r *StandardHTTPRouter) Stop(ctx context.Context) error {
	return nil // Nothing to stop
}

// ProvidesServices returns a list of services provided by this module
func (r *StandardHTTPRouter) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        r.name,
			Description: "HTTP Router",
			Instance:    r,
		},
	}
}

// RequiresServices returns a list of services required by this module
func (r *StandardHTTPRouter) RequiresServices() []modular.ServiceDependency {
	deps := make([]modular.ServiceDependency, 0, len(r.serverDeps))

	// Create a dependency for each HTTP server this router depends on
	for _, serverName := range r.serverDeps {
		deps = append(deps, modular.ServiceDependency{
			Name:               serverName,
			Required:           true,
			SatisfiesInterface: reflect.TypeOf((*HTTPServer)(nil)).Elem(),
		})
	}

	return deps
}
