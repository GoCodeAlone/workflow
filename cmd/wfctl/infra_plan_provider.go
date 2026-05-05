package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// computePlanForInfraSpecs is the W-3b plan-time entry point: it
// discovers iac.provider modules in cfgFile, groups desired specs by
// their `provider:` field, loads each referenced provider via the same
// loader the apply path uses, and dispatches the diff plan once per
// group via the package-level computeInfraPlan seam (so the apply and
// plan paths share a single override point for tests). Per-group plans
// are concatenated in first-reference-in-`desired` order — the order in
// which a group's first owning resource appears in the desired list,
// not iac.provider declaration order — and returned as a single
// IaCPlan.
//
// Plugin-load failure is FATAL with the v0.21.0 BREAKING-change error
// (rev3 fix per cycle-2 YAGNI: no --no-provider escape hatch —
// operators who need pure offline validation use `wfctl validate`).
//
// Configs that declare no iac.provider modules fall back to a nil
// provider, which the underlying ComputePlan tolerates with the legacy
// ConfigHash compare path. This preserves backwards compatibility for
// test fixtures and minimal scripts that pre-date the provider
// plumbing.
func computePlanForInfraSpecs(ctx context.Context, cfgFile, envName string, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return interfaces.IaCPlan{}, fmt.Errorf("load config: %w", err)
	}

	type providerDef struct {
		provType string
		provCfg  map[string]any
	}
	providerDefs := map[string]providerDef{}
	providerTypeCounts := map[string]int{}
	disabledProviders := map[string]struct{}{}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				disabledProviders[m.Name] = struct{}{}
				continue
			}
			modCfg = config.ExpandEnvInMap(resolved.Config)
		} else {
			modCfg = config.ExpandEnvInMap(m.Config)
		}
		pt, _ := modCfg["provider"].(string)
		providerDefs[m.Name] = providerDef{provType: pt, provCfg: modCfg}
		if pt != "" {
			providerTypeCounts[pt]++
		}
	}

	// Configs without iac.provider modules: fall back to ConfigHash-only
	// path. The nil provider is tolerated by platform.ComputePlan and
	// keeps minimal/legacy fixtures working without forcing them to
	// declare a provider just to render a plan.
	if len(providerDefs) == 0 {
		return computeInfraPlan(ctx, nil, desired, current)
	}

	type planGroup struct {
		moduleRef string
		provType  string
		provCfg   map[string]any
		specs     []interfaces.ResourceSpec
	}
	groups := map[string]*planGroup{}
	var groupOrder []string
	for _, spec := range desired {
		moduleRef, _ := spec.Config["provider"].(string)
		if moduleRef == "" {
			return interfaces.IaCPlan{}, fmt.Errorf("infra module %q (%s): missing required 'provider' field", spec.Name, spec.Type)
		}
		if _, exists := groups[moduleRef]; !exists {
			def, ok := providerDefs[moduleRef]
			if !ok {
				if _, disabled := disabledProviders[moduleRef]; disabled {
					return interfaces.IaCPlan{}, fmt.Errorf("infra module %q references provider %q which is disabled for environment %q", spec.Name, moduleRef, envName)
				}
				return interfaces.IaCPlan{}, fmt.Errorf("infra module %q references provider %q which is not declared as an iac.provider module", spec.Name, moduleRef)
			}
			if def.provType == "" {
				return interfaces.IaCPlan{}, fmt.Errorf("provider module %q has no 'provider' type configured", moduleRef)
			}
			groups[moduleRef] = &planGroup{moduleRef: moduleRef, provType: def.provType, provCfg: def.provCfg}
			groupOrder = append(groupOrder, moduleRef)
		}
		groups[moduleRef].specs = append(groups[moduleRef].specs, spec)
	}

	// Loop body wrapped in an IIFE so each provider's closer fires after
	// its group is computed, not deferred to function exit. Without this,
	// a 5-provider config would hold 5 gRPC connections open until
	// computePlanForInfraSpecs returned.
	var allActions []interfaces.PlanAction
	for _, ref := range groupOrder {
		g := groups[ref]
		groupActions, err := func() ([]interfaces.PlanAction, error) {
			provider, closer, loadErr := resolveIaCProvider(ctx, g.provType, g.provCfg)
			if loadErr != nil {
				// rev3 BREAKING CHANGE — literal error format documented
				// in the v0.21.0 CHANGELOG. No --no-provider escape
				// hatch: `wfctl validate` covers offline-config
				// validation. Note: no leading "error:" prefix here —
				// cmd/wfctl/main.go's top-level printer already emits
				// "error: %v" on command failure, so prefixing here
				// would produce double "error: error: ...".
				//
				// loadErr is wrapped with %w (not %v) so callers can
				// errors.Is/errors.As against the underlying loader
				// failure even after this error is re-wrapped by
				// runInfraPlan's `compute plan: %w`. Rendered text is
				// identical to %v.
				return nil, fmt.Errorf(`failed to load plugin %q: %w; wfctl infra plan now requires the plugin process to compute Diff (since v0.21.0)`, g.provType, loadErr)
			}
			if closer != nil {
				provType := g.provType
				defer func() {
					if cerr := closer.Close(); cerr != nil {
						fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", provType, cerr)
					}
				}()
			}

			allowProviderTypeFallback := providerTypeCounts[g.provType] == 1
			scopedCurrent := filterCurrentStateForProvider(current, g.provType, ref, g.specs, allowProviderTypeFallback)

			sub, err := computeInfraPlan(ctx, provider, g.specs, scopedCurrent)
			if err != nil {
				return nil, fmt.Errorf("provider %q: compute plan: %w", ref, err)
			}
			return sub.Actions, nil
		}()
		if err != nil {
			return interfaces.IaCPlan{}, err
		}
		allActions = append(allActions, groupActions...)
	}

	return interfaces.IaCPlan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Actions:   allActions,
		CreatedAt: time.Now().UTC(),
	}, nil
}
