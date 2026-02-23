package module

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// CommandFunc is a state-changing command function that returns a result or an error.
type CommandFunc func(ctx context.Context, r *http.Request) (any, error)

// CommandHandler dispatches POST/PUT/DELETE requests to named command functions.
// Each command is registered by name and dispatched by extracting the last
// path segment from the request URL. Route pipelines can be attached for
// composable per-route processing. A delegate service can be configured
// to handle requests that don't match any registered command name.
type CommandHandler struct {
	name             string
	delegate         string // service name to resolve as http.Handler
	delegateHandler  http.Handler
	app              modular.Application
	commands         map[string]CommandFunc
	routePipelines   map[string]interfaces.PipelineRunner
	executionTracker ExecutionTrackerProvider
	mu               sync.RWMutex
}

// NewCommandHandler creates a new CommandHandler with the given name.
func NewCommandHandler(name string) *CommandHandler {
	return &CommandHandler{
		name:           name,
		commands:       make(map[string]CommandFunc),
		routePipelines: make(map[string]interfaces.PipelineRunner),
	}
}

// SetRoutePipeline attaches a pipeline to a specific route path.
func (h *CommandHandler) SetRoutePipeline(routePath string, pipeline interfaces.PipelineRunner) {
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

// SetExecutionTracker sets the execution tracker for recording pipeline executions.
func (h *CommandHandler) SetExecutionTracker(t ExecutionTrackerProvider) {
	h.executionTracker = t
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

// ServeHTTP implements the http.Handler interface. It looks up a route pipeline
// by the full "METHOD /path" pattern (set by Go 1.22+ ServeMux), falling back
// to the last path segment for backward compatibility with registered commands.
// Dispatch chain: RegisteredCommandFunc -> RoutePipeline -> DelegateHandler -> 404
func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	commandName := lastPathSegment(r.URL.Path)
	// Use Go 1.22+ pattern for pipeline lookup (avoids last-segment collisions)
	routeKey := r.Pattern

	h.mu.RLock()
	fn, exists := h.commands[commandName]
	pipeline := h.routePipelines[routeKey]
	if pipeline == nil {
		// Fallback: try last-segment lookup for backward compatibility
		pipeline = h.routePipelines[commandName]
	}
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
		// Buffer the request body so it can be read by both trigger data parsing
		// and downstream delegate steps that forward the original request.
		bodyBytes, _ := io.ReadAll(r.Body)
		if len(bodyBytes) > 0 {
			var body map[string]any
			if json.Unmarshal(bodyBytes, &body) == nil {
				triggerData["body"] = body
			}
			// Restore the body so delegate steps can re-read it
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		// Type-assert to *Pipeline for concrete field access (Metadata, RoutePattern,
		// Execute) and execution tracker integration. All engine-registered pipelines
		// are *Pipeline; the interface allows custom implementations in tests/plugins.
		if concretePipeline, ok := pipeline.(*Pipeline); ok {
			// Inject HTTP context so delegate steps can forward directly
			concretePipeline.Metadata = map[string]any{
				"_http_request":         r,
				"_http_response_writer": w,
			}
			if concretePipeline.RoutePattern != "" {
				concretePipeline.Metadata["_route_pattern"] = concretePipeline.RoutePattern
			}
			var pc *PipelineContext
			var err error
			if h.executionTracker != nil {
				pc, err = h.executionTracker.TrackPipelineExecution(r.Context(), concretePipeline, triggerData, r)
			} else {
				pc, err = concretePipeline.Execute(r.Context(), triggerData)
			}
			if err != nil {
				if pc == nil || pc.Metadata["_response_handled"] != true {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				}
				return
			}
			if pc.Metadata["_response_handled"] == true {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(pc.Current); err != nil {
				http.Error(w, "failed to encode response", http.StatusInternalServerError)
			}
			return
		}
		// Fallback for non-*Pipeline implementations: use the PipelineRunner interface.
		result, err := pipeline.Run(r.Context(), triggerData)
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

	if h.delegateHandler != nil {
		h.delegateHandler.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown command: " + commandName})
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
