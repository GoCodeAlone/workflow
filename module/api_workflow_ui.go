package module

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

//go:embed all:ui_dist
var uiAssets embed.FS

// ExtractUIAssets extracts the embedded UI assets to destDir, preserving
// directory structure. This is used by the admin package to provide a
// filesystem path for static.fileserver to serve from.
func ExtractUIAssets(destDir string) error {
	return fs.WalkDir(uiAssets, "ui_dist", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute the relative path under ui_dist
		rel, err := filepath.Rel("ui_dist", path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0750)
		}

		data, err := uiAssets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded file %s: %w", path, err)
		}
		return os.WriteFile(target, data, 0600)
	})
}

// ServiceInfo describes a registered service for API responses.
type ServiceInfo struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Implements []string `json:"implements"`
}

// WorkflowUIHandler serves the workflow editor UI and provides API endpoints
// for managing workflow configurations.
type WorkflowUIHandler struct {
	mu           sync.RWMutex
	config       *config.WorkflowConfig
	reloadFn     func(*config.WorkflowConfig) error
	engineStatus func() map[string]any
	svcRegistry  func() map[string]any
}

// NewWorkflowUIHandler creates a new handler with an optional initial config.
func NewWorkflowUIHandler(cfg *config.WorkflowConfig) *WorkflowUIHandler {
	if cfg == nil {
		cfg = config.NewEmptyWorkflowConfig()
	}
	return &WorkflowUIHandler{config: cfg}
}

// SetReloadFunc sets the callback for reloading the engine with new config.
func (h *WorkflowUIHandler) SetReloadFunc(fn func(*config.WorkflowConfig) error) {
	h.reloadFn = fn
}

// SetStatusFunc sets the callback for getting engine status.
func (h *WorkflowUIHandler) SetStatusFunc(fn func() map[string]any) {
	h.engineStatus = fn
}

// SetServiceRegistry sets the callback for accessing the service registry.
func (h *WorkflowUIHandler) SetServiceRegistry(fn func() map[string]any) {
	h.svcRegistry = fn
}

// RegisterRoutes registers all workflow UI routes on the given mux.
func (h *WorkflowUIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workflow/config", h.handleGetConfig)
	mux.HandleFunc("PUT /api/workflow/config", h.handlePutConfig)
	mux.HandleFunc("GET /api/workflow/modules", h.handleGetModules)
	mux.HandleFunc("GET /api/workflow/services", h.handleGetServices)
	mux.HandleFunc("POST /api/workflow/validate", h.handleValidate)
	mux.HandleFunc("POST /api/workflow/reload", h.handleReload)
	mux.HandleFunc("GET /api/workflow/status", h.handleStatus)

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
	Type         string              `json:"type"`
	Label        string              `json:"label"`
	Category     string              `json:"category"`
	ConfigFields []configFieldSchema `json:"configFields"`
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
	{Type: "cache.modular", Label: "Cache", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "provider", Label: "Provider", Type: "select", Options: []string{"memory", "redis"}},
		{Key: "ttl", Label: "TTL", Type: "string", DefaultValue: "5m"},
	}},
	{Type: "database.modular", Label: "Database", Category: "infrastructure", ConfigFields: []configFieldSchema{
		{Key: "driver", Label: "Driver", Type: "select", Options: []string{"postgres", "mysql", "sqlite"}},
		{Key: "dsn", Label: "DSN", Type: "string"},
	}},
	{Type: "notification.slack", Label: "Slack Notification", Category: "integration", ConfigFields: []configFieldSchema{
		{Key: "webhookURL", Label: "Webhook URL", Type: "string"},
		{Key: "channel", Label: "Channel", Type: "string"},
		{Key: "username", Label: "Username", Type: "string", DefaultValue: "workflow-bot"},
	}},
	{Type: "storage.s3", Label: "S3 Storage", Category: "integration", ConfigFields: []configFieldSchema{
		{Key: "bucket", Label: "Bucket", Type: "string"},
		{Key: "region", Label: "Region", Type: "string", DefaultValue: "us-east-1"},
		{Key: "endpoint", Label: "Endpoint", Type: "string"},
	}},
	{Type: "messaging.nats", Label: "NATS Broker", Category: "messaging", ConfigFields: []configFieldSchema{
		{Key: "url", Label: "URL", Type: "string", DefaultValue: "nats://localhost:4222"},
	}},
	{Type: "messaging.kafka", Label: "Kafka Broker", Category: "messaging", ConfigFields: []configFieldSchema{
		{Key: "brokers", Label: "Brokers", Type: "string", DefaultValue: "localhost:9092"},
		{Key: "groupID", Label: "Group ID", Type: "string"},
	}},
	{Type: "observability.otel", Label: "OpenTelemetry", Category: "observability", ConfigFields: []configFieldSchema{
		{Key: "endpoint", Label: "OTLP Endpoint", Type: "string", DefaultValue: "localhost:4318"},
		{Key: "serviceName", Label: "Service Name", Type: "string", DefaultValue: "workflow"},
	}},
}

// ServeHTTP implements http.Handler for config-driven delegate dispatch.
// It handles both query (GET) and command (PUT/POST) operations for engine
// management, dispatching based on the last path segment.
func (h *WorkflowUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seg := lastPathSegment(r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		switch seg {
		case "config":
			h.handleGetConfig(w, r)
		case "status":
			h.handleStatus(w, r)
		case "modules":
			h.handleGetModules(w, r)
		case "services":
			h.handleGetServices(w, r)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	case http.MethodPut:
		switch seg {
		case "config":
			h.handlePutConfig(w, r)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	case http.MethodPost:
		switch seg {
		case "validate":
			h.handleValidate(w, r)
		case "reload":
			h.handleReload(w, r)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

// HandleManagement dispatches management API requests to the appropriate
// handler based on the request path. This is intended to be used as a
// handler function for an http.handler module via SetHandleFunc.
func (h *WorkflowUIHandler) HandleManagement(w http.ResponseWriter, r *http.Request) {
	h.ServeHTTP(w, r)
}

// HandleGetConfig serves the workflow configuration (GET /engine/config).
func (h *WorkflowUIHandler) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	h.handleGetConfig(w, r)
}

// HandlePutConfig updates the workflow configuration (PUT /engine/config).
func (h *WorkflowUIHandler) HandlePutConfig(w http.ResponseWriter, r *http.Request) {
	h.handlePutConfig(w, r)
}

// HandleGetModules lists available module types (GET /engine/modules).
func (h *WorkflowUIHandler) HandleGetModules(w http.ResponseWriter, r *http.Request) {
	h.handleGetModules(w, r)
}

// HandleValidate validates a workflow configuration (POST /engine/validate).
func (h *WorkflowUIHandler) HandleValidate(w http.ResponseWriter, r *http.Request) {
	h.handleValidate(w, r)
}

// HandleReload reloads the engine with the current configuration (POST /engine/reload).
func (h *WorkflowUIHandler) HandleReload(w http.ResponseWriter, r *http.Request) {
	h.handleReload(w, r)
}

// HandleStatus returns the engine status (GET /engine/status).
func (h *WorkflowUIHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	h.handleStatus(w, r)
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

// handleGetServices returns all registered services with their interface info.
func (h *WorkflowUIHandler) handleGetServices(w http.ResponseWriter, _ *http.Request) {
	services := make([]ServiceInfo, 0)

	if h.svcRegistry != nil {
		for name, svc := range h.svcRegistry() {
			info := ServiceInfo{
				Name:       name,
				Type:       "service",
				Implements: []string{},
			}
			if _, ok := svc.(http.Handler); ok {
				info.Implements = append(info.Implements, "http.Handler")
			}
			services = append(services, info)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(services); err != nil {
		http.Error(w, "failed to encode services", http.StatusInternalServerError)
	}
}

// HandleGetServices serves the services list (GET /engine/services).
func (h *WorkflowUIHandler) HandleGetServices(w http.ResponseWriter, r *http.Request) {
	h.handleGetServices(w, r)
}

type validationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

func (h *WorkflowUIHandler) handleReload(w http.ResponseWriter, r *http.Request) {
	if h.reloadFn == nil {
		http.Error(w, "reload not configured", http.StatusServiceUnavailable)
		return
	}

	h.mu.RLock()
	cfg := h.config
	h.mu.RUnlock()

	if err := h.reloadFn(cfg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}); encErr != nil {
			http.Error(w, "failed to encode error response", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "reloaded"}); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (h *WorkflowUIHandler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	status := map[string]any{
		"status": "running",
	}

	if h.engineStatus != nil {
		status = h.engineStatus()
	}

	h.mu.RLock()
	if h.config != nil {
		status["moduleCount"] = len(h.config.Modules)
		status["workflowCount"] = len(h.config.Workflows)
	}
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, "failed to encode status", http.StatusInternalServerError)
	}
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

	// Check duplicate names only among non-pipeline-step modules.
	// Pipeline steps (step.*) can share names across different pipelines.
	names := make(map[string]bool)
	allNames := make(map[string]bool)
	for _, mod := range cfg.Modules {
		allNames[mod.Name] = true
		if mod.Name == "" {
			errors = append(errors, fmt.Sprintf("module of type %s has no name", mod.Type))
		}
		if !strings.HasPrefix(mod.Type, "step.") {
			if names[mod.Name] {
				errors = append(errors, fmt.Sprintf("duplicate module name: %s", mod.Name))
			}
			names[mod.Name] = true
		}

		for _, dep := range mod.DependsOn {
			if !allNames[dep] {
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
