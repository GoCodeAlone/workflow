package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// PlatformApplyStep implements a pipeline step that applies a previously
// generated platform plan. It reads a Plan from the pipeline context, executes
// each action through the provider's resource drivers, and outputs the
// resulting resource states.
type PlatformApplyStep struct {
	name            string
	providerService string
	planFrom        string
}

// NewPlatformApplyStepFactory returns a StepFactory that creates PlatformApplyStep instances.
func NewPlatformApplyStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		providerService, _ := config["provider_service"].(string)
		if providerService == "" {
			return nil, fmt.Errorf("platform_apply step %q: 'provider_service' is required", name)
		}

		planFrom, _ := config["plan_from"].(string)
		if planFrom == "" {
			planFrom = "platform_plan"
		}

		return &PlatformApplyStep{
			name:            name,
			providerService: providerService,
			planFrom:        planFrom,
		}, nil
	}
}

// Name returns the step name.
func (s *PlatformApplyStep) Name() string { return s.name }

// Execute applies the plan by executing each action through the provider's resource drivers.
func (s *PlatformApplyStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	provider, err := s.resolveProvider(pc)
	if err != nil {
		return nil, fmt.Errorf("platform_apply step %q: %w", s.name, err)
	}

	plan, err := s.resolvePlan(pc)
	if err != nil {
		return nil, fmt.Errorf("platform_apply step %q: %w", s.name, err)
	}

	var applied []*platform.ResourceOutput
	var failed []map[string]any

	for _, action := range plan.Actions {
		driver, driverErr := provider.ResourceDriver(action.ResourceType)
		if driverErr != nil {
			failed = append(failed, map[string]any{
				"resource": action.ResourceName,
				"error":    driverErr.Error(),
			})
			continue
		}

		var output *platform.ResourceOutput
		var execErr error

		switch action.Action {
		case "create":
			output, execErr = driver.Create(ctx, action.ResourceName, action.After)
		case "update":
			output, execErr = driver.Update(ctx, action.ResourceName, action.Before, action.After)
		case "delete":
			execErr = driver.Delete(ctx, action.ResourceName)
		case "no-op":
			output, execErr = driver.Read(ctx, action.ResourceName)
		default:
			execErr = fmt.Errorf("unknown action %q", action.Action)
		}

		if execErr != nil {
			failed = append(failed, map[string]any{
				"resource": action.ResourceName,
				"action":   action.Action,
				"error":    execErr.Error(),
			})
			continue
		}

		if output != nil {
			applied = append(applied, output)
		}
	}

	result := &StepResult{
		Output: map[string]any{
			"applied_resources": applied,
			"apply_summary": map[string]any{
				"provider":       provider.Name(),
				"total_actions":  len(plan.Actions),
				"applied_count":  len(applied),
				"failed_count":   len(failed),
				"failed_details": failed,
			},
		},
	}

	if len(failed) > 0 {
		return nil, fmt.Errorf("platform_apply step %q: %d of %d actions failed", s.name, len(failed), len(plan.Actions))
	}

	return result, nil
}

// resolveProvider looks up the platform.Provider from the pipeline context.
func (s *PlatformApplyStep) resolveProvider(pc *PipelineContext) (platform.Provider, error) {
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

// resolvePlan reads the Plan from the pipeline context.
func (s *PlatformApplyStep) resolvePlan(pc *PipelineContext) (*platform.Plan, error) {
	raw, ok := pc.Current[s.planFrom]
	if !ok {
		return nil, fmt.Errorf("plan key %q not found in pipeline context", s.planFrom)
	}
	plan, ok := raw.(*platform.Plan)
	if !ok {
		return nil, fmt.Errorf("pipeline context key %q is not a *platform.Plan", s.planFrom)
	}
	return plan, nil
}
