package main

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// providerDef is the resolved configuration for a single iac.provider module.
// Shared between the plan and apply dispatch paths so that both operate on the
// same resolved shape; keeping them in sync eliminates silent drift in
// env-var resolution, disabled-provider handling, and providerTypeCounts.
type providerDef struct {
	provType string
	provCfg  map[string]any
}

// specGroup groups a set of resource specs that share the same iac.provider
// module reference. Replaces the functionally-identical local planGroup /
// provGroup types that previously existed in infra_plan_provider.go and
// infra_apply.go respectively.
type specGroup struct {
	moduleRef string
	provType  string
	provCfg   map[string]any
	specs     []interfaces.ResourceSpec
}

// resolveProviderDefs walks cfg.Modules, filters iac.provider modules, expands
// env vars in their configs, and returns three maps:
//   - defs: module name → providerDef (type + resolved config)
//   - typeCounts: provider type string → how many iac.provider modules declare it
//     (used by filterCurrentStateForProvider's allowProviderTypeFallback heuristic)
//   - disabled: module names explicitly disabled for envName via a null
//     environments[envName] entry (non-nil set, possibly empty)
//
// resolveProviderDefs never returns an error; all failure modes (missing env
// entries, nil configs) are silent continuations matching the pre-refactor
// behaviour of both callers.
func resolveProviderDefs(cfg *config.WorkflowConfig, envName string) (defs map[string]providerDef, typeCounts map[string]int, disabled map[string]struct{}) {
	defs = map[string]providerDef{}
	typeCounts = map[string]int{}
	disabled = map[string]struct{}{}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				disabled[m.Name] = struct{}{}
				continue
			}
			modCfg = config.ExpandEnvInMap(resolved.Config)
		} else {
			modCfg = config.ExpandEnvInMap(m.Config)
		}
		pt, _ := modCfg["provider"].(string)
		defs[m.Name] = providerDef{provType: pt, provCfg: modCfg}
		if pt != "" {
			typeCounts[pt]++
		}
	}
	return
}

// groupSpecsByProviderRef walks specs, reads each spec's `provider` config
// field, and groups specs into specGroup values keyed by provider module
// reference name. The returned groupOrder slice contains the unique moduleRef
// keys in first-reference-in-specs order (i.e., the order in which each
// group's first owning resource appears in specs, not iac.provider declaration
// order). This preserves the stable ordering that both plan and apply depend on.
//
// An error is returned when:
//   - a spec's `provider` field is absent or empty
//   - the referenced module name is in the disabled set
//   - the referenced module name is not in defs at all
//   - the referenced module's provType is empty (provider not configured)
func groupSpecsByProviderRef(specs []interfaces.ResourceSpec, defs map[string]providerDef, disabled map[string]struct{}, envName string) (groupOrder []string, groups map[string]*specGroup, err error) {
	groups = map[string]*specGroup{}
	for _, spec := range specs {
		moduleRef, _ := spec.Config["provider"].(string)
		if moduleRef == "" {
			return nil, nil, fmt.Errorf("infra module %q (%s): missing required 'provider' field", spec.Name, spec.Type)
		}
		if _, exists := groups[moduleRef]; !exists {
			def, ok := defs[moduleRef]
			if !ok {
				if _, isDisabled := disabled[moduleRef]; isDisabled {
					return nil, nil, fmt.Errorf("infra module %q references provider %q which is disabled for environment %q", spec.Name, moduleRef, envName)
				}
				return nil, nil, fmt.Errorf("infra module %q references provider %q which is not declared as an iac.provider module", spec.Name, moduleRef)
			}
			if def.provType == "" {
				return nil, nil, fmt.Errorf("provider module %q has no 'provider' type configured", moduleRef)
			}
			groups[moduleRef] = &specGroup{moduleRef: moduleRef, provType: def.provType, provCfg: def.provCfg}
			groupOrder = append(groupOrder, moduleRef)
		}
		groups[moduleRef].specs = append(groups[moduleRef].specs, spec)
	}
	return groupOrder, groups, nil
}
