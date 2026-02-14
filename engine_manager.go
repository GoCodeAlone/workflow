package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// ManagedEngine holds a running workflow engine along with its metadata.
type ManagedEngine struct {
	WorkflowID uuid.UUID
	Engine     *StdEngine
	App        modular.Application
	Status     string // "running", "stopped", "error"
	StartedAt  time.Time
	Error      error
	cancel     context.CancelFunc
}

// GetEngine returns the underlying engine, satisfying the module.triggerableEngine
// interface so the CrossWorkflowRouter can trigger workflows via duck-typing.
func (me *ManagedEngine) GetEngine() module.TriggerWorkflower {
	return me.Engine
}

// WorkflowStatus describes the current runtime state of a managed workflow.
type WorkflowStatus struct {
	WorkflowID  uuid.UUID     `json:"workflow_id"`
	Status      string        `json:"status"`
	StartedAt   time.Time     `json:"started_at"`
	Uptime      time.Duration `json:"uptime"`
	Error       string        `json:"error,omitempty"`
	ModuleCount int           `json:"module_count"`
}

// EngineBuilderFunc is called by the manager to create and configure an engine
// from a parsed workflow config. The caller is responsible for registering
// workflow handlers, dynamic components, and other setup. The function must
// call BuildFromConfig on the engine before returning.
type EngineBuilderFunc func(cfg *config.WorkflowConfig, logger *slog.Logger) (*StdEngine, modular.Application, error)

// WorkflowEngineManager manages multiple concurrent workflow engine instances.
type WorkflowEngineManager struct {
	mu            sync.RWMutex
	engines       map[uuid.UUID]*ManagedEngine
	store         store.WorkflowStore
	linkStore     store.CrossWorkflowLinkStore
	router        *module.CrossWorkflowRouter
	logger        *slog.Logger
	engineBuilder EngineBuilderFunc
}

// NewWorkflowEngineManager creates a new manager for workflow engine instances.
// The engineBuilder function is called to create each new engine instance,
// allowing the caller to register handlers and configure the dynamic system.
func NewWorkflowEngineManager(wfStore store.WorkflowStore, linkStore store.CrossWorkflowLinkStore, logger *slog.Logger, engineBuilder EngineBuilderFunc) *WorkflowEngineManager {
	m := &WorkflowEngineManager{
		engines:       make(map[uuid.UUID]*ManagedEngine),
		store:         wfStore,
		linkStore:     linkStore,
		logger:        logger,
		engineBuilder: engineBuilder,
	}

	m.router = module.NewCrossWorkflowRouter(linkStore, func(id uuid.UUID) (any, bool) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		me, ok := m.engines[id]
		return me, ok
	}, logger)
	return m
}

// Router returns the cross-workflow event router.
func (m *WorkflowEngineManager) Router() *module.CrossWorkflowRouter {
	return m.router
}

// DeployWorkflow loads config from the store, creates an isolated engine, and starts it.
func (m *WorkflowEngineManager) DeployWorkflow(ctx context.Context, workflowID uuid.UUID) error {
	// Check if already running
	m.mu.RLock()
	if me, exists := m.engines[workflowID]; exists && me.Status == "running" {
		m.mu.RUnlock()
		return fmt.Errorf("workflow %s is already running", workflowID)
	}
	m.mu.RUnlock()

	// Load workflow record from store
	record, err := m.store.Get(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to load workflow %s: %w", workflowID, err)
	}

	// Parse config YAML
	cfg, err := config.LoadFromString(record.ConfigYAML)
	if err != nil {
		m.updateWorkflowStatus(ctx, workflowID, store.WorkflowStatusError)
		return fmt.Errorf("failed to parse config for workflow %s: %w", workflowID, err)
	}

	// Namespace module names with workflow ID to ensure isolation
	ns := module.NewModuleNamespace(workflowID.String(), "")
	for i := range cfg.Modules {
		cfg.Modules[i].Name = ns.FormatName(cfg.Modules[i].Name)
		for j := range cfg.Modules[i].DependsOn {
			cfg.Modules[i].DependsOn[j] = ns.ResolveDependency(cfg.Modules[i].DependsOn[j])
		}
	}

	// Build engine using the provided builder function
	engine, app, err := m.engineBuilder(cfg, m.logger)
	if err != nil {
		m.updateWorkflowStatus(ctx, workflowID, store.WorkflowStatusError)
		return fmt.Errorf("failed to build workflow %s: %w", workflowID, err)
	}

	// Create cancellable context for this engine
	engineCtx, cancel := context.WithCancel(ctx)

	// Start the engine
	if err := engine.Start(engineCtx); err != nil {
		cancel()
		m.updateWorkflowStatus(ctx, workflowID, store.WorkflowStatusError)
		return fmt.Errorf("failed to start workflow %s: %w", workflowID, err)
	}

	// Store managed engine
	me := &ManagedEngine{
		WorkflowID: workflowID,
		Engine:     engine,
		App:        app,
		Status:     "running",
		StartedAt:  time.Now(),
		cancel:     cancel,
	}

	m.mu.Lock()
	m.engines[workflowID] = me
	m.mu.Unlock()

	// Update workflow status in DB
	m.updateWorkflowStatus(ctx, workflowID, store.WorkflowStatusActive)

	m.logger.Info("Deployed workflow", "workflow_id", workflowID)

	// Refresh cross-workflow links
	if err := m.router.RefreshLinks(ctx); err != nil {
		m.logger.Warn("Failed to refresh cross-workflow links", "error", err)
	}

	return nil
}

// StopWorkflow gracefully stops a running engine.
func (m *WorkflowEngineManager) StopWorkflow(ctx context.Context, workflowID uuid.UUID) error {
	m.mu.Lock()
	me, exists := m.engines[workflowID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("workflow %s is not running", workflowID)
	}
	delete(m.engines, workflowID)
	m.mu.Unlock()

	// Cancel the engine context
	if me.cancel != nil {
		me.cancel()
	}

	// Stop the engine
	if err := me.Engine.Stop(ctx); err != nil {
		m.logger.Error("Error stopping workflow", "workflow_id", workflowID, "error", err)
	}

	me.Status = "stopped"

	// Update workflow status in DB
	m.updateWorkflowStatus(ctx, workflowID, store.WorkflowStatusStopped)

	m.logger.Info("Stopped workflow", "workflow_id", workflowID)
	return nil
}

// ReloadWorkflow stops and redeploys a workflow.
func (m *WorkflowEngineManager) ReloadWorkflow(ctx context.Context, workflowID uuid.UUID) error {
	// Stop if running (ignore error if not running)
	m.mu.RLock()
	_, running := m.engines[workflowID]
	m.mu.RUnlock()

	if running {
		if err := m.StopWorkflow(ctx, workflowID); err != nil {
			m.logger.Warn("Error stopping workflow during reload", "workflow_id", workflowID, "error", err)
		}
	}

	return m.DeployWorkflow(ctx, workflowID)
}

// GetStatus returns the runtime status of a workflow.
func (m *WorkflowEngineManager) GetStatus(workflowID uuid.UUID) (*WorkflowStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	me, exists := m.engines[workflowID]
	if !exists {
		return nil, fmt.Errorf("workflow %s is not running", workflowID)
	}

	status := &WorkflowStatus{
		WorkflowID:  me.WorkflowID,
		Status:      me.Status,
		StartedAt:   me.StartedAt,
		Uptime:      time.Since(me.StartedAt),
		ModuleCount: len(me.App.SvcRegistry()),
	}

	if me.Error != nil {
		status.Error = me.Error.Error()
	}

	return status, nil
}

// ListActive returns the status of all running workflows.
func (m *WorkflowEngineManager) ListActive() []WorkflowStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]WorkflowStatus, 0, len(m.engines))
	for _, me := range m.engines {
		s := WorkflowStatus{
			WorkflowID:  me.WorkflowID,
			Status:      me.Status,
			StartedAt:   me.StartedAt,
			Uptime:      time.Since(me.StartedAt),
			ModuleCount: len(me.App.SvcRegistry()),
		}
		if me.Error != nil {
			s.Error = me.Error.Error()
		}
		statuses = append(statuses, s)
	}

	return statuses
}

// StopAll gracefully stops all running engines.
func (m *WorkflowEngineManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	ids := make([]uuid.UUID, 0, len(m.engines))
	for id := range m.engines {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var lastErr error
	for _, id := range ids {
		if err := m.StopWorkflow(ctx, id); err != nil {
			lastErr = err
			m.logger.Error("Error stopping workflow during shutdown", "workflow_id", id, "error", err)
		}
	}

	return lastErr
}

// updateWorkflowStatus updates the workflow record status in the store.
func (m *WorkflowEngineManager) updateWorkflowStatus(ctx context.Context, workflowID uuid.UUID, status store.WorkflowStatus) {
	record, err := m.store.Get(ctx, workflowID)
	if err != nil {
		m.logger.Error("Failed to load workflow for status update", "workflow_id", workflowID, "error", err)
		return
	}
	record.Status = status
	if err := m.store.Update(ctx, record); err != nil {
		m.logger.Error("Failed to update workflow status", "workflow_id", workflowID, "error", err)
	}
}
