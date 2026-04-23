package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// destroyInfraModules destroys all resources currently tracked in the state
// store for cfgFile. It groups state records by the provider type declared in
// the iac.provider module, calls provider.Destroy for each group, and removes
// the destroyed records from state on success.
//
// This is the direct-path destroy used when the config contains infra.* modules.
// The legacy platform.* path continues to run pipelines.destroy via runPipelineRun.
func destroyInfraModules(ctx context.Context, cfgFile, envName string) error { //nolint:cyclop
	store, err := resolveStateStore(cfgFile)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}

	states, err := store.ListResources(ctx)
	if err != nil {
		return fmt.Errorf("list state: %w", err)
	}
	if len(states) == 0 {
		fmt.Println("No resources tracked in state — nothing to destroy.")
		return nil
	}

	// Load the config to find provider definitions.
	specs, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("parse infra resource specs: %w", err)
	}

	// Build a map of iac.provider module configs by name so we can resolve the
	// right provider for each state record.
	providerDefs := buildProviderDefs(cfgFile, envName)

	// Derive each state record's provider module reference from the spec's
	// "provider" field. If the spec is not found (resource removed from config
	// since last apply), fall back to an empty ref.
	specByName := make(map[string]interfaces.ResourceSpec, len(specs))
	for _, s := range specs {
		specByName[s.Name] = s
	}

	// Group state records by provider module ref (or by inferred provider type).
	type group struct {
		provType string
		provCfg  map[string]any
		refs     []interfaces.ResourceRef
		names    []string // state IDs to delete on success
	}
	groups := map[string]*group{}
	var groupOrder []string

	for i := range states {
		st := &states[i]
		// Try to find the provider ref from the spec.
		moduleRef := ""
		if spec, ok := specByName[st.Name]; ok {
			moduleRef, _ = spec.Config["provider"].(string)
		}

		// If not in spec (orphaned resource), try to infer from state's Provider field.
		// Provider field may hold the providerType string (e.g. "digitalocean").
		if moduleRef == "" {
			// Use Provider as a fallback key.
			moduleRef = "__provider__:" + st.Provider
		}

		if _, exists := groups[moduleRef]; !exists {
			var pType string
			var pCfg map[string]any

			if defVal, ok := providerDefs[moduleRef]; ok {
				pType, pCfg = defVal.provType, defVal.provCfg
			} else if rest, found := strings.CutPrefix(moduleRef, "__provider__:"); found {
				// Orphaned: provider type was stored in st.Provider
				pType = rest
			}

			if pType == "" {
				fmt.Printf("WARNING: cannot determine provider for resource %q (state provider=%q) — skipping\n", st.Name, st.Provider)
				continue
			}

			groups[moduleRef] = &group{provType: pType, provCfg: pCfg}
			groupOrder = append(groupOrder, moduleRef)
		}
		g := groups[moduleRef]
		g.refs = append(g.refs, interfaces.ResourceRef{
			Name:       st.Name,
			Type:       st.Type,
			ProviderID: st.ProviderID,
		})
		g.names = append(g.names, st.Name)
	}

	// Destroy each provider group in order.
	destroyGroup := func(moduleRef string, g *group) error {
		provider, closer, err := resolveIaCProvider(ctx, g.provType, g.provCfg)
		if err != nil {
			return fmt.Errorf("load provider %q: %w", moduleRef, err)
		}
		if closer != nil {
			defer closer.Close() //nolint:errcheck
		}

		fmt.Printf("Destroying %d resource(s) via provider %q (%s)...\n", len(g.refs), moduleRef, g.provType)
		result, err := provider.Destroy(ctx, g.refs)
		if err != nil {
			return fmt.Errorf("destroy via provider %q: %w", moduleRef, err)
		}
		if result != nil {
			for _, name := range result.Destroyed {
				fmt.Printf("  ✓ destroyed %s\n", name)
			}
			if len(result.Errors) > 0 {
				msgs := make([]string, 0, len(result.Errors))
				for _, ae := range result.Errors {
					msgs = append(msgs, fmt.Sprintf("%s/%s: %s", ae.Action, ae.Resource, ae.Error))
				}
				return fmt.Errorf("%d resource(s) failed to destroy: %s", len(result.Errors), strings.Join(msgs, "; "))
			}
		}

		// Remove state records for destroyed resources.
		for _, name := range g.names {
			if delErr := store.DeleteResource(ctx, name); delErr != nil {
				fmt.Printf("  WARNING: failed to remove state for %q: %v\n", name, delErr)
			}
		}
		return nil
	}
	for _, moduleRef := range groupOrder {
		if err := destroyGroup(moduleRef, groups[moduleRef]); err != nil {
			return err
		}
	}
	return nil
}

// iacProviderDef holds the resolved type and config for an iac.provider module.
type iacProviderDef struct {
	provType string
	provCfg  map[string]any
}

// buildProviderDefs returns a map of iac.provider module name → iacProviderDef
// from cfgFile with env overrides applied when envName is non-empty.
// This helper is shared by destroyInfraModules and similar direct-path commands.
func buildProviderDefs(cfgFile, envName string) map[string]iacProviderDef {
	defs := map[string]iacProviderDef{}

	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return defs
	}

	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				continue
			}
			modCfg = config.ExpandEnvInMap(resolved.Config)
		} else {
			modCfg = config.ExpandEnvInMap(m.Config)
		}
		pt, _ := modCfg["provider"].(string)
		defs[m.Name] = iacProviderDef{provType: pt, provCfg: modCfg}
	}
	return defs
}
