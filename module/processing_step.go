package module

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/CrisisTextLine/modular"
)

// Executor is the interface that dynamic components satisfy.
type Executor interface {
	Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error)
}

// ProcessingStepConfig holds configuration for a processing step module.
type ProcessingStepConfig struct {
	ComponentID          string // service name to look up in registry
	SuccessTransition    string // transition to fire on success
	CompensateTransition string // transition to fire on permanent failure
	MaxRetries           int    // default 2
	RetryBackoffMs       int    // base backoff in ms, default 1000
	TimeoutSeconds       int    // per-attempt timeout, default 30
}

// ProcessingStep bridges dynamic components to state machine transitions.
// It implements TransitionHandler, wrapping an Executor with retry and
// compensation logic.
type ProcessingStep struct {
	name     string
	config   ProcessingStepConfig
	executor Executor
	smEngine *StateMachineEngine
	metrics  *MetricsCollector
}

// NewProcessingStep creates a new ProcessingStep module.
func NewProcessingStep(name string, config ProcessingStepConfig) *ProcessingStep {
	if config.MaxRetries < 0 {
		config.MaxRetries = 2
	}
	if config.RetryBackoffMs <= 0 {
		config.RetryBackoffMs = 1000
	}
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 30
	}
	return &ProcessingStep{
		name:   name,
		config: config,
	}
}

// Name returns the module name.
func (ps *ProcessingStep) Name() string {
	return ps.name
}

// Init resolves dependencies from the service registry.
// Note: service registration is handled by ProvidesServices() — the framework
// calls it after Init completes, so we don't register here.
func (ps *ProcessingStep) Init(app modular.Application) error {
	// Resolve the executor (dynamic component) from the service registry
	if ps.config.ComponentID != "" {
		var executor Executor
		if err := app.GetService(ps.config.ComponentID, &executor); err != nil {
			return fmt.Errorf("processing step %q: resolve executor %q: %w", ps.name, ps.config.ComponentID, err)
		}
		ps.executor = executor
	}

	// Resolve state machine engine (optional, for firing transitions).
	// Try by standard name first, then scan the registry for any engine.
	var smEngine *StateMachineEngine
	if err := app.GetService(StateMachineEngineName, &smEngine); err == nil && smEngine != nil {
		ps.smEngine = smEngine
	} else {
		for _, svc := range app.SvcRegistry() {
			if engine, ok := svc.(*StateMachineEngine); ok {
				ps.smEngine = engine
				break
			}
		}
	}

	// Resolve metrics collector (optional)
	var metrics *MetricsCollector
	if err := app.GetService("metrics.collector", &metrics); err == nil && metrics != nil {
		ps.metrics = metrics
	}

	return nil
}

// Start is a no-op for the processing step.
func (ps *ProcessingStep) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for the processing step.
func (ps *ProcessingStep) Stop(_ context.Context) error {
	return nil
}

// ProvidesServices returns the service provided by this module.
func (ps *ProcessingStep) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        ps.name,
			Description: "Processing step: " + ps.name,
			Instance:    ps,
		},
	}
}

// RequiresServices returns services required by this module.
func (ps *ProcessingStep) RequiresServices() []modular.ServiceDependency {
	deps := []modular.ServiceDependency{
		{
			Name:     StateMachineEngineName,
			Required: false,
		},
		{
			Name:     "metrics.collector",
			Required: false,
		},
	}
	if ps.config.ComponentID != "" {
		deps = append(deps, modular.ServiceDependency{
			Name:     ps.config.ComponentID,
			Required: true,
		})
	}
	return deps
}

// HandleTransition implements the TransitionHandler interface. It executes
// the wrapped dynamic component with retry and exponential backoff.
func (ps *ProcessingStep) HandleTransition(ctx context.Context, event TransitionEvent) error {
	if ps.executor == nil {
		return fmt.Errorf("processing step %q: no executor configured", ps.name)
	}

	// Build params from event data
	params := make(map[string]interface{})
	for k, v := range event.Data {
		params[k] = v
	}
	params["workflowId"] = event.WorkflowID
	params["transitionId"] = event.TransitionID
	params["fromState"] = event.FromState
	params["toState"] = event.ToState

	startTime := time.Now()
	var lastErr error

	for attempt := 0; attempt <= ps.config.MaxRetries; attempt++ {
		// Wait with exponential backoff (skip on first attempt)
		if attempt > 0 {
			backoff := ps.calculateBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Create per-attempt timeout context
		attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(ps.config.TimeoutSeconds)*time.Second)
		result, err := ps.executor.Execute(attemptCtx, params)
		cancel()

		if err == nil {
			// Executor succeeded (no Go error). Record metrics and fire success transition.
			ps.recordMetrics("success", time.Since(startTime))
			ps.fireTransition(ctx, event.WorkflowID, ps.config.SuccessTransition, result)
			return nil
		}

		lastErr = err
	}

	// All retries exhausted — permanent failure
	ps.recordMetrics("failure", time.Since(startTime))
	ps.fireTransition(ctx, event.WorkflowID, ps.config.CompensateTransition, map[string]interface{}{
		"error": lastErr.Error(),
	})

	return fmt.Errorf("processing step %q: retries exhausted: %w", ps.name, lastErr)
}

// calculateBackoff returns the backoff duration for the given attempt (1-based).
func (ps *ProcessingStep) calculateBackoff(attempt int) time.Duration {
	base := float64(ps.config.RetryBackoffMs) * math.Pow(2, float64(attempt-1))
	return time.Duration(base) * time.Millisecond
}

// fireTransition triggers a state machine transition to avoid deadlocking
// when called from inside a transition handler. Uses the engine's tracked
// goroutine so shutdown can drain in-flight work.
//
// Note: Handlers are called BEFORE TriggerTransition commits the state change.
// The goroutine must wait briefly so the parent transition commits first;
// otherwise it may see stale state and silently fail.
func (ps *ProcessingStep) fireTransition(_ context.Context, workflowID, transition string, data map[string]interface{}) {
	if transition == "" || ps.smEngine == nil {
		return
	}
	ps.smEngine.TrackGoroutine(func() {
		// Brief pause to let the parent TriggerTransition commit the state
		// change. Without this, the goroutine can race and find the instance
		// still in its pre-transition state.
		time.Sleep(10 * time.Millisecond)
		// Use context.Background() because the spawned goroutine outlives
		// the caller (e.g., an HTTP request handler whose context is
		// cancelled after the response is written).
		_ = ps.smEngine.TriggerTransition(context.Background(), workflowID, transition, data)
	})
}

// recordMetrics records processing step metrics if a collector is available.
func (ps *ProcessingStep) recordMetrics(status string, duration time.Duration) {
	if ps.metrics == nil {
		return
	}
	ps.metrics.RecordModuleOperation(ps.name, "execute", status)
	ps.metrics.RecordWorkflowDuration(ps.name, "processing_step", duration)
}
