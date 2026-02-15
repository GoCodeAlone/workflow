package module

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// CommandFunc is a state-changing command function that returns a result or an error.
type CommandFunc func(ctx context.Context, r *http.Request) (any, error)

// CommandHandler dispatches POST/PUT/DELETE requests to named command functions.
// Each command is registered by name and dispatched by extracting the last
// path segment from the request URL. Route pipelines can be attached for
// composable per-route processing. A delegate service can be configured
// to handle requests that don't match any registered command name.
type CommandHandler struct {
	name            string
	delegate        string // service name to resolve as http.Handler
	delegateHandler http.Handler
	app             modular.Application
	commands        map[string]CommandFunc
	routePipelines  map[string]*Pipeline
	mu              sync.RWMutex
}

// NewCommandHandler creates a new CommandHandler with the given name.
func NewCommandHandler(name string) *CommandHandler {
	return &CommandHandler{
		name:           name,
		commands:       make(map[string]CommandFunc),
		routePipelines: make(map[string]*Pipeline),
	}
}

// SetRoutePipeline attaches a pipeline to a specific route path.
func (h *CommandHandler) SetRoutePipeline(routePath string, pipeline *Pipeline) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routePipelines[routePath] = pipeline
}

// Name returns the unique identifier for this module.
func (h *CommandHandler) Name() string {
	return h.name
}

// SetDelegate sets the delegate service name. The service must implement
// http.Handler and will be resolved from the service registry during Init.
func (h *CommandHandler) SetDelegate(name string) {
	h.delegate = name
}

// SetDelegateHandler directly sets the HTTP handler used for delegation.
func (h *CommandHandler) SetDelegateHandler(handler http.Handler) {
	h.delegateHandler = handler
}

// Init initializes the command handler and resolves the delegate service.
func (h *CommandHandler) Init(app modular.Application) error {
	h.app = app
	if h.delegate != "" {
		h.resolveDelegate()
	}
	return nil
}

// resolveDelegate looks up the delegate service and checks for http.Handler.
func (h *CommandHandler) resolveDelegate() {
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
func (h *CommandHandler) ResolveDelegatePostStart() {
	if h.delegate != "" && h.delegateHandler == nil {
		h.resolveDelegate()
	}
}

// RegisterCommand adds a named command function to the handler.
func (h *CommandHandler) RegisterCommand(name string, fn CommandFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands[name] = fn
}

// Handle dispatches an HTTP request to the appropriate command function.
func (h *CommandHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.ServeHTTP(w, r)
}

// ServeHTTP implements the http.Handler interface. It extracts the command
// name from the last path segment and dispatches to the registered function.
// Dispatch chain: RegisteredCommandFunc -> RoutePipeline -> DelegateHandler -> 404
func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	commandName := lastPathSegment(r.URL.Path)

	h.mu.RLock()
	fn, exists := h.commands[commandName]
	pipeline := h.routePipelines[commandName]
	h.mu.RUnlock()

	if exists {
		result, err := fn(r.Context(), r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if result == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := json.NewEncoder(w).Encode(result); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	if pipeline != nil {
		triggerData := map[string]any{
			"method":      r.Method,
			"path":        r.URL.Path,
			"commandName": commandName,
		}
		// Parse request body into trigger data
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			triggerData["body"] = body
		}
		pc, err := pipeline.Execute(r.Context(), triggerData)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
	json.NewEncoder(w).Encode(map[string]string{"error": "unknown command: " + commandName})
}

// ProvidesServices returns a list of services provided by this module.
func (h *CommandHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        h.name,
			Description: "Command Handler",
			Instance:    h,
		},
	}
}

// RequiresServices returns a list of services required by this module.
func (h *CommandHandler) RequiresServices() []modular.ServiceDependency {
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
