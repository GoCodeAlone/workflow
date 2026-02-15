package module

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// QueryFunc is a read-only query function that returns data or an error.
type QueryFunc func(ctx context.Context, r *http.Request) (any, error)

// QueryHandler dispatches GET requests to named query functions.
// Each query is registered by name and dispatched by extracting the last
// path segment from the request URL. Route pipelines can be attached for
// composable per-route processing. A delegate service can be configured
// to handle requests that don't match any registered query name.
type QueryHandler struct {
	name            string
	delegate        string // service name to resolve as http.Handler
	delegateHandler http.Handler
	app             modular.Application
	queries         map[string]QueryFunc
	routePipelines  map[string]*Pipeline
	mu              sync.RWMutex
}

// NewQueryHandler creates a new QueryHandler with the given name.
func NewQueryHandler(name string) *QueryHandler {
	return &QueryHandler{
		name:           name,
		queries:        make(map[string]QueryFunc),
		routePipelines: make(map[string]*Pipeline),
	}
}

// SetRoutePipeline attaches a pipeline to a specific route path.
func (h *QueryHandler) SetRoutePipeline(routePath string, pipeline *Pipeline) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routePipelines[routePath] = pipeline
}

// Name returns the unique identifier for this module.
func (h *QueryHandler) Name() string {
	return h.name
}

// SetDelegate sets the delegate service name. The service must implement
// http.Handler and will be resolved from the service registry during Init.
func (h *QueryHandler) SetDelegate(name string) {
	h.delegate = name
}

// SetDelegateHandler directly sets the HTTP handler used for delegation.
func (h *QueryHandler) SetDelegateHandler(handler http.Handler) {
	h.delegateHandler = handler
}

// Init initializes the query handler and resolves the delegate service.
func (h *QueryHandler) Init(app modular.Application) error {
	h.app = app
	if h.delegate != "" {
		h.resolveDelegate()
	}
	return nil
}

// resolveDelegate looks up the delegate service and checks for http.Handler.
func (h *QueryHandler) resolveDelegate() {
	if h.app == nil || h.delegate == "" {
		return
	}
	svc, ok := h.app.SvcRegistry()[h.delegate]
	if !ok {
		return
	}
	if handler, ok := svc.(http.Handler); ok {
		h.delegateHandler = handler
	}
}

// ResolveDelegatePostStart is called after engine.Start to resolve delegates
// that may not have been available during Init (e.g., services registered by
// post-start hooks).
func (h *QueryHandler) ResolveDelegatePostStart() {
	if h.delegate != "" && h.delegateHandler == nil {
		h.resolveDelegate()
	}
}

// RegisterQuery adds a named query function to the handler.
func (h *QueryHandler) RegisterQuery(name string, fn QueryFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queries[name] = fn
}

// Handle dispatches an HTTP request to the appropriate query function.
func (h *QueryHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.ServeHTTP(w, r)
}

// ServeHTTP implements the http.Handler interface. It extracts the query
// name from the last path segment and dispatches to the registered function.
// Dispatch chain: RegisteredQueryFunc -> RoutePipeline -> DelegateHandler -> 404
func (h *QueryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	queryName := lastPathSegment(r.URL.Path)

	h.mu.RLock()
	fn, exists := h.queries[queryName]
	pipeline := h.routePipelines[queryName]
	h.mu.RUnlock()

	if exists {
		result, err := fn(r.Context(), r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	if pipeline != nil {
		triggerData := map[string]any{
			"method":    r.Method,
			"path":      r.URL.Path,
			"queryName": queryName,
			"query":     r.URL.Query(),
		}
		// Inject HTTP context so delegate steps can forward directly
		pipeline.Metadata = map[string]any{
			"_http_request":         r,
			"_http_response_writer": w,
		}
		if pipeline.RoutePattern != "" {
			pipeline.Metadata["_route_pattern"] = pipeline.RoutePattern
		}
		pc, err := pipeline.Execute(r.Context(), triggerData)
		if err != nil {
			// Only write error if response wasn't already handled by a delegate step
			if pc == nil || pc.Metadata["_response_handled"] != true {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			}
			return
		}
		// If response was handled by a delegate step, don't write again
		if pc.Metadata["_response_handled"] == true {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pc.Current); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	if h.delegateHandler != nil {
		h.delegateHandler.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown query: " + queryName})
}

// ProvidesServices returns a list of services provided by this module.
func (h *QueryHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        h.name,
			Description: "Query Handler",
			Instance:    h,
		},
	}
}

// RequiresServices returns a list of services required by this module.
func (h *QueryHandler) RequiresServices() []modular.ServiceDependency {
	if h.delegate != "" {
		return []modular.ServiceDependency{
			{
				Name:     h.delegate,
				Required: false,
			},
		}
	}
	return nil
}

// lastPathSegment extracts the last non-empty segment from a URL path.
// For example, "/api/v1/admin/engine/config" returns "config".
func lastPathSegment(path string) string {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
