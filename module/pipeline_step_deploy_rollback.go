package module

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

// DeployHistoryStore manages a per-service deployment history so that rollback
// can target a specific previous version.
type DeployHistoryStore interface {
	// RecordDeploy records a new deployment entry.
	RecordDeploy(service, image, version string) error
	// GetHistory returns all deployment entries for a service, newest first.
	GetHistory(service string) ([]DeployHistoryEntry, error)
	// GetVersion returns the entry matching the specified version string, or
	// the most recent entry if version is "previous".
	GetVersion(service, version string) (*DeployHistoryEntry, error)
}

// DeployHistoryEntry describes a single deployment event.
type DeployHistoryEntry struct {
	Service    string    `json:"service"`
	Image      string    `json:"image"`
	Version    string    `json:"version"`
	DeployedAt time.Time `json:"deployed_at"`
}

// MemoryDeployHistoryStore is an in-memory DeployHistoryStore used for testing.
type MemoryDeployHistoryStore struct {
	entries map[string][]DeployHistoryEntry
}

// NewMemoryDeployHistoryStore creates a new in-memory history store.
func NewMemoryDeployHistoryStore() *MemoryDeployHistoryStore {
	return &MemoryDeployHistoryStore{entries: make(map[string][]DeployHistoryEntry)}
}

// RecordDeploy appends a deployment entry for the service.
func (s *MemoryDeployHistoryStore) RecordDeploy(service, image, version string) error {
	s.entries[service] = append([]DeployHistoryEntry{
		{Service: service, Image: image, Version: version, DeployedAt: time.Now()},
	}, s.entries[service]...)
	return nil
}

// GetHistory returns all entries for the service, newest first.
func (s *MemoryDeployHistoryStore) GetHistory(service string) ([]DeployHistoryEntry, error) {
	return s.entries[service], nil
}

// GetVersion returns the entry for the requested version, or the most recent
// previous entry when version is "previous".
func (s *MemoryDeployHistoryStore) GetVersion(service, version string) (*DeployHistoryEntry, error) {
	entries := s.entries[service]
	if len(entries) == 0 {
		return nil, fmt.Errorf("no deployment history for service %q", service)
	}
	if version == "previous" {
		if len(entries) < 2 {
			return nil, fmt.Errorf("no previous deployment for service %q", service)
		}
		e := entries[1]
		return &e, nil
	}
	for _, e := range entries {
		if e.Version == version {
			ec := e
			return &ec, nil
		}
	}
	return nil, fmt.Errorf("version %q not found in history for service %q", version, service)
}

// resolveDeployHistoryStore looks up a DeployHistoryStore from the service registry.
func resolveDeployHistoryStore(app modular.Application, storeName, stepName string) (DeployHistoryStore, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[storeName]
	if !ok {
		return nil, fmt.Errorf("step %q: deploy history store %q not found in registry", stepName, storeName)
	}
	store, ok := svc.(DeployHistoryStore)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement DeployHistoryStore (got %T)", stepName, storeName, svc)
	}
	return store, nil
}

// ─── step.deploy_rollback ─────────────────────────────────────────────────────

// DeployRollbackStep retrieves a target deployment version from history and
// re-deploys it via the service's DeployDriver.
type DeployRollbackStep struct {
	name            string
	service         string
	targetVersion   string
	historyStore    string
	healthCheckPath string
	healthTimeout   time.Duration
	app             modular.Application
}

// NewDeployRollbackStepFactory returns a StepFactory for step.deploy_rollback.
func NewDeployRollbackStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("deploy_rollback step %q: 'service' is required", name)
		}

		historyStore, _ := cfg["history_store"].(string)
		if historyStore == "" {
			return nil, fmt.Errorf("deploy_rollback step %q: 'history_store' is required", name)
		}

		targetVersion, _ := cfg["target_version"].(string)
		if targetVersion == "" {
			targetVersion = "previous"
		}

		var healthPath string
		var healthTimeout time.Duration
		if hcRaw, ok := cfg["health_check"].(map[string]any); ok {
			healthPath, _ = hcRaw["path"].(string)
			if to, ok := hcRaw["timeout"].(string); ok {
				if d, err := time.ParseDuration(to); err == nil {
					healthTimeout = d
				}
			}
		}
		if healthTimeout == 0 {
			healthTimeout = 30 * time.Second
		}

		return &DeployRollbackStep{
			name:            name,
			service:         service,
			targetVersion:   targetVersion,
			historyStore:    historyStore,
			healthCheckPath: healthPath,
			healthTimeout:   healthTimeout,
			app:             app,
		}, nil
	}
}

// Name returns the step name.
func (s *DeployRollbackStep) Name() string { return s.name }

// Execute rolls back the service to the target version.
func (s *DeployRollbackStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	driver, err := resolveDeployDriver(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}

	store, err := resolveDeployHistoryStore(s.app, s.historyStore, s.name)
	if err != nil {
		return nil, err
	}

	entry, err := store.GetVersion(s.service, s.targetVersion)
	if err != nil {
		return nil, fmt.Errorf("deploy_rollback step %q: %w", s.name, err)
	}

	if err := driver.Update(ctx, entry.Image); err != nil {
		return nil, fmt.Errorf("deploy_rollback step %q: update to %q: %w", s.name, entry.Image, err)
	}

	// Health check after rollback.
	hcCtx, cancel := context.WithTimeout(ctx, s.healthTimeout)
	hcErr := driver.HealthCheck(hcCtx, s.healthCheckPath)
	cancel()
	if hcErr != nil {
		return nil, fmt.Errorf("deploy_rollback step %q: health check after rollback failed: %w", s.name, hcErr)
	}

	return &StepResult{Output: map[string]any{
		"success":                true,
		"service":                s.service,
		"rolled_back_to":         entry.Version,
		"image":                  entry.Image,
		"originally_deployed_at": entry.DeployedAt.Format(time.RFC3339),
	}}, nil
}
