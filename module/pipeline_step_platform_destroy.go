package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// PlatformDestroyStep implements a pipeline step that destroys previously
// provisioned resources. It reads resource outputs from the pipeline context
// and calls the provider's resource driver Delete method for each.
type PlatformDestroyStep struct {
	name            string
	providerService string
	resourcesFrom   string
}

// NewPlatformDestroyStepFactory returns a StepFactory that creates PlatformDestroyStep instances.
func NewPlatformDestroyStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		providerService, _ := config["provider_service"].(string)
		if providerService == "" {
			return nil, fmt.Errorf("platform_destroy step %q: 'provider_service' is required", name)
		}

		resourcesFrom, _ := config["resources_from"].(string)
		if resourcesFrom == "" {
			resourcesFrom = "applied_resources"
		}

		return &PlatformDestroyStep{
			name:            name,
			providerService: providerService,
			resourcesFrom:   resourcesFrom,
		}, nil
	}
}

// Name returns the step name.
func (s *PlatformDestroyStep) Name() string { return s.name }

// Execute destroys each resource by calling Delete on the provider's resource driver.
func (s *PlatformDestroyStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	provider, err := s.resolveProvider(pc)
	if err != nil {
		return nil, fmt.Errorf("platform_destroy step %q: %w", s.name, err)
	}

	resources, err := s.resolveResources(pc)
	if err != nil {
		return nil, fmt.Errorf("platform_destroy step %q: %w", s.name, err)
	}

	var destroyed []string
	var failed []map[string]any

	for _, res := range resources {
		driver, driverErr := provider.ResourceDriver(res.ProviderType)
		if driverErr != nil {
			failed = append(failed, map[string]any{
				"resource": res.Name,
				"error":    driverErr.Error(),
			})
			continue
		}

		if delErr := driver.Delete(ctx, res.Name); delErr != nil {
			failed = append(failed, map[string]any{
				"resource": res.Name,
				"error":    delErr.Error(),
			})
			continue
		}
		destroyed = append(destroyed, res.Name)
	}

	result := &StepResult{
		Output: map[string]any{
			"destroy_summary": map[string]any{
				"provider":        provider.Name(),
				"total_resources": len(resources),
				"destroyed_count": len(destroyed),
				"destroyed":       destroyed,
				"failed_count":    len(failed),
				"failed_details":  failed,
			},
		},
	}

	if len(failed) > 0 {
		return nil, fmt.Errorf("platform_destroy step %q: %d of %d resources failed to destroy", s.name, len(failed), len(resources))
	}

	return result, nil
}

// resolveProvider looks up the platform.Provider from the pipeline context.
func (s *PlatformDestroyStep) resolveProvider(pc *PipelineContext) (platform.Provider, error) {
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
func (s *PlatformDestroyStep) resolveResources(pc *PipelineContext) ([]*platform.ResourceOutput, error) {
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
