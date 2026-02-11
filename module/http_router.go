package module

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	"github.com/CrisisTextLine/modular"
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
	serveMux   *http.ServeMux
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

	r.routes = append(r.routes, Route{
		Method:      method,
		Path:        path,
		Handler:     handler,
		Middlewares: middlewares,
	})

	fmt.Printf("Route added: %s %s\n", method, path)
}

// HasRoute checks if a route with the given method and path already exists
func (r *StandardHTTPRouter) HasRoute(method, path string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, route := range r.routes {
		if route.Method == method && route.Path == path {
			return true
		}
	}
	return false
}

// ServeHTTP implements the http.Handler interface
func (r *StandardHTTPRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.serveMux != nil {
		r.serveMux.ServeHTTP(w, req)
	} else {
		http.NotFound(w, req)
	}
}

// Start is a no-op for router (implements Startable interface)
func (r *StandardHTTPRouter) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	mux := http.NewServeMux()
	for _, route := range r.routes {
		mux.HandleFunc(fmt.Sprintf("%s %s", route.Method, route.Path), func(w http.ResponseWriter, r *http.Request) {
			// Create handler chain with middleware
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				route.Handler.Handle(w, r)
			})

			// Apply middlewares in reverse order so they execute in the order they were added
			if route.Middlewares != nil {
				for i := len(route.Middlewares) - 1; i >= 0; i-- {
					handler = route.Middlewares[i].Process(handler)
				}
			}

			// Execute the handler chain
			handler.ServeHTTP(w, r)
		})
	}

	r.serveMux = mux

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
