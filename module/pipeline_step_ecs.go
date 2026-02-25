package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── ecs_plan ─────────────────────────────────────────────────────────────────

// ECSPlanStep calls Plan() on a named platform.ecs module.
type ECSPlanStep struct {
	name    string
	service string
	app     modular.Application
}

// NewECSPlanStepFactory returns a StepFactory for step.ecs_plan.
func NewECSPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("ecs_plan step %q: 'service' is required", name)
		}
		return &ECSPlanStep{name: name, service: service, app: app}, nil
	}
}

func (s *ECSPlanStep) Name() string { return s.name }

func (s *ECSPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	e, err := resolveECSModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	plan, err := e.Plan()
	if err != nil {
		return nil, fmt.Errorf("ecs_plan step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plan":     plan,
		"service":  s.service,
		"provider": plan.Provider,
		"actions":  plan.Actions,
	}}, nil
}

// ─── ecs_apply ────────────────────────────────────────────────────────────────

// ECSApplyStep calls Apply() on a named platform.ecs module.
type ECSApplyStep struct {
	name    string
	service string
	app     modular.Application
}

// NewECSApplyStepFactory returns a StepFactory for step.ecs_apply.
func NewECSApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("ecs_apply step %q: 'service' is required", name)
		}
		return &ECSApplyStep{name: name, service: service, app: app}, nil
	}
}

func (s *ECSApplyStep) Name() string { return s.name }

func (s *ECSApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	e, err := resolveECSModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	result, err := e.Apply()
	if err != nil {
		return nil, fmt.Errorf("ecs_apply step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"result":  result,
		"service": s.service,
		"success": result.Success,
		"message": result.Message,
		"state":   result.State,
	}}, nil
}

// ─── ecs_status ───────────────────────────────────────────────────────────────

// ECSStatusStep calls Status() on a named platform.ecs module.
type ECSStatusStep struct {
	name    string
	service string
	app     modular.Application
}

// NewECSStatusStepFactory returns a StepFactory for step.ecs_status.
func NewECSStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("ecs_status step %q: 'service' is required", name)
		}
		return &ECSStatusStep{name: name, service: service, app: app}, nil
	}
}

func (s *ECSStatusStep) Name() string { return s.name }

func (s *ECSStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	e, err := resolveECSModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	st, err := e.Status()
	if err != nil {
		return nil, fmt.Errorf("ecs_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"status":  st,
		"service": s.service,
	}}, nil
}

// ─── ecs_destroy ──────────────────────────────────────────────────────────────

// ECSDestroyStep calls Destroy() on a named platform.ecs module.
type ECSDestroyStep struct {
	name    string
	service string
	app     modular.Application
}

// NewECSDestroyStepFactory returns a StepFactory for step.ecs_destroy.
func NewECSDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := cfg["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("ecs_destroy step %q: 'service' is required", name)
		}
		return &ECSDestroyStep{name: name, service: service, app: app}, nil
	}
}

func (s *ECSDestroyStep) Name() string { return s.name }

func (s *ECSDestroyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	e, err := resolveECSModule(s.app, s.service, s.name)
	if err != nil {
		return nil, err
	}
	if err := e.Destroy(); err != nil {
		return nil, fmt.Errorf("ecs_destroy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"destroyed": true,
		"service":   s.service,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveECSModule(app modular.Application, service, stepName string) (*PlatformECS, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[service]
	if !ok {
		return nil, fmt.Errorf("step %q: service %q not found in registry", stepName, service)
	}
	e, ok := svc.(*PlatformECS)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *PlatformECS (got %T)", stepName, service, svc)
	}
	return e, nil
}
