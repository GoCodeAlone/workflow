package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// statusInfraModules queries the live status of all resources tracked in the
// state store and prints a human-readable summary. It is the direct-path
// implementation used for infra.* module configs.
func statusInfraModules(ctx context.Context, cfgFile, envName string) error {
	store, err := resolveStateStore(cfgFile)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	states, err := store.ListResources(ctx)
	if err != nil {
		return fmt.Errorf("list state: %w", err)
	}
	if len(states) == 0 {
		fmt.Println("No resources tracked in state.")
		return nil
	}

	groups, groupOrder := groupStatesByProvider(states, cfgFile, envName)

	for _, moduleRef := range groupOrder {
		g := groups[moduleRef]
		provider, closer, err := resolveIaCProvider(ctx, g.provType, g.provCfg)
		if err != nil {
			fmt.Printf("WARNING: load provider %q: %v\n", moduleRef, err)
			continue
		}
		if closer != nil {
			defer closer.Close() //nolint:errcheck
		}

		statuses, err := provider.Status(ctx, g.refs)
		if err != nil {
			fmt.Printf("WARNING: status for provider %q: %v\n", moduleRef, err)
			continue
		}

		for _, s := range statuses {
			fmt.Printf("  %-40s  %-20s  %s\n", s.Name, s.Type, s.Status)
		}
		if len(statuses) == 0 {
			// Print raw state info when provider returns nothing.
			for _, ref := range g.refs {
				fmt.Printf("  %-40s  %-20s  (unknown — provider returned no status)\n", ref.Name, ref.Type)
			}
		}
	}
	return nil
}

// driftInfraModules detects configuration drift for all resources tracked in
// the state store and prints any differences. It is the direct-path
// implementation used for infra.* module configs.
func driftInfraModules(ctx context.Context, cfgFile, envName string) error {
	store, err := resolveStateStore(cfgFile)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	states, err := store.ListResources(ctx)
	if err != nil {
		return fmt.Errorf("list state: %w", err)
	}
	if len(states) == 0 {
		fmt.Println("No resources tracked in state.")
		return nil
	}

	groups, groupOrder := groupStatesByProvider(states, cfgFile, envName)

	driftFound := false
	for _, moduleRef := range groupOrder {
		g := groups[moduleRef]
		provider, closer, err := resolveIaCProvider(ctx, g.provType, g.provCfg)
		if err != nil {
			fmt.Printf("WARNING: load provider %q: %v\n", moduleRef, err)
			continue
		}
		if closer != nil {
			defer closer.Close() //nolint:errcheck
		}

		results, err := provider.DetectDrift(ctx, g.refs)
		if err != nil {
			fmt.Printf("WARNING: drift detection for provider %q: %v\n", moduleRef, err)
			continue
		}

		for _, d := range results {
			if d.Drifted {
				driftFound = true
				fmt.Printf("  DRIFT  %s (%s)\n", d.Name, d.Type)
				for k, v := range d.Expected {
					actual := d.Actual[k]
					if fmt.Sprintf("%v", v) != fmt.Sprintf("%v", actual) {
						fmt.Printf("    %s: expected=%v  actual=%v\n", k, v, actual)
					}
				}
			} else {
				fmt.Printf("  OK     %s (%s)\n", d.Name, d.Type)
			}
		}
		if len(results) == 0 {
			for _, ref := range g.refs {
				fmt.Printf("  OK     %s (%s)  (provider returned no drift result)\n", ref.Name, ref.Type)
			}
		}
	}

	if driftFound {
		return fmt.Errorf("drift detected — run 'wfctl infra apply' to reconcile")
	}
	return nil
}

// ── groupStatesByProvider ──────────────────────────────────────────────────────

// providerGroup collects ResourceRefs that belong to the same IaCProvider.
type providerGroup struct {
	provType string
	provCfg  map[string]any
	refs     []interfaces.ResourceRef
}

// groupStatesByProvider partitions state records into provider groups using
// the same logic as destroyInfraModules. Returns a map + stable ordering slice.
func groupStatesByProvider(states []interfaces.ResourceState, cfgFile, envName string) (map[string]*providerGroup, []string) {
	// Parse specs to get per-resource provider references.
	specs, _ := parseInfraResourceSpecsForEnv(cfgFile, envName)
	specByName := make(map[string]interfaces.ResourceSpec, len(specs))
	for _, s := range specs {
		specByName[s.Name] = s
	}

	providerDefs := buildProviderDefs(cfgFile, envName)
	groups := map[string]*providerGroup{}
	var groupOrder []string

	for _, st := range states {
		moduleRef := ""
		if spec, ok := specByName[st.Name]; ok {
			moduleRef, _ = spec.Config["provider"].(string)
		}
		if moduleRef == "" {
			moduleRef = "__provider__:" + st.Provider
		}

		if _, exists := groups[moduleRef]; !exists {
			var pType string
			var pCfg map[string]any

			if defVal, ok := providerDefs[moduleRef]; ok {
				pType, pCfg = defVal.provType, defVal.provCfg
			} else if _, rest, found := strings.Cut(moduleRef, "__provider__:"); found {
				pType = rest
			}

			if pType == "" {
				fmt.Printf("WARNING: cannot determine provider for resource %q — skipping\n", st.Name)
				continue
			}
			groups[moduleRef] = &providerGroup{provType: pType, provCfg: pCfg}
			groupOrder = append(groupOrder, moduleRef)
		}
		groups[moduleRef].refs = append(groups[moduleRef].refs, interfaces.ResourceRef{
			Name:       st.Name,
			Type:       st.Type,
			ProviderID: st.ProviderID,
		})
	}
	return groups, groupOrder
}
