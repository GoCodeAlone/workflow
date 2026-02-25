package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── dns_plan ─────────────────────────────────────────────────────────────────

// DNSPlanStep calls Plan() on a named platform.dns module.
type DNSPlanStep struct {
	name string
	zone string
	app  modular.Application
}

// NewDNSPlanStepFactory returns a StepFactory for step.dns_plan.
func NewDNSPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		zone, _ := cfg["zone"].(string)
		if zone == "" {
			return nil, fmt.Errorf("dns_plan step %q: 'zone' is required", name)
		}
		return &DNSPlanStep{name: name, zone: zone, app: app}, nil
	}
}

func (s *DNSPlanStep) Name() string { return s.name }

func (s *DNSPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDNSModule(s.app, s.zone, s.name)
	if err != nil {
		return nil, err
	}
	plan, err := m.Plan()
	if err != nil {
		return nil, fmt.Errorf("dns_plan step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"plan":    plan,
		"zone":    s.zone,
		"changes": plan.Changes,
		"records": plan.Records,
	}}, nil
}

// ─── dns_apply ────────────────────────────────────────────────────────────────

// DNSApplyStep calls Apply() on a named platform.dns module.
type DNSApplyStep struct {
	name string
	zone string
	app  modular.Application
}

// NewDNSApplyStepFactory returns a StepFactory for step.dns_apply.
func NewDNSApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		zone, _ := cfg["zone"].(string)
		if zone == "" {
			return nil, fmt.Errorf("dns_apply step %q: 'zone' is required", name)
		}
		return &DNSApplyStep{name: name, zone: zone, app: app}, nil
	}
}

func (s *DNSApplyStep) Name() string { return s.name }

func (s *DNSApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDNSModule(s.app, s.zone, s.name)
	if err != nil {
		return nil, err
	}
	state, err := m.Apply()
	if err != nil {
		return nil, fmt.Errorf("dns_apply step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"state":    state,
		"zone":     s.zone,
		"zoneId":   state.ZoneID,
		"zoneName": state.ZoneName,
		"status":   state.Status,
		"records":  state.Records,
	}}, nil
}

// ─── dns_status ───────────────────────────────────────────────────────────────

// DNSStatusStep calls Status() on a named platform.dns module.
type DNSStatusStep struct {
	name string
	zone string
	app  modular.Application
}

// NewDNSStatusStepFactory returns a StepFactory for step.dns_status.
func NewDNSStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		zone, _ := cfg["zone"].(string)
		if zone == "" {
			return nil, fmt.Errorf("dns_status step %q: 'zone' is required", name)
		}
		return &DNSStatusStep{name: name, zone: zone, app: app}, nil
	}
}

func (s *DNSStatusStep) Name() string { return s.name }

func (s *DNSStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveDNSModule(s.app, s.zone, s.name)
	if err != nil {
		return nil, err
	}
	state, err := m.Status()
	if err != nil {
		return nil, fmt.Errorf("dns_status step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"state":  state,
		"zone":   s.zone,
		"status": state.Status,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveDNSModule(app modular.Application, zone, stepName string) (*PlatformDNS, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[zone]
	if !ok {
		return nil, fmt.Errorf("step %q: dns service %q not found in registry", stepName, zone)
	}
	m, ok := svc.(*PlatformDNS)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *PlatformDNS (got %T)", stepName, zone, svc)
	}
	return m, nil
}
