package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// PlatformPlanStep implements a pipeline step that generates an execution plan
// by mapping capability declarations through a platform provider. It reads
// capability declarations from the pipeline context, calls the provider's
// MapCapability method, and produces a platform.Plan in the pipeline context.
type PlatformPlanStep struct {
	name            string
	providerService string
	resourcesFrom   string
	contextOrg      string
	contextEnv      string
	contextApp      string
	tier            platform.Tier
	dryRun          bool
}

// NewPlatformPlanStepFactory returns a StepFactory that creates PlatformPlanStep instances.
func NewPlatformPlanStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		providerService, _ := config["provider_service"].(string)
		if providerService == "" {
			return nil, fmt.Errorf("platform_plan step %q: 'provider_service' is required", name)
		}

		resourcesFrom, _ := config["resources_from"].(string)
		if resourcesFrom == "" {
			resourcesFrom = "resources"
		}

		contextOrg, _ := config["context_org"].(string)
		contextEnv, _ := config["context_env"].(string)
		contextApp, _ := config["context_app"].(string)

		tier := platform.TierApplication
		if tierVal, ok := config["tier"].(int); ok {
			tier = platform.Tier(tierVal)
		}

		dryRun, _ := config["dry_run"].(bool)

		return &PlatformPlanStep{
			name:            name,
			providerService: providerService,
			resourcesFrom:   resourcesFrom,
			contextOrg:      contextOrg,
			contextEnv:      contextEnv,
			contextApp:      contextApp,
			tier:            tier,
			dryRun:          dryRun,
		}, nil
	}
}

// Name returns the step name.
func (s *PlatformPlanStep) Name() string { return s.name }

// Execute generates a platform plan by mapping capability declarations through the provider.
func (s *PlatformPlanStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	provider, err := s.resolveProvider(pc)
	if err != nil {
		return nil, fmt.Errorf("platform_plan step %q: %w", s.name, err)
	}

	declarations, err := s.resolveDeclarations(pc)
	if err != nil {
		return nil, fmt.Errorf("platform_plan step %q: %w", s.name, err)
	}

	pctx := &platform.PlatformContext{
		Org:         s.contextOrg,
		Environment: s.contextEnv,
		Application: s.contextApp,
		Tier:        s.tier,
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	var actions []platform.PlanAction
	for _, decl := range declarations {
		plans, mapErr := provider.MapCapability(ctx, decl, pctx)
		if mapErr != nil {
			return nil, fmt.Errorf("platform_plan step %q: failed to map capability %q: %w", s.name, decl.Name, mapErr)
		}
		for _, rp := range plans {
			actions = append(actions, platform.PlanAction{
				Action:       "create",
				ResourceName: rp.Name,
				ResourceType: rp.ResourceType,
				Provider:     provider.Name(),
				After:        rp.Properties,
			})
		}
	}

	plan := &platform.Plan{
		ID:        fmt.Sprintf("plan-%s-%d", s.name, time.Now().UnixNano()),
		Tier:      s.tier,
		Context:   pctx.ContextPath(),
		Actions:   actions,
		CreatedAt: time.Now(),
		Status:    "pending",
		Provider:  provider.Name(),
		DryRun:    s.dryRun,
	}

	return &StepResult{
		Output: map[string]any{
			"platform_plan": plan,
			"plan_summary": map[string]any{
				"provider":     provider.Name(),
				"action_count": len(actions),
				"tier":         s.tier.String(),
				"context":      pctx.ContextPath(),
				"dry_run":      s.dryRun,
			},
		},
	}, nil
}

// resolveProvider looks up the platform.Provider from the pipeline context.
func (s *PlatformPlanStep) resolveProvider(pc *PipelineContext) (platform.Provider, error) {
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

// resolveDeclarations reads the list of CapabilityDeclarations from the pipeline context.
func (s *PlatformPlanStep) resolveDeclarations(pc *PipelineContext) ([]platform.CapabilityDeclaration, error) {
	raw, ok := pc.Current[s.resourcesFrom]
	if !ok {
		return nil, fmt.Errorf("resources key %q not found in pipeline context", s.resourcesFrom)
	}

	switch v := raw.(type) {
	case []platform.CapabilityDeclaration:
		return v, nil
	case []any:
		var decls []platform.CapabilityDeclaration
		for i, item := range v {
			decl, ok := item.(platform.CapabilityDeclaration)
			if !ok {
				return nil, fmt.Errorf("resources[%d] is not a CapabilityDeclaration", i)
			}
			decls = append(decls, decl)
		}
		return decls, nil
	default:
		return nil, fmt.Errorf("resources key %q has unexpected type %T", s.resourcesFrom, raw)
	}
}
