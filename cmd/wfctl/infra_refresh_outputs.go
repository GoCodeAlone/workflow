package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/refreshoutputs"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// runInfraRefreshOutputs reads live Outputs from each provider for the
// resources already in state and persists any field-level changes back to
// the state backend. The contract is strictly read-only at the cloud level —
// no Update or Replace is ever invoked. See iac/refreshoutputs/refresh.go
// for the helper this command wraps.
//
// When the resolved config has no usable iac.provider module for the
// requested env, the literal error
//
//	refresh-outputs: provider not configured for env "<env>"
//
// is returned so that operators can distinguish a misconfigured workflow
// from a transient cloud-side failure. T2.7 asserts against this exact line.
func runInfraRefreshOutputs(args []string) error {
	fs := flag.NewFlagSet("infra refresh-outputs", flag.ContinueOnError)
	// Direct help/usage to stdout so `--help` is pipeable and the CI
	// runtime-launch validator (T2.7) can capture it via captureStdout.
	fs.SetOutput(os.Stdout)
	var configFile, envName string
	var concurrency int
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name (resolves per-module overrides)")
	fs.StringVar(&envName, "e", "", "Environment name (short for --env)")
	// Default mirrors refreshoutputs.defaultConcurrency. Refresh still treats
	// values < 1 as "use default" so callers passing an explicit 0 (or
	// negative) keep working; this default just makes `--help` honest.
	fs.IntVar(&concurrency, "concurrency", 8, "Maximum concurrent Read calls")
	var pluginDirFlag string
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prevInfraPluginDir := currentInfraPluginDir
	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = prevInfraPluginDir }()

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	ctx := context.Background()

	providerDefs, err := discoverIaCProvidersForRefresh(cfgFile, envName)
	if err != nil {
		return err
	}
	if len(providerDefs) == 0 {
		return fmt.Errorf("refresh-outputs: provider not configured for env %q", envName)
	}

	states, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("load current state: %w", err)
	}
	if len(states) == 0 {
		fmt.Println("Refresh: no state to refresh.")
		return nil
	}

	store, err := resolveStateStore(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}

	return refreshOutputsAcrossProviders(ctx, providerDefs, states, store, concurrency, os.Stdout)
}

// refreshOutputsProviderDef captures everything refresh-outputs needs to
// load and call a single iac.provider module: its module name, the provider
// type, and the env-resolved config.
type refreshOutputsProviderDef struct {
	moduleName string
	provType   string
	provCfg    map[string]any
}

// discoverIaCProvidersForRefresh walks cfgFile's modules and returns one
// providerDef per iac.provider module that resolves successfully for envName
// (modules disabled with `environments: { <env>: ~ }` are skipped). When
// envName is "", the top-level module config is used as-is. The returned
// slice preserves declaration order.
func discoverIaCProvidersForRefresh(cfgFile, envName string) ([]refreshOutputsProviderDef, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	var defs []refreshOutputsProviderDef
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				// Disabled for this env via null override; not "no provider"
				// for the env unless every iac.provider is disabled.
				continue
			}
			modCfg = config.ExpandEnvInMap(resolved.Config)
		} else {
			modCfg = config.ExpandEnvInMap(m.Config)
		}
		pt, _ := modCfg["provider"].(string)
		if pt == "" {
			fmt.Fprintf(os.Stderr, "warning: iac.provider %q has no 'provider' field; skipping\n", m.Name)
			continue
		}
		defs = append(defs, refreshOutputsProviderDef{
			moduleName: m.Name,
			provType:   pt,
			provCfg:    modCfg,
		})
	}
	return defs, nil
}

// refreshOutputsAcrossProviders groups state entries by which iac.provider
// module owns them, calls refreshoutputs.Refresh for each group, and
// persists any state entries whose Outputs changed. It loads each provider
// at most once.
//
// State entries with a non-empty ProviderRef are matched to the iac.provider
// module of the same name. State entries without a ProviderRef fall back to
// the iac.provider module whose provider type matches state.Provider, but
// only when exactly one such module exists; otherwise the fallback is
// ambiguous and the entry is skipped with a warning rather than refreshed
// against the wrong provider.
func refreshOutputsAcrossProviders(
	ctx context.Context,
	providerDefs []refreshOutputsProviderDef,
	states []interfaces.ResourceState,
	store infraStateStore,
	concurrency int,
	stdout io.Writer,
) error {
	defByName := make(map[string]refreshOutputsProviderDef, len(providerDefs))
	defsByType := make(map[string][]string)
	for _, d := range providerDefs {
		defByName[d.moduleName] = d
		defsByType[d.provType] = append(defsByType[d.provType], d.moduleName)
	}

	groups := make(map[string][]int) // moduleName → indices into states
	var groupOrder []string
	for i := range states {
		s := &states[i]
		moduleName := s.ProviderRef
		if moduleName == "" && s.Provider != "" {
			candidates := defsByType[s.Provider]
			if len(candidates) == 1 {
				moduleName = candidates[0]
			}
		}
		if moduleName == "" {
			fmt.Fprintf(stdout, "Refresh: skipping %q — cannot resolve owning provider (provider_ref=%q, provider=%q)\n",
				s.Name, s.ProviderRef, s.Provider)
			continue
		}
		if _, ok := defByName[moduleName]; !ok {
			fmt.Fprintf(stdout, "Refresh: skipping %q — provider module %q not declared in config\n", s.Name, moduleName)
			continue
		}
		if _, exists := groups[moduleName]; !exists {
			groupOrder = append(groupOrder, moduleName)
		}
		groups[moduleName] = append(groups[moduleName], i)
	}

	updated := 0
	for _, moduleName := range groupOrder {
		def := defByName[moduleName]
		idxs := groups[moduleName]
		groupStates := make([]interfaces.ResourceState, len(idxs))
		for j, idx := range idxs {
			groupStates[j] = states[idx]
		}
		fmt.Fprintf(stdout, "Refresh: reading %d resource(s) via provider %q (%s)...\n",
			len(groupStates), moduleName, def.provType)
		if err := refreshOneProviderGroup(ctx, def, idxs, groupStates, states, store, concurrency, &updated, stdout); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Refresh: complete — %d resource(s) updated.\n", updated)
	return nil
}

// refreshOneProviderGroup loads a single provider, refreshes its state
// subset, and persists any entries whose Outputs changed. Extracted so the
// closer is `defer`-closed for panic safety and to keep
// refreshOutputsAcrossProviders readable.
func refreshOneProviderGroup(
	ctx context.Context,
	def refreshOutputsProviderDef,
	idxs []int,
	groupStates []interfaces.ResourceState,
	states []interfaces.ResourceState,
	store infraStateStore,
	concurrency int,
	updated *int,
	stdout io.Writer,
) error {
	provider, closer, err := resolveIaCProvider(ctx, def.provType, def.provCfg)
	if err != nil {
		return fmt.Errorf("provider %q (%s): load provider: %w", def.moduleName, def.provType, err)
	}
	if closer != nil {
		defer func() {
			if cerr := closer.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", def.provType, cerr)
			}
		}()
	}
	refreshed, err := refreshoutputs.Refresh(ctx, provider, groupStates, refreshoutputs.Options{Concurrency: concurrency})
	if err != nil {
		return fmt.Errorf("provider %q: %w", def.moduleName, err)
	}
	for j, idx := range idxs {
		fresh := refreshed[j]
		// refreshoutputs.Refresh has already done the deep-equality check:
		// it preserves the original Outputs map (same header) when nothing
		// changed and only allocates a fresh map when fields differ. A
		// pointer-equality check on the map header is therefore sufficient
		// — and avoids re-walking nested slices/maps that real cloud
		// outputs carry. nil ↔ nil compares equal (both .Pointer() == 0)
		// which is the correct unchanged-case answer.
		if reflect.ValueOf(states[idx].Outputs).Pointer() == reflect.ValueOf(fresh.Outputs).Pointer() {
			continue
		}
		states[idx] = fresh
		// Build a ResourceOutput so persistResourceWithSecretRouting (read mode)
		// can sanitize sensitive keys from the refreshed cloud outputs. Use
		// driver.SensitiveKeys() as the per-call Sensitive map source for
		// refresh: refresh paths don't have a per-call Sensitive declaration
		// from a driver method, but SensitiveKeys() is the static driver
		// declaration that approximates the same intent.
		ro := interfaces.ResourceOutput{
			Name: fresh.Name, Type: fresh.Type, ProviderID: fresh.ProviderID,
			Outputs: fresh.Outputs,
		}
		if drv, derr := provider.ResourceDriver(fresh.Type); derr == nil && drv != nil {
			sk := drv.SensitiveKeys()
			if len(sk) > 0 {
				ro.Sensitive = make(map[string]bool, len(sk))
				for _, k := range sk {
					ro.Sensitive[k] = true
				}
			}
		}
		if _, err := persistResourceWithSecretRouting(ctx, store, nil, nil, fresh, ro, persistModeRead); err != nil {
			return fmt.Errorf("provider %q: persist refreshed %q: %w", def.moduleName, fresh.Name, err)
		}
		*updated++
		fmt.Fprintf(stdout, "Refresh: updated %s\n", fresh.Name)
	}
	return nil
}
