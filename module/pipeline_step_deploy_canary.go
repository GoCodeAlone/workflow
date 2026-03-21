package module

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

// CanaryDriver extends DeployDriver with canary-specific traffic routing and
// metric gate evaluation.
type CanaryDriver interface {
	DeployDriver
	// CreateCanary creates a canary instance with the given image.
	CreateCanary(ctx context.Context, image string) error
	// RoutePercent shifts the given percentage of traffic to the canary.
	RoutePercent(ctx context.Context, percent int) error
	// CheckMetricGate evaluates the named metric gate. Returns nil if the gate passes.
	CheckMetricGate(ctx context.Context, gate string) error
	// PromoteCanary replaces the stable instance with the canary.
	PromoteCanary(ctx context.Context) error
	// DestroyCanary removes the canary instance and routes all traffic back to stable.
	DestroyCanary(ctx context.Context) error
}

// CanaryStage describes a single stage in a canary rollout.
type CanaryStage struct {
	Percent    int
	Duration   time.Duration
	MetricGate string
}

// resolveCanaryDriver looks up a CanaryDriver from the service registry.
func resolveCanaryDriver(app modular.Application, serviceName, stepName string) (CanaryDriver, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[serviceName]
	if !ok {
		return nil, fmt.Errorf("step %q: service %q not found in registry", stepName, serviceName)
	}
	driver, ok := svc.(CanaryDriver)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement CanaryDriver (got %T)", stepName, serviceName, svc)
	}
	return driver, nil
}

// ─── step.deploy_canary ───────────────────────────────────────────────────────

// DeployCanaryStep gradually shifts traffic to a new image via configurable
// stages. Each stage routes a percentage of traffic, waits a duration, and
// evaluates an optional metric gate. If any gate fails the canary is destroyed.
type DeployCanaryStep struct {
	name              string
	service           string
	image             string
	stages            []CanaryStage
	rollbackOnFailure bool
	app               modular.Application
}

// NewDeployCanaryStepFactory returns a StepFactory for step.deploy_canary.
func NewDeployCanaryStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("deploy_canary step %q: 'service' is required", name)
		}
		image, _ := cfg["image"].(string)
		if image == "" {
			return nil, fmt.Errorf("deploy_canary step %q: 'image' is required", name)
		}

		var stages []CanaryStage
		if rawStages, ok := cfg["stages"].([]any); ok {
			for i, rs := range rawStages {
				sm, ok := rs.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("deploy_canary step %q: stage %d must be a map", name, i)
				}
				cs := CanaryStage{Percent: 10}
				if pct, ok := sm["percent"].(int); ok {
					cs.Percent = pct
				}
				if dur, ok := sm["duration"].(string); ok {
					if d, err := time.ParseDuration(dur); err == nil {
						cs.Duration = d
					}
				}
				cs.MetricGate, _ = sm["metric_gate"].(string)
				stages = append(stages, cs)
			}
		}
		if len(stages) == 0 {
			// Default single-stage: 100% traffic, no gate.
			stages = []CanaryStage{{Percent: 100, Duration: 0}}
		}

		rollback, _ := cfg["rollback_on_failure"].(bool)

		return &DeployCanaryStep{
			name:              name,
			service:           service,
			image:             image,
			stages:            stages,
			rollbackOnFailure: rollback,
			app:               app,
		}, nil
	}
}

// Name returns the step name.
func (s *DeployCanaryStep) Name() string { return s.name }

// Execute performs a canary deployment through all configured stages.
func (s *DeployCanaryStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	driver, err := resolveCanaryDriver(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}

	if err := driver.CreateCanary(ctx, s.image); err != nil {
		return nil, fmt.Errorf("deploy_canary step %q: create canary: %w", s.name, err)
	}

	stageReached := 0
	for i, stage := range s.stages {
		if err := driver.RoutePercent(ctx, stage.Percent); err != nil {
			if s.rollbackOnFailure {
				_ = driver.DestroyCanary(ctx)
			}
			return nil, fmt.Errorf("deploy_canary step %q: route traffic at stage %d: %w", s.name, i+1, err)
		}

		// Wait for stage duration (best-effort; ctx cancellation respected).
		if stage.Duration > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(stage.Duration):
			}
		}

		// Evaluate metric gate if provided.
		if stage.MetricGate != "" {
			if gErr := driver.CheckMetricGate(ctx, stage.MetricGate); gErr != nil {
				if s.rollbackOnFailure {
					_ = driver.DestroyCanary(ctx)
				}
				return nil, fmt.Errorf("deploy_canary step %q: metric gate %q failed at stage %d: %w", s.name, stage.MetricGate, i+1, gErr)
			}
		}

		stageReached = i + 1
	}

	// All stages passed — promote canary.
	if err := driver.PromoteCanary(ctx); err != nil {
		return nil, fmt.Errorf("deploy_canary step %q: promote canary: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"success":       true,
		"service":       s.service,
		"image":         s.image,
		"stage_reached": stageReached,
		"total_stages":  len(s.stages),
		"promoted":      true,
	}}, nil
}
