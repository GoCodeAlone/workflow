package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── apigw_plan ───────────────────────────────────────────────────────────────

// ApigwPlanStep calls Plan() on a named platform.apigateway module.
type ApigwPlanStep struct {
	name    string
	gateway string
	app     modular.Application
}

// NewApigwPlanStepFactory returns a StepFactory for step.apigw_plan.
func NewApigwPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		gateway, _ := cfg["gateway"].(string)
		if gateway == "" {
			return nil, fmt.Errorf("apigw_plan step %q: 'gateway' is required", name)
		}
		return &ApigwPlanStep{name: name, gateway: gateway, app: app}, nil
	}
}

func (s *ApigwPlanStep) Name() string { return s.name }

func (s *ApigwPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	gw, err := resolveAPIGatewayModule(s.app, s.gateway, s.name)
	if err != nil {
		return nil, err
	}
	plan, err := gw.Plan()
	if err != nil {
		return nil, fmt.Errorf("apigw_plan step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plan":    plan,
		"gateway": s.gateway,
		"name":    plan.Name,
		"stage":   plan.Stage,
		"changes": plan.Changes,
		"routes":  plan.Routes,
	}}, nil
}

// ─── apigw_apply ──────────────────────────────────────────────────────────────

// ApigwApplyStep calls Apply() on a named platform.apigateway module.
type ApigwApplyStep struct {
	name    string
	gateway string
	app     modular.Application
}

// NewApigwApplyStepFactory returns a StepFactory for step.apigw_apply.
func NewApigwApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		gateway, _ := cfg["gateway"].(string)
		if gateway == "" {
			return nil, fmt.Errorf("apigw_apply step %q: 'gateway' is required", name)
		}
		return &ApigwApplyStep{name: name, gateway: gateway, app: app}, nil
	}
}

func (s *ApigwApplyStep) Name() string { return s.name }

func (s *ApigwApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	gw, err := resolveAPIGatewayModule(s.app, s.gateway, s.name)
	if err != nil {
		return nil, err
	}
	state, err := gw.Apply()
	if err != nil {
		return nil, fmt.Errorf("apigw_apply step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"state":    state,
		"gateway":  s.gateway,
		"id":       state.ID,
		"endpoint": state.Endpoint,
		"status":   state.Status,
	}}, nil
}

// ─── apigw_status ─────────────────────────────────────────────────────────────

// ApigwStatusStep calls Status() on a named platform.apigateway module.
type ApigwStatusStep struct {
	name    string
	gateway string
	app     modular.Application
}

// NewApigwStatusStepFactory returns a StepFactory for step.apigw_status.
func NewApigwStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		gateway, _ := cfg["gateway"].(string)
		if gateway == "" {
			return nil, fmt.Errorf("apigw_status step %q: 'gateway' is required", name)
		}
		return &ApigwStatusStep{name: name, gateway: gateway, app: app}, nil
	}
}

func (s *ApigwStatusStep) Name() string { return s.name }

func (s *ApigwStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	gw, err := resolveAPIGatewayModule(s.app, s.gateway, s.name)
	if err != nil {
		return nil, err
	}
	st, err := gw.Status()
	if err != nil {
		return nil, fmt.Errorf("apigw_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"status":  st,
		"gateway": s.gateway,
	}}, nil
}

// ─── apigw_destroy ────────────────────────────────────────────────────────────

// ApigwDestroyStep calls Destroy() on a named platform.apigateway module.
type ApigwDestroyStep struct {
	name    string
	gateway string
	app     modular.Application
}

// NewApigwDestroyStepFactory returns a StepFactory for step.apigw_destroy.
func NewApigwDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		gateway, _ := cfg["gateway"].(string)
		if gateway == "" {
			return nil, fmt.Errorf("apigw_destroy step %q: 'gateway' is required", name)
		}
		return &ApigwDestroyStep{name: name, gateway: gateway, app: app}, nil
	}
}

func (s *ApigwDestroyStep) Name() string { return s.name }

func (s *ApigwDestroyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	gw, err := resolveAPIGatewayModule(s.app, s.gateway, s.name)
	if err != nil {
		return nil, err
	}
	if err := gw.Destroy(); err != nil {
		return nil, fmt.Errorf("apigw_destroy step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"destroyed": true,
		"gateway":   s.gateway,
	}}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func resolveAPIGatewayModule(app modular.Application, gateway, stepName string) (*PlatformAPIGateway, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[gateway]
	if !ok {
		return nil, fmt.Errorf("step %q: gateway service %q not found in registry", stepName, gateway)
	}
	gw, ok := svc.(*PlatformAPIGateway)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *PlatformAPIGateway (got %T)", stepName, gateway, svc)
	}
	return gw, nil
}
