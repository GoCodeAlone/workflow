package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── scaling_plan ─────────────────────────────────────────────────────────────

// ScalingPlanStep calls Plan() on a named platform.autoscaling module.
type ScalingPlanStep struct {
	name    string
	scaling string
	app     modular.Application
}

// NewScalingPlanStepFactory returns a StepFactory for step.scaling_plan.
func NewScalingPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		scaling, _ := cfg["scaling"].(string)
		if scaling == "" {
			return nil, fmt.Errorf("scaling_plan step %q: 'scaling' is required", name)
		}
		return &ScalingPlanStep{name: name, scaling: scaling, app: app}, nil
	}
}

func (s *ScalingPlanStep) Name() string { return s.name }

func (s *ScalingPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	as, err := resolveAutoscalingModule(s.app, s.scaling, s.name)
	if err != nil {
		return nil, err
	}
	plan, err := as.Plan()
	if err != nil {
		return nil, fmt.Errorf("scaling_plan step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plan":     plan,
		"scaling":  s.scaling,
		"policies": plan.Policies,
		"changes":  plan.Changes,
	}}, nil
}

// ─── scaling_apply ────────────────────────────────────────────────────────────

// ScalingApplyStep calls Apply() on a named platform.autoscaling module.
type ScalingApplyStep struct {
	name    string
	scaling string
	app     modular.Application
}

// NewScalingApplyStepFactory returns a StepFactory for step.scaling_apply.
func NewScalingApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		scaling, _ := cfg["scaling"].(string)
		if scaling == "" {
			return nil, fmt.Errorf("scaling_apply step %q: 'scaling' is required", name)
		}
		return &ScalingApplyStep{name: name, scaling: scaling, app: app}, nil
	}
}

func (s *ScalingApplyStep) Name() string { return s.name }

func (s *ScalingApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	as, err := resolveAutoscalingModule(s.app, s.scaling, s.name)
	if err != nil {
		return nil, err
	}
	state, err := as.Apply()
	if err != nil {
		return nil, fmt.Errorf("scaling_apply step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"state":           state,
		"scaling":         s.scaling,
		"id":              state.ID,
		"currentCapacity": state.CurrentCapacity,
		"status":          state.Status,
	}}, nil
}

// ─── scaling_status ───────────────────────────────────────────────────────────

// ScalingStatusStep calls Status() on a named platform.autoscaling module.
type ScalingStatusStep struct {
	name    string
	scaling string
	app     modular.Application
}

// NewScalingStatusStepFactory returns a StepFactory for step.scaling_status.
func NewScalingStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		scaling, _ := cfg["scaling"].(string)
		if scaling == "" {
			return nil, fmt.Errorf("scaling_status step %q: 'scaling' is required", name)
		}
		return &ScalingStatusStep{name: name, scaling: scaling, app: app}, nil
	}
}

func (s *ScalingStatusStep) Name() string { return s.name }

func (s *ScalingStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	as, err := resolveAutoscalingModule(s.app, s.scaling, s.name)
	if err != nil {
		return nil, err
	}
	st, err := as.Status()
	if err != nil {
		return nil, fmt.Errorf("scaling_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"status":  st,
		"scaling": s.scaling,
	}}, nil
}

// ─── scaling_destroy ──────────────────────────────────────────────────────────

// ScalingDestroyStep calls Destroy() on a named platform.autoscaling module.
type ScalingDestroyStep struct {
	name    string
	scaling string
	app     modular.Application
}

// NewScalingDestroyStepFactory returns a StepFactory for step.scaling_destroy.
func NewScalingDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		scaling, _ := cfg["scaling"].(string)
		if scaling == "" {
			return nil, fmt.Errorf("scaling_destroy step %q: 'scaling' is required", name)
		}
		return &ScalingDestroyStep{name: name, scaling: scaling, app: app}, nil
	}
}

func (s *ScalingDestroyStep) Name() string { return s.name }

func (s *ScalingDestroyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	as, err := resolveAutoscalingModule(s.app, s.scaling, s.name)
	if err != nil {
		return nil, err
	}
	if err := as.Destroy(); err != nil {
		return nil, fmt.Errorf("scaling_destroy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"destroyed": true,
		"scaling":   s.scaling,
	}}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func resolveAutoscalingModule(app modular.Application, scaling, stepName string) (*PlatformAutoscaling, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[scaling]
	if !ok {
		return nil, fmt.Errorf("step %q: scaling service %q not found in registry", stepName, scaling)
	}
	as, ok := svc.(*PlatformAutoscaling)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *PlatformAutoscaling (got %T)", stepName, scaling, svc)
	}
	return as, nil
}
