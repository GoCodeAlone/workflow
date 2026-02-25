package module

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// contextKey is a private type for context keys defined in this package,
// preventing collisions with keys defined in other packages.
type contextKey string

// paramsKey is the context key used to store extracted path parameters.
const paramsKey contextKey = "params"

// pathParamRe matches {name} or {name...} segments in a URL path template.
var pathParamRe = regexp.MustCompile(`\{(\w+)(?:\.\.\.)?}`)

// extractRouteParams reads all {name} placeholders from a path template
// and returns their runtime values from the request using r.PathValue.
func extractRouteParams(path string, r *http.Request) map[string]string {
	matches := pathParamRe.FindAllStringSubmatch(path, -1)
	if len(matches) == 0 {
		return nil
	}
	params := make(map[string]string, len(matches))
	for _, m := range matches {
		name := m[1]
		if v := r.PathValue(name); v != "" {
			params[name] = v
		}
	}
	return params
}

// Route represents an HTTP route
type Route struct {
	Method      string
	Path        string
	Handler     HTTPHandler
	Middlewares []HTTPMiddleware
}

// StandardHTTPRouter implements both HTTPRouter and http.Handler interfaces
type StandardHTTPRouter struct {
	name              string
	routes            []Route
	mu                sync.RWMutex
	serverDeps        []string // Names of HTTP server modules this router depends on
	serveMux          *http.ServeMux
	globalMiddlewares []HTTPMiddleware
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

// AddRouteWithMiddleware adds a route with middleware to the router.
// If the router has already been started, the internal mux is rebuilt
// so that dynamically added routes (e.g. from pipeline triggers) are served.
func (r *StandardHTTPRouter) AddRouteWithMiddleware(method, path string, handler HTTPHandler, middlewares []HTTPMiddleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Avoid duplicate routes (same method+path)
	for _, existing := range r.routes {
		if existing.Method == method && existing.Path == path {
			fmt.Printf("Route already exists, skipping: %s %s\n", method, path)
			return
		}
	}

	r.routes = append(r.routes, Route{
		Method:      method,
		Path:        path,
		Handler:     handler,
		Middlewares: middlewares,
	})

	fmt.Printf("Route added: %s %s\n", method, path)

	// Rebuild the mux if we've already started (hot-add support)
	if r.serveMux != nil {
		r.rebuildMuxLocked()
	}
}

// AddGlobalMiddleware appends a middleware that wraps every request served by
// this router, regardless of which route is matched. Global middlewares are
// applied in the order they are added, before any per-route middlewares.
// This is the correct place to attach cross-cutting concerns such as
// distributed tracing that must observe all traffic.
func (r *StandardHTTPRouter) AddGlobalMiddleware(mw HTTPMiddleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalMiddlewares = append(r.globalMiddlewares, mw)
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

// ServeHTTP implements the http.Handler interface.
// Global middlewares (e.g. OTEL tracing) are applied around the entire mux so
// every request — including health-checks and pipeline-triggered routes — is
// instrumented, regardless of how the route was registered.
func (r *StandardHTTPRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var base http.Handler
	if r.serveMux != nil {
		base = r.serveMux
	} else {
		base = http.NotFoundHandler()
	}

	// Wrap with global middlewares in reverse registration order so that the
	// first-added middleware is outermost (executes first).
	handler := base
	for i := len(r.globalMiddlewares) - 1; i >= 0; i-- {
		handler = r.globalMiddlewares[i].Process(handler)
	}

	handler.ServeHTTP(w, req)
}

// Start compiles all registered routes into the internal ServeMux.
func (r *StandardHTTPRouter) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rebuildMuxLocked()
	return nil
}

// rebuildMuxLocked creates a new ServeMux from the current routes.
// Caller must hold r.mu.
func (r *StandardHTTPRouter) rebuildMuxLocked() {
	mux := http.NewServeMux()
	for _, route := range r.routes {
		mux.HandleFunc(fmt.Sprintf("%s %s", route.Method, route.Path), func(w http.ResponseWriter, r *http.Request) {
			// Inject path params into context so triggers can read them via r.Context().Value(paramsKey).
			if params := extractRouteParams(route.Path, r); len(params) > 0 {
				r = r.WithContext(context.WithValue(r.Context(), paramsKey, params))
			}

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
			SatisfiesInterface: reflect.TypeFor[HTTPServer](),
		})
	}

	return deps
}
