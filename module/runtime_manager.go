package module

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/google/uuid"
)

// RuntimeInstance represents a running workflow loaded from the filesystem.
type RuntimeInstance struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ConfigPath string         `json:"config_path"`
	WorkDir    string         `json:"work_dir"`
	Status     string         `json:"status"` // "running", "stopped", "error"
	StartedAt  time.Time      `json:"started_at"`
	Error      string         `json:"error,omitempty"`
	Ports      map[string]int `json:"ports,omitempty"`
	Config     *config.WorkflowConfig

	cancel context.CancelFunc
}

// RuntimeEngineBuilder creates and starts an engine from a workflow config.
// It returns a stop function that should be called to shut down the engine.
type RuntimeEngineBuilder func(cfg *config.WorkflowConfig, logger *slog.Logger) (stopFunc func(context.Context) error, err error)

// RuntimeManager manages workflow instances loaded from the filesystem.
// It is used with the --load-workflows CLI flag to run example workflows
// alongside the admin server.
type RuntimeManager struct {
	mu            sync.RWMutex
	instances     map[string]*RuntimeInstance
	stopFuncs     map[string]func(context.Context) error
	store         *V1Store
	builder       RuntimeEngineBuilder
	logger        *slog.Logger
	portAllocator *PortAllocator
}

// NewRuntimeManager creates a new runtime manager.
func NewRuntimeManager(store *V1Store, builder RuntimeEngineBuilder, logger *slog.Logger) *RuntimeManager {
	return &RuntimeManager{
		instances: make(map[string]*RuntimeInstance),
		stopFuncs: make(map[string]func(context.Context) error),
		store:     store,
		builder:   builder,
		logger:    logger,
	}
}

// SetPortAllocator configures the port allocator for automatic port assignment.
func (rm *RuntimeManager) SetPortAllocator(pa *PortAllocator) {
	rm.portAllocator = pa
}

// AnnounceServices logs the ports assigned to a workflow instance.
func (rm *RuntimeManager) AnnounceServices(instance *RuntimeInstance) {
	if instance.Ports == nil || len(instance.Ports) == 0 {
		return
	}
	rm.logger.Info(fmt.Sprintf("Workflow %q started:", instance.Name))
	for modName, port := range instance.Ports {
		rm.logger.Info(fmt.Sprintf("  - %s: http://localhost:%d", modName, port))
	}
}

// rewritePorts scans modules for http.server types and assigns auto-allocated ports.
func rewritePorts(cfg *config.WorkflowConfig, allocator *PortAllocator, name string) map[string]int {
	if allocator == nil {
		return nil
	}
	ports := make(map[string]int)
	for i, mod := range cfg.Modules {
		if mod.Type == "http.server" {
			port, err := allocator.Allocate(name)
			if err != nil {
				continue
			}
			if cfg.Modules[i].Config == nil {
				cfg.Modules[i].Config = make(map[string]any)
			}
			cfg.Modules[i].Config["address"] = fmt.Sprintf(":%d", port)
			ports[mod.Name] = port
		}
	}
	return ports
}

// LoadFromPaths loads workflows from comma-separated paths.
// Each path can be a YAML file or a directory containing workflow.yaml.
func (rm *RuntimeManager) LoadFromPaths(ctx context.Context, paths []string) error {
	for _, p := range paths {
		p = filepath.Clean(p)
		info, err := os.Stat(p)
		if err != nil {
			rm.logger.Warn("Skipping invalid path", "path", p, "error", err)
			continue
		}

		if info.IsDir() {
			// Look for workflow.yaml in the directory
			yamlPath := filepath.Join(p, "workflow.yaml")
			if _, err := os.Stat(yamlPath); err != nil {
				rm.logger.Warn("No workflow.yaml found in directory", "path", p)
				continue
			}
			if err := rm.loadWorkflow(ctx, yamlPath, p); err != nil {
				rm.logger.Error("Failed to load workflow", "path", yamlPath, "error", err)
			}
		} else {
			// Direct YAML file
			dir := filepath.Dir(p)
			if err := rm.loadWorkflow(ctx, p, dir); err != nil {
				rm.logger.Error("Failed to load workflow", "path", p, "error", err)
			}
		}
	}
	return nil
}

// loadWorkflow loads and starts a single workflow from a config file.
func (rm *RuntimeManager) loadWorkflow(ctx context.Context, configPath, workDir string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	// Derive a name from the directory
	name := filepath.Base(workDir)
	if name == "." || name == "/" {
		name = filepath.Base(configPath)
	}

	id := uuid.New().String()

	instance := &RuntimeInstance{
		ID:         id,
		Name:       name,
		ConfigPath: configPath,
		WorkDir:    workDir,
		Status:     "starting",
		StartedAt:  time.Now(),
		Config:     cfg,
	}

	rm.mu.Lock()
	rm.instances[id] = instance
	rm.mu.Unlock()

	// Register with V1Store so the admin UI can see it
	rm.registerInStore(instance)

	// Load dynamic components from the workflow's components/ directory
	componentsDir := filepath.Join(workDir, "components")
	if info, err := os.Stat(componentsDir); err == nil && info.IsDir() {
		rm.logger.Info("Found components directory", "path", componentsDir, "workflow", name)
		// Components will be loaded by the engine builder if it supports it
	}

	// Create a cancellable context for this workflow
	engineCtx, cancel := context.WithCancel(ctx)
	instance.cancel = cancel

	// Build and start the engine
	stopFunc, buildErr := rm.builder(cfg, rm.logger)
	if buildErr != nil {
		cancel()
		instance.Status = "error"
		instance.Error = buildErr.Error()
		rm.logger.Error("Failed to build workflow engine", "workflow", name, "error", buildErr)
		return buildErr
	}

	rm.mu.Lock()
	rm.stopFuncs[id] = stopFunc
	instance.Status = "running"
	rm.mu.Unlock()

	rm.logger.Info("Workflow loaded and running",
		"workflow", name,
		"id", id,
		"config", configPath,
	)

	// Watch for context cancellation to clean up
	go func() {
		<-engineCtx.Done()
		rm.mu.Lock()
		if inst, ok := rm.instances[id]; ok {
			inst.Status = "stopped"
		}
		rm.mu.Unlock()
	}()

	return nil
}

// registerInStore creates a workflow record in the V1Store so the admin UI sees it.
func (rm *RuntimeManager) registerInStore(inst *RuntimeInstance) {
	if rm.store == nil {
		return
	}

	// Find or create a project for runtime workflows
	projectID := rm.ensureRuntimeProject()
	if projectID == "" {
		return
	}

	// Read the config YAML for storage
	configData, err := os.ReadFile(inst.ConfigPath)
	if err != nil {
		rm.logger.Warn("Failed to read config file for store registration", "error", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, dbErr := rm.store.db.Exec(
		`INSERT OR IGNORE INTO workflows (id, project_id, name, slug, description, config_yaml, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID, projectID, inst.Name, inst.Name,
		fmt.Sprintf("Loaded from %s", inst.ConfigPath),
		string(configData), "active", now, now,
	)
	if dbErr != nil {
		rm.logger.Warn("Failed to register workflow in store", "workflow", inst.Name, "error", dbErr)
	}
}

// ensureRuntimeProject creates the "Runtime Workflows" project if it doesn't exist.
func (rm *RuntimeManager) ensureRuntimeProject() string {
	if rm.store == nil {
		return ""
	}

	const runtimeProjectID = "runtime-workflows"
	const runtimeCompanyID = "runtime-company"

	now := time.Now().UTC().Format(time.RFC3339)

	// Ensure company
	_, _ = rm.store.db.Exec(
		`INSERT OR IGNORE INTO companies (id, name, slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		runtimeCompanyID, "Runtime", "runtime", now, now,
	)

	// Ensure project
	_, _ = rm.store.db.Exec(
		`INSERT OR IGNORE INTO projects (id, company_id, name, slug, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		runtimeProjectID, runtimeCompanyID, "Runtime Workflows", "runtime-workflows",
		"Workflows loaded via --load-workflows", now, now,
	)

	return runtimeProjectID
}

// LaunchFromYAML creates and starts a workflow engine from a YAML config string.
// The id parameter links this instance to its workflow record in the store.
func (rm *RuntimeManager) LaunchFromYAML(ctx context.Context, id, name, yamlContent string) error {
	return rm.LaunchFromWorkspace(ctx, id, name, yamlContent, "")
}

// LaunchFromWorkspace creates and starts a workflow engine from a YAML config string,
// optionally setting the workspace directory for relative path resolution.
func (rm *RuntimeManager) LaunchFromWorkspace(ctx context.Context, id, name, yamlContent, workspaceDir string) error {
	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if workspaceDir != "" {
		cfg.ConfigDir = workspaceDir
	}

	ports := rewritePorts(cfg, rm.portAllocator, name)

	instance := &RuntimeInstance{
		ID:        id,
		Name:      name,
		WorkDir:   workspaceDir,
		Status:    "starting",
		StartedAt: time.Now(),
		Config:    cfg,
		Ports:     ports,
	}

	rm.mu.Lock()
	if existing, ok := rm.instances[id]; ok && existing.Status == "running" {
		rm.mu.Unlock()
		return fmt.Errorf("workflow %s is already running", id)
	}
	rm.instances[id] = instance
	rm.mu.Unlock()

	// Use a background context for the engine lifecycle — the caller's context
	// (typically an HTTP request) should not cancel the long-running engine.
	engineCtx, cancel := context.WithCancel(context.Background())
	instance.cancel = cancel

	stopFunc, buildErr := rm.builder(cfg, rm.logger)
	if buildErr != nil {
		cancel()
		rm.mu.Lock()
		instance.Status = "error"
		instance.Error = buildErr.Error()
		rm.mu.Unlock()
		return buildErr
	}

	rm.mu.Lock()
	rm.stopFuncs[id] = stopFunc
	instance.Status = "running"
	rm.mu.Unlock()

	rm.logger.Info("Workflow launched from YAML",
		"workflow", name,
		"id", id,
		"workspace_dir", workspaceDir,
	)

	rm.AnnounceServices(instance)

	go func() {
		<-engineCtx.Done()
		rm.mu.Lock()
		if inst, ok := rm.instances[id]; ok && inst.Status == "running" {
			inst.Status = "stopped"
		}
		rm.mu.Unlock()
	}()

	return nil
}

// LaunchFromPath loads a workflow config from a server-local path and starts it.
// The path can be a YAML file or a directory containing workflow.yaml.
func (rm *RuntimeManager) LaunchFromPath(ctx context.Context, path string) (*RuntimeInstance, error) {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %s: %w", path, err)
	}

	var configPath, workDir string
	if info.IsDir() {
		yamlPath := filepath.Join(path, "workflow.yaml")
		if _, err := os.Stat(yamlPath); err != nil {
			return nil, fmt.Errorf("no workflow.yaml found in directory %s", path)
		}
		configPath = yamlPath
		workDir = path
	} else {
		configPath = path
		workDir = filepath.Dir(path)
	}

	if err := rm.loadWorkflow(ctx, configPath, workDir); err != nil {
		return nil, err
	}

	// Find the instance that was just created (most recent by start time)
	rm.mu.RLock()
	var latest *RuntimeInstance
	for _, inst := range rm.instances {
		if inst.ConfigPath == configPath {
			if latest == nil || inst.StartedAt.After(latest.StartedAt) {
				latest = inst
			}
		}
	}
	rm.mu.RUnlock()

	if latest == nil {
		return nil, fmt.Errorf("workflow loaded but instance not found")
	}

	copy := *latest
	copy.Config = nil
	return &copy, nil
}

// StopWorkflow stops a specific running workflow.
func (rm *RuntimeManager) StopWorkflow(ctx context.Context, id string) error {
	rm.mu.Lock()
	inst, ok := rm.instances[id]
	stopFunc := rm.stopFuncs[id]
	rm.mu.Unlock()

	if !ok {
		return fmt.Errorf("workflow instance %s not found", id)
	}

	if inst.cancel != nil {
		inst.cancel()
	}

	if stopFunc != nil {
		if err := stopFunc(ctx); err != nil {
			rm.logger.Error("Error stopping workflow", "id", id, "name", inst.Name, "error", err)
			return err
		}
	}

	rm.mu.Lock()
	inst.Status = "stopped"
	delete(rm.stopFuncs, id)
	rm.mu.Unlock()

	if rm.portAllocator != nil {
		rm.portAllocator.Release(inst.Name)
	}

	rm.logger.Info("Stopped workflow", "id", id, "name", inst.Name)
	return nil
}

// StopAll stops all running workflow instances.
func (rm *RuntimeManager) StopAll(ctx context.Context) error {
	rm.mu.RLock()
	ids := make([]string, 0, len(rm.instances))
	for id, inst := range rm.instances {
		if inst.Status == "running" {
			ids = append(ids, id)
		}
	}
	rm.mu.RUnlock()

	var lastErr error
	for _, id := range ids {
		if err := rm.StopWorkflow(ctx, id); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ListInstances returns all workflow instances.
func (rm *RuntimeManager) ListInstances() []RuntimeInstance {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make([]RuntimeInstance, 0, len(rm.instances))
	for _, inst := range rm.instances {
		result = append(result, RuntimeInstance{
			ID:         inst.ID,
			Name:       inst.Name,
			ConfigPath: inst.ConfigPath,
			WorkDir:    inst.WorkDir,
			Status:     inst.Status,
			StartedAt:  inst.StartedAt,
			Error:      inst.Error,
			Ports:      inst.Ports,
		})
	}
	return result
}

// GetInstance returns a specific workflow instance by ID.
func (rm *RuntimeManager) GetInstance(id string) (*RuntimeInstance, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	inst, ok := rm.instances[id]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *inst
	copy.Config = nil // Don't expose full config in API responses
	return &copy, true
}
