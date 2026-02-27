package config

import (
	"context"
	"log/slog"
	"sync"
)

// ModuleReconfigurer is implemented by the engine to support partial (per-module) reloads.
// When a config change only affects module configs, the engine can apply changes surgically
// rather than performing a full stop/rebuild/start cycle.
type ModuleReconfigurer interface {
	// ReconfigureModules applies new configuration to specific running modules.
	// Returns the names of any modules that could not be reconfigured in-place
	// (requiring a full reload) and any hard error.
	ReconfigureModules(ctx context.Context, changes []ModuleConfigChange) (failedModules []string, err error)
}

// ConfigReloader coordinates config change detection and engine reload decisions.
// It diffs old and new configs, performs partial per-module reconfiguration when
// possible, and falls back to a full reload when non-module sections change or
// modules are added/removed/non-reconfigurable.
type ConfigReloader struct {
	mu          sync.Mutex
	current     *WorkflowConfig
	currentHash string
	logger      *slog.Logger

	fullReloadFn func(*WorkflowConfig) error
	reconfigurer ModuleReconfigurer
}

// NewConfigReloader creates a ConfigReloader with the given initial config.
// fullReloadFn is called when a full engine restart is required.
// reconfigurer is optional; if nil, all module changes fall back to fullReloadFn.
func NewConfigReloader(
	initial *WorkflowConfig,
	fullReloadFn func(*WorkflowConfig) error,
	reconfigurer ModuleReconfigurer,
	logger *slog.Logger,
) (*ConfigReloader, error) {
	hash, err := HashConfig(initial)
	if err != nil {
		return nil, err
	}
	return &ConfigReloader{
		current:      initial,
		currentHash:  hash,
		logger:       logger,
		fullReloadFn: fullReloadFn,
		reconfigurer: reconfigurer,
	}, nil
}

// SetReconfigurer updates the ModuleReconfigurer used for partial (per-module)
// reloads. This should be called after a successful full engine reload if the
// underlying engine (and its reconfigurer) has changed.
func (r *ConfigReloader) SetReconfigurer(reconfigurer ModuleReconfigurer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconfigurer = reconfigurer
}

// HandleChange processes a config change event. It diffs the old and new configs,
// attempts per-module reconfiguration for module-only changes, and falls back
// to a full reload when necessary.
func (r *ConfigReloader) HandleChange(evt ConfigChangeEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	diff := DiffModuleConfigs(r.current, evt.Config)

	// Non-module sections changed, or modules were added/removed — full reload required.
	if HasNonModuleChanges(r.current, evt.Config) ||
		len(diff.Added) > 0 || len(diff.Removed) > 0 {
		r.logger.Info("non-module changes detected, performing full reload",
			"added", len(diff.Added), "removed", len(diff.Removed))
		if err := r.fullReloadFn(evt.Config); err != nil {
			return err
		}
		r.current = evt.Config
		r.currentHash = evt.NewHash
		return nil
	}

	// Only module config changes — try partial reconfiguration.
	if len(diff.Modified) > 0 {
		if r.reconfigurer == nil {
			// No reconfigurer available — fall back to full reload.
			r.logger.Info("module changes detected but no reconfigurer, performing full reload",
				"modified", len(diff.Modified))
			if err := r.fullReloadFn(evt.Config); err != nil {
				return err
			}
			r.current = evt.Config
			r.currentHash = evt.NewHash
			return nil
		}

		failed, err := r.reconfigurer.ReconfigureModules(context.Background(), diff.Modified)
		if err != nil {
			return err
		}
		if len(failed) > 0 {
			r.logger.Info("some modules cannot be reconfigured in-place, performing full reload",
				"modules", failed)
			if err := r.fullReloadFn(evt.Config); err != nil {
				return err
			}
		}
		r.current = evt.Config
		r.currentHash = evt.NewHash
		return nil
	}

	r.logger.Debug("config change detected but no effective differences")
	return nil
}
