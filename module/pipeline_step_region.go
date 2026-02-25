package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ─── region_deploy ────────────────────────────────────────────────────────────

// RegionDeployStep deploys to a specific region via a platform.region module.
type RegionDeployStep struct {
	name   string
	module string
	region string
	app    modular.Application
}

// NewRegionDeployStepFactory returns a StepFactory for step.region_deploy.
func NewRegionDeployStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		mod, _ := cfg["module"].(string)
		if mod == "" {
			return nil, fmt.Errorf("region_deploy step %q: 'module' is required", name)
		}
		region, _ := cfg["region"].(string)
		if region == "" {
			return nil, fmt.Errorf("region_deploy step %q: 'region' is required", name)
		}
		return &RegionDeployStep{name: name, module: mod, region: region, app: app}, nil
	}
}

func (s *RegionDeployStep) Name() string { return s.name }

func (s *RegionDeployStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveMultiRegionModule(s.app, s.module, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.Deploy(s.region); err != nil {
		return nil, fmt.Errorf("region_deploy step %q: %w", s.name, err)
	}
	st, err := m.Status()
	if err != nil {
		return nil, fmt.Errorf("region_deploy step %q: status: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"module": s.module,
		"region": s.region,
		"status": st.Status,
	}}, nil
}

// ─── region_promote ───────────────────────────────────────────────────────────

// RegionPromoteStep promotes a region from secondary to primary.
type RegionPromoteStep struct {
	name   string
	module string
	region string
	app    modular.Application
}

// NewRegionPromoteStepFactory returns a StepFactory for step.region_promote.
func NewRegionPromoteStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		mod, _ := cfg["module"].(string)
		if mod == "" {
			return nil, fmt.Errorf("region_promote step %q: 'module' is required", name)
		}
		region, _ := cfg["region"].(string)
		if region == "" {
			return nil, fmt.Errorf("region_promote step %q: 'region' is required", name)
		}
		return &RegionPromoteStep{name: name, module: mod, region: region, app: app}, nil
	}
}

func (s *RegionPromoteStep) Name() string { return s.name }

func (s *RegionPromoteStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveMultiRegionModule(s.app, s.module, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.Promote(s.region); err != nil {
		return nil, fmt.Errorf("region_promote step %q: %w", s.name, err)
	}
	st, _ := m.Status()
	return &StepResult{Output: map[string]any{
		"module":        s.module,
		"promoted":      s.region,
		"primaryRegion": st.PrimaryRegion,
	}}, nil
}

// ─── region_failover ──────────────────────────────────────────────────────────

// RegionFailoverStep triggers failover from one region to another.
type RegionFailoverStep struct {
	name   string
	module string
	from   string
	to     string
	app    modular.Application
}

// NewRegionFailoverStepFactory returns a StepFactory for step.region_failover.
func NewRegionFailoverStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		mod, _ := cfg["module"].(string)
		if mod == "" {
			return nil, fmt.Errorf("region_failover step %q: 'module' is required", name)
		}
		from, _ := cfg["from"].(string)
		if from == "" {
			return nil, fmt.Errorf("region_failover step %q: 'from' is required", name)
		}
		to, _ := cfg["to"].(string)
		if to == "" {
			return nil, fmt.Errorf("region_failover step %q: 'to' is required", name)
		}
		return &RegionFailoverStep{name: name, module: mod, from: from, to: to, app: app}, nil
	}
}

func (s *RegionFailoverStep) Name() string { return s.name }

func (s *RegionFailoverStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveMultiRegionModule(s.app, s.module, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.Failover(s.from, s.to); err != nil {
		return nil, fmt.Errorf("region_failover step %q: %w", s.name, err)
	}
	st, _ := m.Status()
	return &StepResult{Output: map[string]any{
		"module":       s.module,
		"from":         s.from,
		"to":           s.to,
		"activeRegion": st.ActiveRegion,
		"status":       st.Status,
	}}, nil
}

// ─── region_status ────────────────────────────────────────────────────────────

// RegionStatusStep checks health across all regions.
type RegionStatusStep struct {
	name   string
	module string
	app    modular.Application
}

// NewRegionStatusStepFactory returns a StepFactory for step.region_status.
func NewRegionStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		mod, _ := cfg["module"].(string)
		if mod == "" {
			return nil, fmt.Errorf("region_status step %q: 'module' is required", name)
		}
		return &RegionStatusStep{name: name, module: mod, app: app}, nil
	}
}

func (s *RegionStatusStep) Name() string { return s.name }

func (s *RegionStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveMultiRegionModule(s.app, s.module, s.name)
	if err != nil {
		return nil, err
	}
	healths, err := m.CheckHealth()
	if err != nil {
		return nil, fmt.Errorf("region_status step %q: %w", s.name, err)
	}
	st, _ := m.Status()
	return &StepResult{Output: map[string]any{
		"module":        s.module,
		"regions":       healths,
		"activeRegion":  st.ActiveRegion,
		"primaryRegion": st.PrimaryRegion,
		"status":        st.Status,
	}}, nil
}

// ─── region_weight ────────────────────────────────────────────────────────────

// RegionWeightStep adjusts traffic routing weights for a region.
type RegionWeightStep struct {
	name   string
	module string
	region string
	weight int
	app    modular.Application
}

// NewRegionWeightStepFactory returns a StepFactory for step.region_weight.
func NewRegionWeightStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		mod, _ := cfg["module"].(string)
		if mod == "" {
			return nil, fmt.Errorf("region_weight step %q: 'module' is required", name)
		}
		region, _ := cfg["region"].(string)
		if region == "" {
			return nil, fmt.Errorf("region_weight step %q: 'region' is required", name)
		}
		weight, ok := intFromAny(cfg["weight"])
		if !ok {
			return nil, fmt.Errorf("region_weight step %q: 'weight' is required (integer 0-100)", name)
		}
		return &RegionWeightStep{name: name, module: mod, region: region, weight: weight, app: app}, nil
	}
}

func (s *RegionWeightStep) Name() string { return s.name }

func (s *RegionWeightStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveMultiRegionModule(s.app, s.module, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.SetWeight(s.region, s.weight); err != nil {
		return nil, fmt.Errorf("region_weight step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"module":  s.module,
		"region":  s.region,
		"weight":  s.weight,
		"weights": m.Weights(),
	}}, nil
}

// ─── region_sync ──────────────────────────────────────────────────────────────

// RegionSyncStep synchronises state/config across all regions.
type RegionSyncStep struct {
	name   string
	module string
	app    modular.Application
}

// NewRegionSyncStepFactory returns a StepFactory for step.region_sync.
func NewRegionSyncStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		mod, _ := cfg["module"].(string)
		if mod == "" {
			return nil, fmt.Errorf("region_sync step %q: 'module' is required", name)
		}
		return &RegionSyncStep{name: name, module: mod, app: app}, nil
	}
}

func (s *RegionSyncStep) Name() string { return s.name }

func (s *RegionSyncStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	m, err := resolveMultiRegionModule(s.app, s.module, s.name)
	if err != nil {
		return nil, err
	}
	if err := m.Sync(); err != nil {
		return nil, fmt.Errorf("region_sync step %q: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"module": s.module,
		"synced": true,
	}}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func resolveMultiRegionModule(app modular.Application, module, stepName string) (*MultiRegionModule, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[module]
	if !ok {
		return nil, fmt.Errorf("step %q: module service %q not found in registry", stepName, module)
	}
	m, ok := svc.(*MultiRegionModule)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q is not a *MultiRegionModule (got %T)", stepName, module, svc)
	}
	return m, nil
}
