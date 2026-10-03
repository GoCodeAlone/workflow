package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/sensitiveinputs"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// Drivers compare a separate planning copy. Sensitive leaves stay references
// even there; actions and hashes always retain the declarative source config.
func computeDeclarativeInfraPlan(ctx context.Context, provider interfaces.IaCProvider, specs []interfaces.ResourceSpec, current []interfaces.ResourceState, cfg *config.WorkflowConfig, envName string) (interfaces.IaCPlan, error) {
	planning, err := prepareDeclarativePlanningSpecs(ctx, provider, specs, current, cfg, envName)
	if err != nil {
		return interfaces.IaCPlan{}, err
	}
	plan, err := computeInfraPlan(ctx, provider, planning, current)
	if err != nil {
		return interfaces.IaCPlan{}, err
	}
	byName := make(map[string]interfaces.ResourceSpec, len(specs))
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	for i := range plan.Actions {
		action := &plan.Actions[i]
		if spec, ok := byName[action.Resource.Name]; ok {
			action.Resource = spec
			action.ResolvedConfigHash = platform.ConfigHash(spec.Config)
		}
	}
	return plan, nil
}

func prepareDeclarativePlanningSpecs(ctx context.Context, provider interfaces.IaCProvider, specs []interfaces.ResourceSpec, current []interfaces.ResourceState, cfg *config.WorkflowConfig, envName string) ([]interfaces.ResourceSpec, error) {
	preserved := make(map[string][]string, len(specs))
	for _, spec := range specs {
		if provider == nil {
			continue
		}
		driver, err := provider.ResourceDriver(spec.Type)
		if err != nil {
			if errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
				continue
			}
			return nil, fmt.Errorf("%s/%s: resolve resource driver: %w", spec.Type, spec.Name, err)
		}
		declarer, ok := driver.(interfaces.ResourceSensitiveInputDeclarer)
		if !ok {
			continue
		}
		paths, err := declarer.SensitiveInputPaths(ctx)
		if errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s/%s: sensitive input discovery: %w", spec.Type, spec.Name, err)
		}
		if err := sensitiveinputs.ValidateReferences(spec.Config, paths); err != nil {
			return nil, fmt.Errorf("%s/%s: %w", spec.Type, spec.Name, err)
		}
		preserved[spec.Name], err = sensitiveinputs.Expand(spec.Config, paths)
		if err != nil {
			return nil, err
		}
	}
	planning, _, err := resolveSpecsAgainstStatePreserving(specs, current, cfg, envName, preserved)
	return planning, err
}
