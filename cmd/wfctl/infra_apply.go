package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// hasInfraModules reports whether cfgFile contains any modules with the new
// infra.* type prefix. Used by runInfraApply to select the dispatch path:
// direct IaCProvider path for infra.* configs, pipeline path for legacy
// platform.* configs.
func hasInfraModules(cfgFile string) bool {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return false
	}
	for _, m := range cfg.Modules {
		if strings.HasPrefix(m.Type, "infra.") {
			return true
		}
	}
	return false
}

// applyInfraModules applies all infra.* modules in cfgFile by directly loading
// each referenced IaCProvider plugin, computing a diff plan, and executing it.
// Modules are grouped by their provider: reference; each unique provider is
// loaded once and applied in declaration order. The envName parameter (may be
// empty) triggers per-environment config resolution.
//
// This is the new dispatch path used when the config contains infra.* modules
// instead of the legacy platform.* + pipelines.apply pipeline path.
func applyInfraModules(ctx context.Context, cfgFile, envName string) error {
	// Resolve specs (env overrides applied when envName is set).
	specs, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("parse infra resource specs: %w", err)
	}

	// Keep only infra.* specs for the direct path.
	var infraSpecs []interfaces.ResourceSpec
	for _, s := range specs {
		if strings.HasPrefix(s.Type, "infra.") {
			infraSpecs = append(infraSpecs, s)
		}
	}
	if len(infraSpecs) == 0 {
		fmt.Println("No infra.* modules to apply.")
		return nil
	}

	// Load full config to resolve iac.provider module definitions.
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build a lookup table of iac.provider module name → (providerType, providerCfg).
	type providerDef struct {
		provType string
		provCfg  map[string]any
	}
	providerDefs := map[string]providerDef{}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		expanded := config.ExpandEnvInMap(m.Config)
		pt, _ := expanded["provider"].(string)
		providerDefs[m.Name] = providerDef{provType: pt, provCfg: expanded}
	}

	// Group infra specs by iac.provider module name, preserving declaration order.
	type provGroup struct {
		moduleRef string
		provType  string
		provCfg   map[string]any
		specs     []interfaces.ResourceSpec
	}
	groups := map[string]*provGroup{}
	var groupOrder []string

	for _, spec := range infraSpecs {
		moduleRef, _ := spec.Config["provider"].(string)
		if moduleRef == "" {
			return fmt.Errorf("infra module %q (%s): missing required 'provider' field", spec.Name, spec.Type)
		}
		if _, exists := groups[moduleRef]; !exists {
			def, ok := providerDefs[moduleRef]
			if !ok {
				return fmt.Errorf("infra module %q references provider %q which is not declared as an iac.provider module", spec.Name, moduleRef)
			}
			if def.provType == "" {
				return fmt.Errorf("provider module %q has no 'provider' type configured", moduleRef)
			}
			groups[moduleRef] = &provGroup{moduleRef: moduleRef, provType: def.provType, provCfg: def.provCfg}
			groupOrder = append(groupOrder, moduleRef)
		}
		groups[moduleRef].specs = append(groups[moduleRef].specs, spec)
	}

	// Load current state once; each provider call filters to its own resources.
	current := loadCurrentState(cfgFile)

	// Apply each provider group in declaration order.
	for _, moduleRef := range groupOrder {
		g := groups[moduleRef]
		fmt.Printf("Applying %d resource(s) via provider %q (%s)...\n", len(g.specs), moduleRef, g.provType)
		if err := applyWithProvider(ctx, g.provType, g.provCfg, g.specs, current); err != nil {
			return fmt.Errorf("provider %q (%s): %w", moduleRef, g.provType, err)
		}
	}
	return nil
}

// applyWithProvider loads the named IaCProvider plugin, computes a diff plan
// for the given specs against the current state, and executes it via Apply.
// Returns nil when there are no changes to apply.
func applyWithProvider(ctx context.Context, providerType string, providerCfg map[string]any, specs []interfaces.ResourceSpec, current []interfaces.ResourceState) error {
	provider, closer, err := resolveIaCProvider(ctx, providerType, providerCfg)
	if err != nil {
		return fmt.Errorf("load provider: %w", err)
	}
	if closer != nil {
		defer closer.Close() //nolint:errcheck
	}

	// Narrow current state to only resources in this provider's spec set to
	// avoid spurious deletes of resources managed by other providers.
	specNames := make(map[string]struct{}, len(specs))
	for _, s := range specs {
		specNames[s.Name] = struct{}{}
	}
	var provCurrent []interfaces.ResourceState
	for i := range current {
		if _, ok := specNames[current[i].Name]; ok {
			provCurrent = append(provCurrent, current[i])
		}
	}

	// Compute the diff plan locally (provider-agnostic).
	plan, err := platform.ComputePlan(specs, provCurrent)
	if err != nil {
		return fmt.Errorf("compute plan: %w", err)
	}
	if len(plan.Actions) == 0 {
		fmt.Println("  No changes — infrastructure is up-to-date.")
		return nil
	}

	fmt.Printf("  Plan: %d action(s) to execute.\n", len(plan.Actions))
	result, err := provider.Apply(ctx, &plan)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	if result != nil {
		for _, r := range result.Resources {
			fmt.Printf("  ✓ %s (%s)\n", r.Name, r.Type)
		}
		if len(result.Errors) > 0 {
			msgs := make([]string, 0, len(result.Errors))
			for _, ae := range result.Errors {
				msgs = append(msgs, fmt.Sprintf("%s/%s: %s", ae.Action, ae.Resource, ae.Error))
			}
			return fmt.Errorf("%d resource(s) failed: %s", len(result.Errors), strings.Join(msgs, "; "))
		}
	}
	return nil
}
