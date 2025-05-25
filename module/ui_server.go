package module

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
)

//go:embed embedded_files
var staticFiles embed.FS

// UIServer represents a server for the workflow UI
type UIServer struct {
	name       string
	address    string
	router     *http.ServeMux
	httpServer *http.Server
	engine     UIWorkflowEngine
	logger     *slog.Logger
	configs    map[string]*config.WorkflowConfig
}

// UIWorkflowEngine interface for managing workflows in the UI
type UIWorkflowEngine interface {
	GetWorkflows() map[string]interface{}
	BuildFromConfig(cfg *config.WorkflowConfig) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// NewUIServer creates a new UI server
func NewUIServer(name string, address string, logger modular.Logger) *UIServer {
	var slogger *slog.Logger
	if sl, ok := logger.(*slog.Logger); ok {
		slogger = sl
	} else {
		slogger = slog.Default()
	}

	return &UIServer{
		name:    name,
		address: address,
		router:  http.NewServeMux(),
		configs: make(map[string]*config.WorkflowConfig),
		logger:  slogger,
	}
}

// Name returns the module name
func (s *UIServer) Name() string {
	return s.name
}

// Init initializes the UI server (implements modular.Module interface)
func (s *UIServer) Init(app modular.Application) error {
	// No special initialization needed
	return nil
}

// Start starts the UI server
func (s *UIServer) Start(ctx context.Context) error {
	s.logger.Info("Starting UI server", "address", s.address)

	// Set up API routes
	s.router.HandleFunc("/api/workflows", s.handleWorkflows)
	s.router.HandleFunc("/api/workflows/", s.handleWorkflowDetail)
	
	// Set up static files server
	staticFS, err := fs.Sub(staticFiles, "embedded_files")
	if err != nil {
		return fmt.Errorf("failed to create static files sub-filesystem: %w", err)
	}
	
	s.router.Handle("/", http.FileServer(http.FS(staticFS)))

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:    s.address,
		Handler: s.router,
	}

	// Start the HTTP server in a goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("UI server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the UI server
func (s *UIServer) Stop(ctx context.Context) error {
	s.logger.Info("Stopping UI server")
	return s.httpServer.Shutdown(ctx)
}

// SetEngine sets the workflow engine reference
func (s *UIServer) SetEngine(engine UIWorkflowEngine) {
	s.engine = engine
}

// handleWorkflows handles requests to the /api/workflows endpoint
func (s *UIServer) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listWorkflows(w, r)
	case http.MethodPost:
		s.createWorkflow(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWorkflowDetail handles requests to the /api/workflows/{name} endpoint
func (s *UIServer) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	// Extract workflow name from the URL path
	workflowName := strings.TrimPrefix(r.URL.Path, "/api/workflows/")
	if workflowName == "" {
		http.Error(w, "Workflow name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getWorkflow(w, r, workflowName)
	case http.MethodPut:
		s.updateWorkflow(w, r, workflowName)
	case http.MethodDelete:
		s.deleteWorkflow(w, r, workflowName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listWorkflows lists all workflows
func (s *UIServer) listWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows := []map[string]interface{}{}

	// If engine is available, get active workflows
	if s.engine != nil {
		activeWorkflows := s.engine.GetWorkflows()
		for name, workflow := range activeWorkflows {
			workflows = append(workflows, map[string]interface{}{
				"name":   name,
				"status": "active",
				"data":   workflow,
			})
		}
	}

	// Add stored workflow configurations
	for name, cfg := range s.configs {
		workflows = append(workflows, map[string]interface{}{
			"name":        name,
			"status":      "configured",
			"description": cfg.Description,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workflows": workflows,
	})
}

// getWorkflow gets details of a specific workflow
func (s *UIServer) getWorkflow(w http.ResponseWriter, r *http.Request, name string) {
	// First check in stored configs
	if cfg, exists := s.configs[name]; exists {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    name,
			"status":  "configured",
			"config":  cfg,
		})
		return
	}

	// If engine is available, check active workflows
	if s.engine != nil {
		workflows := s.engine.GetWorkflows()
		if workflow, exists := workflows[name]; exists {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":   name,
				"status": "active",
				"data":   workflow,
			})
			return
		}
	}

	http.Error(w, "Workflow not found", http.StatusNotFound)
}

// createWorkflow creates a new workflow
func (s *UIServer) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var newWorkflow struct {
		Name   string                 `json:"name"`
		Config *config.WorkflowConfig `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&newWorkflow); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if newWorkflow.Name == "" || newWorkflow.Config == nil {
		http.Error(w, "Name and config are required", http.StatusBadRequest)
		return
	}

	// Store the workflow configuration
	s.configs[newWorkflow.Name] = newWorkflow.Config

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "created",
		"name":   newWorkflow.Name,
	})
}

// updateWorkflow updates an existing workflow
func (s *UIServer) updateWorkflow(w http.ResponseWriter, r *http.Request, name string) {
	var updateData struct {
		Config *config.WorkflowConfig `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if updateData.Config == nil {
		http.Error(w, "Config is required", http.StatusBadRequest)
		return
	}

	// Store the updated workflow configuration
	s.configs[name] = updateData.Config

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "updated",
		"name":   name,
	})
}

// deleteWorkflow deletes a workflow
func (s *UIServer) deleteWorkflow(w http.ResponseWriter, r *http.Request, name string) {
	// Check if workflow exists in stored configs
	if _, exists := s.configs[name]; !exists {
		http.Error(w, "Workflow not found", http.StatusNotFound)
		return
	}

	// Delete the workflow configuration
	delete(s.configs, name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"name":   name,
	})
}

// ServeHTTP serves HTTP requests (implements http.Handler interface)
func (s *UIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}