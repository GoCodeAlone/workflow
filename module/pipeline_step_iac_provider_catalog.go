package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/providerclient"
)

// staticRegions is the fallback region list when the provider does not
// advertise IaCProviderRegionLister. Covers the most common cloud regions
// across major providers; plugins that support a narrower or wider set
// will surface that via the live path.
var staticRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"eu-west-1",
	"eu-west-2",
	"eu-central-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-northeast-1",
}

// ─── step.iac_provider_catalog ───────────────────────────────────────────────

// IaCProviderCatalogStep resolves an IaCProvider, fetches regions and resource
// type capabilities, and returns a catalog suitable for UI rendering.
type IaCProviderCatalogStep struct {
	name     string
	provider string
	env      string
	app      modular.Application
}

// NewIaCProviderCatalogStepFactory returns a StepFactory for step.iac_provider_catalog.
func NewIaCProviderCatalogStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_catalog step %q: 'provider' is required", name)
		}
		env, _ := cfg["env"].(string)
		return &IaCProviderCatalogStep{
			name:     name,
			provider: providerName,
			env:      env,
			app:      app,
		}, nil
	}
}

func (s *IaCProviderCatalogStep) Name() string { return s.name }

func (s *IaCProviderCatalogStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_catalog")
	if err != nil {
		return nil, err
	}

	// Attempt to get live regions from the provider via the optional
	// RegionListerProvider capability accessor (PR-1 pattern).
	var regions []string
	source := "static"

	if rlp, ok := provider.(providerclient.RegionListerProvider); ok {
		if rl := rlp.RegionLister(); rl != nil {
			liveRegions, listErr := rl.ListRegions(ctx, s.env)
			if listErr != nil {
				// Non-fatal: fall back to static list and surface a warning in
				// the output so callers know the live path was attempted.
				source = "static_fallback_error"
			} else {
				regions = liveRegions
				source = "live"
			}
		}
	}

	if source != "live" {
		regions = append([]string(nil), staticRegions...)
	}

	// Collect resource types from provider capabilities.
	caps := provider.Capabilities()
	types := make([]map[string]any, 0, len(caps))
	for _, c := range caps {
		types = append(types, map[string]any{
			"resource_type": c.ResourceType,
			"tier":          c.Tier,
			"operations":    c.Operations,
		})
	}

	return &StepResult{Output: map[string]any{
		"provider": s.provider,
		"regions":  regions,
		"types":    types,
		"source":   source,
	}}, nil
}
