package module

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

//go:embed all:ui_dist
var uiAssets embed.FS

// WorkflowUIHandler serves the workflow editor UI and provides API endpoints
// for managing workflow configurations.
type WorkflowUIHandler struct {
	mu     sync.RWMutex
	config *config.WorkflowConfig
}

// NewWorkflowUIHandler creates a new handler with an optional initial config.
func NewWorkflowUIHandler(cfg *config.WorkflowConfig) *WorkflowUIHandler {
	if cfg == nil {
		cfg = config.NewEmptyWorkflowConfig()
	}
	return &WorkflowUIHandler{config: cfg}
}

// RegisterRoutes registers all workflow UI routes on the given mux.
func (h *WorkflowUIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workflow/config", h.handleGetConfig)
	mux.HandleFunc("PUT /api/workflow/config", h.handlePutConfig)
	mux.HandleFunc("GET /api/workflow/modules", h.handleGetModules)
	mux.HandleFunc("POST /api/workflow/validate", h.handleValidate)

	// Serve the embedded UI assets
	uiFS, err := fs.Sub(uiAssets, "ui_dist")
	if err != nil {
		panic(fmt.Sprintf("failed to create sub filesystem: %v", err))
	}
	fileServer := http.FileServer(http.FS(uiFS))
	mux.Handle("/", fileServer)
}

func (h *WorkflowUIHandler) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.config); err != nil {
		http.Error(w, "failed to encode config", http.StatusInternalServerError)
	}
}

func (h *WorkflowUIHandler) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	var cfg config.WorkflowConfig
	switch contentType {
	case "application/x-yaml", "text/yaml":
		decoder := yaml.NewDecoder(r.Body)
		if err := decoder.Decode(&cfg); err != nil {
			http.Error(w, fmt.Sprintf("invalid YAML: %v", err), http.StatusBadRequest)
			return
		}
	default:
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
	}

	h.mu.Lock()
	h.config = &cfg
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (h *WorkflowUIHandler) handleGetModules(w http.ResponseWriter, r *http.Request) {
	h.handleGetModulesImpl(w, r)
}

type moduleTypeDef struct {
	Type          string              `json:"type"`
	Label         string              `json:"label"`
	Category      string              `json:"category"`
	ConfigFields  []configFieldSchema `json:"configFields"`
}

type configFieldSchema struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	Options      []string `json:"options,omitempty"`
	DefaultValue any      `json:"defaultValue,omitempty"`
}

var availableModules = []moduleTypeDef{
	{Type: "http.server", Label: "HTTP Server", Category: "http", ConfigFields: []configFieldSchema{
		{Key: "address", Label: "Address", Type: "string", DefaultValue: ":8080"},
	}},
	{Type: "http.router", Label: "HTTP Router", Category: "http", ConfigFields: []configFieldSchema{
		{Key: "prefix", Label: "Path Prefix", Type: "string"},
	}},
	{Type: "http.handler", Label: "HTTP Handler", Category: "http", ConfigFields: []configFieldSchema{
		{Key: "method", Label: "Method", Type: "select", Options: []string{"GET", "POST", "PUT", "DELETE"}},
		{Key: "path", Label: "Path", Type: "string"},
	}},
	{Type: "http.proxy", Label: "HTTP Proxy", Category: "http", ConfigFields: []configFieldSchema{
		{Key: "target", Label: "Target URL", Type: "string"},
	}},
	{Type: "api.handler", Label: "API Handler", Category: "http", ConfigFields: []configFieldSchema{
		{Key: "method", Label: "Method", Type: "select", Options: []string{"GET", "POST", "PUT", "DELETE"}},
		{Key: "path", Label: "Path", Type: "string"},
	}},
	{Type: "http.middleware.auth", Label: "Auth Middleware", Category: "middleware", ConfigFields: []configFieldSchema{
		{Key: "type", Label: "Auth Type", Type: "select", Options: []string{"jwt", "basic", "apikey"}},
	}},
	{Type: "http.middleware.logging", Label: "Logging Middleware", Category: "middleware", ConfigFields: []configFieldSchema{
		{Key: "level", Label: "Log Level", Type: "select", Options: []string{"debug", "info", "warn", "error"}},
	}},
	{Type: "http.middleware.ratelimit", Label: "Rate Limiter", Category: "middleware", ConfigFields: []configFieldSchema{
		{Key: "rps", Label: "Requests/sec", Type: "number", DefaultValue: 100},
	}},
	{Type: "http.middleware.cors", Label: "CORS Middleware", Category: "middleware", ConfigFields: []configFieldSchema{
		{Key: "allowOrigins", Label: "Allowed Origins", Type: "string", DefaultValue: "*"},
	}},
	{Type: "messaging.broker", Label: "Message Broker", Category: "messaging", ConfigFields: []configFieldSchema{
		{Key: "provider", Label: "Provider", Type: "select", Options: []string{"nats", "rabbitmq", "kafka"}},
		{Key: "url", Label: "URL", Type: "string"},
	}},
	{Type: "messaging.handler", Label: "Message Handler", Category: "messaging", ConfigFields: []configFieldSchema{
		{Key: "topic", Label: "Topic", Type: "string"},
	}},
	{Type: "statemachine.engine", Label: "State Machine", Category: "statemachine", ConfigFields: []configFieldSchema{
		{Key: "initialState", Label: "Initial State", Type: "string"},
	}},
	{Type: "state.tracker", Label: "State Tracker", Category: "statemachine", ConfigFields: []configFieldSchema{
		{Key: "store", Label: "Store Type", Type: "select", Options: []string{"memory", "redis", "database"}},
	}},
	{Type: "state.connector", Label: "State Connector", Category: "statemachine"},
	{Type: "scheduler.modular", Label: "Scheduler", Category: "scheduling", ConfigFields: []configFieldSchema{
		{Key: "interval", Label: "Interval", Type: "string", DefaultValue: "1m"},
		{Key: "cron", Label: "Cron Expression", Type: "string"},
	}},
	{Type: "auth.modular", Label: "Auth Service", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "provider", Label: "Provider", Type: "select", Options: []string{"jwt", "oauth2", "apikey"}},
	}},
	{Type: "eventbus.modular", Label: "Event Bus", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "bufferSize", Label: "Buffer Size", Type: "number", DefaultValue: 1024},
	}},
	{Type: "cache.modular", Label: "Cache", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "provider", Label: "Provider", Type: "select", Options: []string{"memory", "redis"}},
		{Key: "ttl", Label: "TTL", Type: "string", DefaultValue: "5m"},
	}},
	{Type: "chimux.router", Label: "Chi Mux Router", Category: "http", ConfigFields: []configFieldSchema{
		{Key: "prefix", Label: "Path Prefix", Type: "string"},
	}},
	{Type: "eventlogger.modular", Label: "Event Logger", Category: "events", ConfigFields: []configFieldSchema{
		{Key: "output", Label: "Output", Type: "select", Options: []string{"stdout", "file", "database"}},
	}},
	{Type: "httpclient.modular", Label: "HTTP Client", Category: "integration", ConfigFields: []configFieldSchema{
		{Key: "baseURL", Label: "Base URL", Type: "string"},
		{Key: "timeout", Label: "Timeout", Type: "string", DefaultValue: "30s"},
	}},
	{Type: "database.modular", Label: "Database", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "driver", Label: "Driver", Type: "select", Options: []string{"postgres", "mysql", "sqlite"}},
		{Key: "dsn", Label: "DSN", Type: "string"},
	}},
	{Type: "jsonschema.modular", Label: "JSON Schema Validator", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "schema", Label: "Schema", Type: "json"},
	}},
}

func init() {
	// This ensures handleGetModules is implemented at package init time.
	// The handler function is set as a method below.
}

// handleGetModulesImpl is the actual implementation for the modules endpoint.
func (h *WorkflowUIHandler) handleGetModulesImpl(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(availableModules); err != nil {
		http.Error(w, "failed to encode modules", http.StatusInternalServerError)
	}
}

type validationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

func (h *WorkflowUIHandler) handleValidate(w http.ResponseWriter, r *http.Request) {
	var cfg config.WorkflowConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	var errors []string

	if len(cfg.Modules) == 0 {
		errors = append(errors, "workflow has no modules")
	}

	names := make(map[string]bool)
	for _, mod := range cfg.Modules {
		if mod.Name == "" {
			errors = append(errors, fmt.Sprintf("module of type %s has no name", mod.Type))
		}
		if names[mod.Name] {
			errors = append(errors, fmt.Sprintf("duplicate module name: %s", mod.Name))
		}
		names[mod.Name] = true

		for _, dep := range mod.DependsOn {
			if !names[dep] {
				// Check if the dependency exists anywhere in the modules list
				found := false
				for _, m := range cfg.Modules {
					if m.Name == dep {
						found = true
						break
					}
				}
				if !found {
					errors = append(errors, fmt.Sprintf("%s depends on unknown module: %s", mod.Name, dep))
				}
			}
		}
	}

	result := validationResult{
		Valid:  len(errors) == 0,
		Errors: errors,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, "failed to encode result", http.StatusInternalServerError)
	}
}
