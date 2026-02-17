package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// PlatformWorkflowHandler handles platform infrastructure workflows.
// It orchestrates plan/apply/destroy lifecycle operations against a
// platform.Provider using capability declarations from configuration.
type PlatformWorkflowHandler struct {
	provider        platform.Provider
	contextResolver platform.ContextResolver
	config          *platform.PlatformConfig
}

// NewPlatformWorkflowHandler creates a new platform workflow handler.
func NewPlatformWorkflowHandler() *PlatformWorkflowHandler {
	return &PlatformWorkflowHandler{}
}

// CanHandle returns true if this handler can process the given workflow type.
func (h *PlatformWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "platform"
}

// ConfigureWorkflow sets up the workflow from configuration.
// It parses the platform configuration and initializes the provider.
func (h *PlatformWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	cfgMap, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid platform workflow configuration format")
	}

	platCfg, err := platform.ParsePlatformConfig(cfgMap)
	if err != nil {
		return fmt.Errorf("failed to parse platform config: %w", err)
	}

	if err := platform.ValidatePlatformConfig(platCfg); err != nil {
		return fmt.Errorf("invalid platform config: %w", err)
	}

	h.config = platCfg

	// Look for a registered provider by name in the service registry.
	var provider platform.Provider
	providerSvcName := "platform.provider." + platCfg.Provider.Name
	if err := app.GetService(providerSvcName, &provider); err != nil || provider == nil {
		// Try the generic name as fallback.
		if err := app.GetService("platform.provider", &provider); err != nil || provider == nil {
			app.Logger().Debug("No platform provider found in service registry", "name", providerSvcName)
		}
	}

	if provider != nil {
		h.provider = provider
	}

	// Look for a context resolver in the service registry.
	var resolver platform.ContextResolver
	if err := app.GetService("platform.context_resolver", &resolver); err == nil && resolver != nil {
		h.contextResolver = resolver
	}

	return nil
}

// SetProvider sets the platform provider for this handler.
func (h *PlatformWorkflowHandler) SetProvider(p platform.Provider) {
	h.provider = p
}

// SetContextResolver sets the context resolver for this handler.
func (h *PlatformWorkflowHandler) SetContextResolver(r platform.ContextResolver) {
	h.contextResolver = r
}

// ExecuteWorkflow executes a platform workflow with the given action and input data.
// Supported actions: "plan", "apply", "destroy", "status".
func (h *PlatformWorkflowHandler) ExecuteWorkflow(ctx context.Context, _ string, action string, data map[string]any) (map[string]any, error) {
	if h.config == nil {
		return nil, fmt.Errorf("platform workflow not configured")
	}

	switch action {
	case "plan":
		return h.executePlan(ctx, data)
	case "apply":
		return h.executeApply(ctx, data)
	case "destroy":
		return h.executeDestroy(ctx, data)
	case "status":
		return h.executeStatus(ctx)
	default:
		return nil, fmt.Errorf("unknown platform action: %s", action)
	}
}

// executePlan generates an execution plan by mapping capabilities to provider resources.
func (h *PlatformWorkflowHandler) executePlan(ctx context.Context, data map[string]any) (map[string]any, error) {
	if h.provider == nil {
		return nil, fmt.Errorf("no platform provider configured")
	}

	tier := resolveTier(data)

	// Build platform context.
	pctx, err := h.buildPlatformContext(ctx, tier)
	if err != nil {
		return nil, fmt.Errorf("failed to build platform context: %w", err)
	}

	// Collect capability declarations for the requested tier.
	declarations := h.collectDeclarations(tier)
	if len(declarations) == 0 {
		return map[string]any{
			"plan_id": "",
			"actions": []any{},
			"message": "no capabilities declared for tier",
			"tier":    tier.String(),
		}, nil
	}

	// Validate tier boundaries if a context resolver is available.
	if h.contextResolver != nil {
		violations := h.contextResolver.ValidateTierBoundary(pctx, declarations)
		if len(violations) > 0 {
			violationMsgs := make([]string, len(violations))
			for i, v := range violations {
				violationMsgs[i] = v.Message
			}
			return nil, fmt.Errorf("tier boundary violations: %v", violationMsgs)
		}
	}

	// Map each capability to provider-specific resources.
	var actions []platform.PlanAction
	for _, decl := range declarations {
		resourcePlans, mapErr := h.provider.MapCapability(ctx, decl, pctx)
		if mapErr != nil {
			return nil, fmt.Errorf("failed to map capability %q: %w", decl.Name, mapErr)
		}

		for _, rp := range resourcePlans {
			actions = append(actions, platform.PlanAction{
				Action:       "create",
				ResourceName: rp.Name,
				ResourceType: rp.ResourceType,
				Provider:     h.provider.Name(),
				After:        rp.Properties,
			})
		}
	}

	plan := &platform.Plan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Tier:      tier,
		Context:   pctx.ContextPath(),
		Actions:   actions,
		CreatedAt: time.Now(),
		Status:    "pending",
		Provider:  h.provider.Name(),
	}

	// Persist plan if a state store is available.
	if ss := h.provider.StateStore(); ss != nil {
		if err := ss.SavePlan(ctx, plan); err != nil {
			return nil, fmt.Errorf("failed to save plan: %w", err)
		}
	}

	return map[string]any{
		"plan_id":      plan.ID,
		"tier":         plan.Tier.String(),
		"context":      plan.Context,
		"action_count": len(plan.Actions),
		"status":       plan.Status,
		"provider":     plan.Provider,
	}, nil
}

// executeApply executes an approved plan by creating/updating resources through the provider.
func (h *PlatformWorkflowHandler) executeApply(ctx context.Context, data map[string]any) (map[string]any, error) {
	if h.provider == nil {
		return nil, fmt.Errorf("no platform provider configured")
	}

	planID, _ := data["plan_id"].(string)
	if planID == "" {
		return nil, fmt.Errorf("plan_id is required for apply action")
	}

	ss := h.provider.StateStore()
	if ss == nil {
		return nil, fmt.Errorf("state store not available for plan retrieval")
	}

	plan, err := ss.GetPlan(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve plan %q: %w", planID, err)
	}

	if plan.Status != "pending" && plan.Status != "approved" {
		return nil, fmt.Errorf("plan %q is not in an applicable state (current: %s)", planID, plan.Status)
	}

	// Mark plan as applying.
	now := time.Now()
	plan.ApprovedAt = &now
	plan.Status = "applying"
	if approvedBy, ok := data["approved_by"].(string); ok {
		plan.ApprovedBy = approvedBy
	}
	if err := ss.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("failed to update plan status: %w", err)
	}

	// Execute each action in the plan.
	var outputs []*platform.ResourceOutput
	for _, action := range plan.Actions {
		driver, drvErr := h.provider.ResourceDriver(action.ResourceType)
		if drvErr != nil {
			plan.Status = "failed"
			_ = ss.SavePlan(ctx, plan)
			return nil, fmt.Errorf("no driver for resource type %q: %w", action.ResourceType, drvErr)
		}

		switch action.Action {
		case "create":
			output, createErr := driver.Create(ctx, action.ResourceName, action.After)
			if createErr != nil {
				plan.Status = "failed"
				_ = ss.SavePlan(ctx, plan)
				return nil, fmt.Errorf("failed to create resource %q: %w", action.ResourceName, createErr)
			}
			outputs = append(outputs, output)
		case "update":
			output, updateErr := driver.Update(ctx, action.ResourceName, action.Before, action.After)
			if updateErr != nil {
				plan.Status = "failed"
				_ = ss.SavePlan(ctx, plan)
				return nil, fmt.Errorf("failed to update resource %q: %w", action.ResourceName, updateErr)
			}
			outputs = append(outputs, output)
		case "delete":
			if delErr := driver.Delete(ctx, action.ResourceName); delErr != nil {
				plan.Status = "failed"
				_ = ss.SavePlan(ctx, plan)
				return nil, fmt.Errorf("failed to delete resource %q: %w", action.ResourceName, delErr)
			}
		}
	}

	// Propagate outputs to context resolver for downstream tiers.
	if h.contextResolver != nil && len(outputs) > 0 {
		pctx, buildErr := h.buildPlatformContext(ctx, plan.Tier)
		if buildErr == nil {
			_ = h.contextResolver.PropagateOutputs(ctx, pctx, outputs)
		}
	}

	// Save resource states.
	for _, output := range outputs {
		_ = ss.SaveResource(ctx, plan.Context, output)
	}

	plan.Status = "applied"
	_ = ss.SavePlan(ctx, plan)

	return map[string]any{
		"plan_id":         plan.ID,
		"status":          "applied",
		"resources_count": len(outputs),
		"provider":        h.provider.Name(),
	}, nil
}

// executeDestroy tears down all resources in the given context path.
func (h *PlatformWorkflowHandler) executeDestroy(ctx context.Context, data map[string]any) (map[string]any, error) {
	if h.provider == nil {
		return nil, fmt.Errorf("no platform provider configured")
	}

	ss := h.provider.StateStore()
	if ss == nil {
		return nil, fmt.Errorf("state store not available for destroy")
	}

	tier := resolveTier(data)
	pctx, err := h.buildPlatformContext(ctx, tier)
	if err != nil {
		return nil, fmt.Errorf("failed to build platform context: %w", err)
	}

	contextPath := pctx.ContextPath()
	resources, err := ss.ListResources(ctx, contextPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources for %q: %w", contextPath, err)
	}

	var destroyed int
	for i := len(resources) - 1; i >= 0; i-- {
		res := resources[i]
		driver, drvErr := h.provider.ResourceDriver(res.ProviderType)
		if drvErr != nil {
			return nil, fmt.Errorf("no driver for resource type %q: %w", res.ProviderType, drvErr)
		}

		if delErr := driver.Delete(ctx, res.Name); delErr != nil {
			return nil, fmt.Errorf("failed to destroy resource %q: %w", res.Name, delErr)
		}

		_ = ss.DeleteResource(ctx, contextPath, res.Name)
		destroyed++
	}

	return map[string]any{
		"status":          "destroyed",
		"resources_count": destroyed,
		"context":         contextPath,
		"provider":        h.provider.Name(),
	}, nil
}

// executeStatus returns the current status of platform resources.
func (h *PlatformWorkflowHandler) executeStatus(ctx context.Context) (map[string]any, error) {
	result := map[string]any{
		"configured": h.config != nil,
	}

	if h.config != nil {
		result["org"] = h.config.Org
		result["environment"] = h.config.Environment
		result["provider"] = h.config.Provider.Name
	}

	if h.provider != nil {
		result["provider_healthy"] = h.provider.Healthy(ctx) == nil
		result["provider_version"] = h.provider.Version()
	}

	return result, nil
}

// buildPlatformContext builds a PlatformContext for the given tier using the
// handler's configuration and optional context resolver.
func (h *PlatformWorkflowHandler) buildPlatformContext(ctx context.Context, tier platform.Tier) (*platform.PlatformContext, error) {
	if h.contextResolver != nil {
		return h.contextResolver.ResolveContext(ctx, h.config.Org, h.config.Environment, "", tier)
	}

	// Fallback: build a minimal context from config.
	return &platform.PlatformContext{
		Org:         h.config.Org,
		Environment: h.config.Environment,
		Tier:        tier,
	}, nil
}

// collectDeclarations gathers capability declarations from the config for the given tier.
func (h *PlatformWorkflowHandler) collectDeclarations(tier platform.Tier) []platform.CapabilityDeclaration {
	var tierCfg platform.TierConfig
	switch tier {
	case platform.TierInfrastructure:
		tierCfg = h.config.Tiers.Infrastructure
	case platform.TierSharedPrimitive:
		tierCfg = h.config.Tiers.SharedPrimitives
	case platform.TierApplication:
		tierCfg = h.config.Tiers.Application
	default:
		return nil
	}

	declarations := make([]platform.CapabilityDeclaration, 0, len(tierCfg.Capabilities))
	for _, cap := range tierCfg.Capabilities {
		declarations = append(declarations, platform.CapabilityDeclaration{
			Name:       cap.Name,
			Type:       cap.Type,
			Tier:       tier,
			Properties: cap.Properties,
			DependsOn:  cap.DependsOn,
		})
	}
	return declarations
}

// resolveTier extracts the tier from the data map, defaulting to TierApplication.
func resolveTier(data map[string]any) platform.Tier {
	if tierStr, ok := data["tier"].(string); ok {
		switch tierStr {
		case "infrastructure":
			return platform.TierInfrastructure
		case "shared_primitive":
			return platform.TierSharedPrimitive
		case "application":
			return platform.TierApplication
		}
	}
	if tierNum, ok := data["tier"].(float64); ok {
		t := platform.Tier(int(tierNum))
		if t.Valid() {
			return t
		}
	}
	return platform.TierApplication
}
