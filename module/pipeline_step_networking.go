package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── network_plan ─────────────────────────────────────────────────────────────

// NetworkPlanStep calls Plan() on a named platform.networking module.
type NetworkPlanStep struct {
	name    string
	network string
	app     modular.Application
}

// NewNetworkPlanStepFactory returns a StepFactory for step.network_plan.
func NewNetworkPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		network, _ := cfg["network"].(string)
		if network == "" {
			return nil, fmt.Errorf("network_plan step %q: 'network' is required", name)
		}
		return &NetworkPlanStep{name: name, network: network, app: app}, nil
	}
}

func (s *NetworkPlanStep) Name() string { return s.name }

func (s *NetworkPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveNetworkModule(s.app, s.network, s.name)
	if err != nil {
		return nil, err
	}
	plan, err := m.Plan()
	if err != nil {
		return nil, fmt.Errorf("network_plan step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plan":    plan,
		"network": s.network,
		"changes": plan.Changes,
		"vpc":     plan.VPC,
	}}, nil
}

// ─── network_apply ────────────────────────────────────────────────────────────

// NetworkApplyStep calls Apply() on a named platform.networking module.
type NetworkApplyStep struct {
	name    string
	network string
	app     modular.Application
}

// NewNetworkApplyStepFactory returns a StepFactory for step.network_apply.
func NewNetworkApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		network, _ := cfg["network"].(string)
		if network == "" {
			return nil, fmt.Errorf("network_apply step %q: 'network' is required", name)
		}
		return &NetworkApplyStep{name: name, network: network, app: app}, nil
	}
}

func (s *NetworkApplyStep) Name() string { return s.name }

func (s *NetworkApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveNetworkModule(s.app, s.network, s.name)
	if err != nil {
		return nil, err
	}
	state, err := m.Apply()
	if err != nil {
		return nil, fmt.Errorf("network_apply step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"state":   state,
		"network": s.network,
		"vpcId":   state.VPCID,
		"status":  state.Status,
	}}, nil
}

// ─── network_status ───────────────────────────────────────────────────────────

// NetworkStatusStep calls Status() on a named platform.networking module.
type NetworkStatusStep struct {
	name    string
	network string
	app     modular.Application
}

// NewNetworkStatusStepFactory returns a StepFactory for step.network_status.
func NewNetworkStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		network, _ := cfg["network"].(string)
		if network == "" {
			return nil, fmt.Errorf("network_status step %q: 'network' is required", name)
		}
		return &NetworkStatusStep{name: name, network: network, app: app}, nil
	}
}

func (s *NetworkStatusStep) Name() string { return s.name }

func (s *NetworkStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveNetworkModule(s.app, s.network, s.name)
	if err != nil {
		return nil, err
	}
	st, err := m.Status()
	if err != nil {
		return nil, fmt.Errorf("network_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"status":  st,
		"network": s.network,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveNetworkModule(app modular.Application, network, stepName string) (*PlatformNetworking, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[network]
	if !ok {
		return nil, fmt.Errorf("step %q: network service %q not found in registry", stepName, network)
	}
	m, ok := svc.(*PlatformNetworking)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *PlatformNetworking (got %T)", stepName, network, svc)
	}
	return m, nil
}
