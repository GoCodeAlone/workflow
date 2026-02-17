package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// DriftReport describes drift detected for a single resource.
type DriftReport struct {
	// ResourceName is the name of the resource that was checked.
	ResourceName string `json:"resourceName"`

	// ResourceType is the provider-specific resource type.
	ResourceType string `json:"resourceType"`

	// Drifted indicates whether drift was detected.
	Drifted bool `json:"drifted"`

	// Diffs contains the field-level differences between desired and actual state.
	Diffs []platform.DiffEntry `json:"diffs,omitempty"`

	// Error is set if the drift check failed for this resource.
	Error string `json:"error,omitempty"`
}

// DriftCheckStep implements a pipeline step that checks for configuration drift
// by comparing expected resource state against the actual provider state. For
// each resource, it calls the provider's resource driver Diff method.
type DriftCheckStep struct {
	name            string
	providerService string
	resourcesFrom   string
}

// NewDriftCheckStepFactory returns a StepFactory that creates DriftCheckStep instances.
func NewDriftCheckStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		providerService, _ := config["provider_service"].(string)
		if providerService == "" {
			return nil, fmt.Errorf("drift_check step %q: 'provider_service' is required", name)
		}

		resourcesFrom, _ := config["resources_from"].(string)
		if resourcesFrom == "" {
			resourcesFrom = "applied_resources"
		}

		return &DriftCheckStep{
			name:            name,
			providerService: providerService,
			resourcesFrom:   resourcesFrom,
		}, nil
	}
}

// Name returns the step name.
func (s *DriftCheckStep) Name() string { return s.name }

// Execute checks each resource for drift by comparing desired properties against
// the provider's actual state using the Diff method on resource drivers.
func (s *DriftCheckStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	provider, err := s.resolveProvider(pc)
	if err != nil {
		return nil, fmt.Errorf("drift_check step %q: %w", s.name, err)
	}

	resources, err := s.resolveResources(pc)
	if err != nil {
		return nil, fmt.Errorf("drift_check step %q: %w", s.name, err)
	}

	var reports []DriftReport
	driftDetected := false

	for _, res := range resources {
		report := DriftReport{
			ResourceName: res.Name,
			ResourceType: res.ProviderType,
		}

		driver, driverErr := provider.ResourceDriver(res.ProviderType)
		if driverErr != nil {
			report.Error = driverErr.Error()
			reports = append(reports, report)
			continue
		}

		diffs, diffErr := driver.Diff(ctx, res.Name, res.Properties)
		if diffErr != nil {
			report.Error = diffErr.Error()
			reports = append(reports, report)
			continue
		}

		if len(diffs) > 0 {
			report.Drifted = true
			report.Diffs = diffs
			driftDetected = true
		}
		reports = append(reports, report)
	}

	return &StepResult{
		Output: map[string]any{
			"drift_reports": reports,
			"drift_summary": map[string]any{
				"provider":       provider.Name(),
				"total_checked":  len(resources),
				"drift_detected": driftDetected,
				"drifted_count":  countDrifted(reports),
			},
		},
	}, nil
}

// countDrifted returns the number of drift reports where drift was detected.
func countDrifted(reports []DriftReport) int {
	count := 0
	for _, r := range reports {
		if r.Drifted {
			count++
		}
	}
	return count
}

// resolveProvider looks up the platform.Provider from the pipeline context.
func (s *DriftCheckStep) resolveProvider(pc *PipelineContext) (platform.Provider, error) {
	raw, ok := pc.Current[s.providerService]
	if !ok {
		return nil, fmt.Errorf("provider service %q not found in pipeline context", s.providerService)
	}
	provider, ok := raw.(platform.Provider)
	if !ok {
		return nil, fmt.Errorf("pipeline context key %q is not a platform.Provider", s.providerService)
	}
	return provider, nil
}

// resolveResources reads the resource output list from the pipeline context.
func (s *DriftCheckStep) resolveResources(pc *PipelineContext) ([]*platform.ResourceOutput, error) {
	raw, ok := pc.Current[s.resourcesFrom]
	if !ok {
		return nil, fmt.Errorf("resources key %q not found in pipeline context", s.resourcesFrom)
	}

	switch v := raw.(type) {
	case []*platform.ResourceOutput:
		return v, nil
	case []any:
		var resources []*platform.ResourceOutput
		for i, item := range v {
			res, ok := item.(*platform.ResourceOutput)
			if !ok {
				return nil, fmt.Errorf("resources[%d] is not a *ResourceOutput", i)
			}
			resources = append(resources, res)
		}
		return resources, nil
	default:
		return nil, fmt.Errorf("resources key %q has unexpected type %T", s.resourcesFrom, raw)
	}
}
