package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// infraApplyTroubleshootTimeout is the budget for a Troubleshoot call when
// infra apply fails. Kept separate so tests can override it.
var infraApplyTroubleshootTimeout = 30 * time.Second

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

// hasPlatformModules reports whether cfgFile contains any modules with the legacy
// platform.* type prefix.
func hasPlatformModules(cfgFile string) bool {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return false
	}
	for _, m := range cfg.Modules {
		if strings.HasPrefix(m.Type, "platform.") {
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
func applyInfraModules(ctx context.Context, cfgFile, envName string) error { //nolint:cyclop
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
	if err := validateUniqueInfraResourceNames(infraSpecs); err != nil {
		return err
	}

	// Load full config to resolve iac.provider module definitions.
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build a lookup table of iac.provider module name → (providerType, providerCfg).
	// Also track which providers are explicitly disabled for this env so we can
	// emit a precise error if an infra module references one.
	type providerDef struct {
		provType string
		provCfg  map[string]any
	}
	providerDefs := map[string]providerDef{}
	providerTypeCounts := map[string]int{}
	disabledProviders := map[string]struct{}{} // providers with environments[envName]: null
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		// Apply per-env overrides when envName is set so that provider credentials
		// or regions declared under environments: are respected.
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				disabledProviders[m.Name] = struct{}{} // disabled via null env entry
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
				if _, disabled := disabledProviders[moduleRef]; disabled {
					return fmt.Errorf("infra module %q references provider %q which is disabled for environment %q", spec.Name, moduleRef, envName)
				}
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

	// Load current state once; nil on first run is valid (no prior state).
	current, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("load current state: %w", err)
	}

	// Resolve the state store once. A missing iac.state module resolves to a
	// noop store, but a configured backend that cannot be opened is fatal:
	// applying cloud mutations without durable state risks losing provider_ref
	// ownership metadata.
	store, storeErr := resolveStateStore(cfgFile, envName)
	if storeErr != nil {
		return fmt.Errorf("open state store: %w", storeErr)
	}

	// Apply each provider group in declaration order.
	applyGroup := func(moduleRef string, g *provGroup) error {
		fmt.Printf("Applying %d resource(s) via provider %q (%s)...\n", len(g.specs), moduleRef, g.provType)
		provider, closer, err := resolveIaCProvider(ctx, g.provType, g.provCfg)
		if err != nil {
			return fmt.Errorf("provider %q (%s): load provider: %w", moduleRef, g.provType, err)
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
		scopedCurrent := filterCurrentStateForProvider(current, g.provType, moduleRef, g.specs, allowProviderTypeFallback)
		return applyWithProviderAndStore(ctx, provider, g.provType, g.specs, scopedCurrent, store, os.Stderr, envName)
	}
	for _, moduleRef := range groupOrder {
		if err := applyGroup(moduleRef, groups[moduleRef]); err != nil {
			return fmt.Errorf("provider %q: %w", moduleRef, err)
		}
	}
	return nil
}

func filterCurrentStateForProvider(current []interfaces.ResourceState, providerType, moduleRef string, specs []interfaces.ResourceSpec, allowProviderTypeFallback bool) []interfaces.ResourceState {
	if len(current) == 0 {
		return nil
	}
	scoped := make([]interfaces.ResourceState, 0, len(current))
	for i := range current {
		st := current[i]
		if stateProviderRef := resourceStateProviderRef(st); stateProviderRef != "" {
			if stateProviderRef == moduleRef {
				scoped = append(scoped, st)
				continue
			}
			continue
		}
		if st.Provider == providerType && allowProviderTypeFallback {
			scoped = append(scoped, st)
			continue
		}
		if st.Provider == "" && allowProviderTypeFallback && stateNameInSpecs(st.Name, specs) {
			scoped = append(scoped, st)
		}
	}
	return scoped
}

func stateNameInSpecs(name string, specs []interfaces.ResourceSpec) bool {
	for i := range specs {
		if specs[i].Name == name {
			return true
		}
	}
	return false
}

func validateUniqueInfraResourceNames(specs []interfaces.ResourceSpec) error {
	seen := make(map[string]interfaces.ResourceSpec, len(specs))
	for _, spec := range specs {
		if prev, exists := seen[spec.Name]; exists {
			prevProvider := resourceSpecProviderRef(prev)
			provider := resourceSpecProviderRef(spec)
			if prevProvider != provider || prev.Type != spec.Type {
				return fmt.Errorf("infra resource name %q is used by both %s/provider %q and %s/provider %q; state identity is name-based, so resource names must be unique across provider groups", spec.Name, prev.Type, prevProvider, spec.Type, provider)
			}
			return fmt.Errorf("infra resource name %q is declared more than once", spec.Name)
		}
		seen[spec.Name] = spec
	}
	return nil
}

func resourceStateProviderRef(st interfaces.ResourceState) string {
	if st.ProviderRef != "" {
		return st.ProviderRef
	}
	if st.AppliedConfig == nil {
		return ""
	}
	providerRef, _ := st.AppliedConfig["provider"].(string)
	return providerRef
}

func resourceSpecProviderRef(spec interfaces.ResourceSpec) string {
	if spec.Config == nil {
		return ""
	}
	providerRef, _ := spec.Config["provider"].(string)
	return providerRef
}

// applyWithProviderAndStore computes a diff plan for the given specs against
// the current state and executes it via provider.Apply. On success, each
// provisioned resource is persisted to store. Save failures abort the command
// so callers cannot miss a successful cloud mutation whose state was not
// recorded. Deleted resources are removed from store after a successful destroy
// action.
//
// providerType is used only as a label when constructing ResourceState records.
// Callers pass a nil store (or noopStateStore) when state persistence is not
// required. w receives diagnostic output; callers typically pass os.Stderr but
// tests may supply a bytes.Buffer to capture and assert the output.
// envName labels the failure step-summary output (e.g. "staging", "prod");
// pass empty string when not running in a CI context or when env metadata is
// unavailable.
func applyWithProviderAndStore(ctx context.Context, provider interfaces.IaCProvider, providerType string, specs []interfaces.ResourceSpec, current []interfaces.ResourceState, store infraStateStore, w io.Writer, envName string) error {
	if store == nil {
		store = &noopStateStore{}
	}

	// Resolve abstract sizing tiers into concrete provider-specific values
	// (e.g. Size: "m" → instance_type: "s-1vcpu-2gb") for each spec that
	// declares an abstract Size tier. Provider-specific slugs (e.g.
	// "db-s-1vcpu-1gb") are passed through as-is to avoid double-resolution.
	// The resolved values are merged into spec.Config so that plan output and
	// apply are always in sync.
	for i := range specs {
		spec := &specs[i]
		if spec.Size == "" || !isAbstractSize(spec.Size) {
			continue
		}
		sizing, err := provider.ResolveSizing(spec.Type, spec.Size, spec.Hints)
		if err != nil {
			return fmt.Errorf("%s/%s: resolve sizing: %w", spec.Type, spec.Name, err)
		}
		if sizing != nil {
			if spec.Config == nil {
				spec.Config = map[string]any{}
			}
			spec.Config["instance_type"] = sizing.InstanceType
			for k, v := range sizing.Specs {
				spec.Config[k] = v
			}
		}
	}

	// Pass the caller-provided current state to ComputePlan so resources that
	// were previously provisioned but are no longer desired generate deletes.
	// Multi-provider callers must pass only the state owned by this provider
	// group; applyInfraModules does that before invoking this helper.

	var err error
	current, err = adoptExistingResources(ctx, provider, providerType, specs, current, store)
	if err != nil {
		return err
	}

	// Compute the diff plan locally (provider-agnostic).
	plan, err := platform.ComputePlan(specs, current)
	if err != nil {
		return fmt.Errorf("compute plan: %w", err)
	}
	if len(plan.Actions) == 0 {
		fmt.Println("  No changes — infrastructure is up-to-date.")
		return nil
	}

	// Collect delete-action resource names so we can clean up state afterward.
	deleteNames := make(map[string]struct{})
	for i := range plan.Actions {
		if plan.Actions[i].Action == "delete" {
			deleteNames[plan.Actions[i].Resource.Name] = struct{}{}
		}
	}

	// Soft-warn if any update/delete action targets a resource whose ProviderID
	// does not match the driver's declared format. The driver may self-heal, so
	// we log and continue rather than blocking the apply.
	validateInputProviderIDs(provider, &plan)
	fmt.Printf("  Plan: %d action(s) to execute.\n", len(plan.Actions))
	result, err := provider.Apply(ctx, &plan)
	if err != nil {
		// Derive the most specific resource ref we can for troubleshooting.
		// Single-action plans give us an exact resource; multi-resource plans
		// fall back to the first spec so the troubleshooter has at least a name.
		ref := interfaces.ResourceRef{}
		if len(plan.Actions) == 1 {
			ref.Name = plan.Actions[0].Resource.Name
			ref.Type = plan.Actions[0].Resource.Type
		} else if len(specs) == 1 {
			ref.Name = specs[0].Name
			ref.Type = specs[0].Type
		}
		em := detectCIProvider()
		// Resolve the ResourceDriver for the failed resource type so
		// troubleshootAfterFailure can reach a Troubleshooter implementation.
		// ref.Type is set when we have a single-action or single-spec plan.
		var diags []interfaces.Diagnostic
		if ref.Type != "" {
			if rd, rdErr := provider.ResourceDriver(ref.Type); rdErr == nil {
				diags = troubleshootAfterFailure(ctx, w, rd, ref, err, infraApplyTroubleshootTimeout, em)
			}
			// If ResourceDriver fails we fall through silently — diagnostics are
			// best-effort and must not mask the original apply error.
		}
		// WriteStepSummary is called unconditionally so a GHA step summary is
		// written even when ref.Type is empty (multi-resource plan) or
		// ResourceDriver is unavailable; diagnostics are empty in those cases
		// but the failure header and root cause are still useful.
		if sumErr := WriteStepSummary(em, SummaryInput{
			Operation:   "apply",
			Env:         envName,
			Resource:    ref.Name,
			Outcome:     "FAILED",
			RootCause:   err.Error(),
			Diagnostics: diags,
		}); sumErr != nil {
			log.Printf("step summary: %v (ignored)", sumErr)
		}
		return fmt.Errorf("apply: %w", err)
	}
	if result != nil {
		// Persist state for every successfully provisioned resource.
		for _, r := range result.Resources {
			fmt.Printf("  ✓ %s (%s)\n", r.Name, r.Type)

			// Hard-fail when the driver returns a malformed ProviderID for a strict
			// format. This prevents corrupt state from reaching the store.
			if err := validateOutputProviderID(provider, providerType, &r); err != nil {
				return fmt.Errorf("state write rejected: %w", err)
			}

			// Find the matching spec to get the applied config.
			var appliedCfg map[string]any
			var providerRef string
			var dependencies []string
			for i := range specs {
				if specs[i].Name == r.Name {
					appliedCfg = specs[i].Config
					providerRef, _ = specs[i].Config["provider"].(string)
					dependencies = append([]string(nil), specs[i].DependsOn...)
					break
				}
			}

			now := time.Now().UTC()
			rs := interfaces.ResourceState{
				ID:            r.Name,
				Name:          r.Name,
				Type:          r.Type,
				Provider:      providerType,
				ProviderRef:   providerRef,
				ProviderID:    r.ProviderID,
				ConfigHash:    configHashMap(appliedCfg),
				AppliedConfig: appliedCfg,
				Outputs:       r.Outputs,
				Dependencies:  dependencies,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if saveErr := store.SaveResource(ctx, rs); saveErr != nil {
				return fmt.Errorf("%s/%s: persist state after apply: %w", r.Type, r.Name, saveErr)
			}
		}

		// Delete state records for resources that were destroyed.
		for name := range deleteNames {
			if delErr := store.DeleteResource(ctx, name); delErr != nil {
				fmt.Printf("  WARNING: failed to remove state for %q: %v\n", name, delErr)
			}
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

func adoptExistingResources(ctx context.Context, provider interfaces.IaCProvider, providerType string, specs []interfaces.ResourceSpec, current []interfaces.ResourceState, store infraStateStore) ([]interfaces.ResourceState, error) {
	if len(specs) == 0 {
		return current, nil
	}
	currentByName := make(map[string]struct{}, len(current))
	for i := range current {
		currentByName[current[i].Name] = struct{}{}
	}

	drivers := make(map[string]interfaces.ResourceDriver)
	for _, spec := range specs {
		if _, exists := currentByName[spec.Name]; exists {
			continue
		}
		builtinAdoptable := hasBuiltInAdoptionRef(spec.Type)
		driver, ok := drivers[spec.Type]
		if !ok {
			var err error
			driver, err = provider.ResourceDriver(spec.Type)
			if err != nil {
				if !builtinAdoptable {
					continue
				}
				return nil, fmt.Errorf("%s/%s: resolve resource driver: %w", spec.Type, spec.Name, err)
			}
			drivers[spec.Type] = driver
		}
		ref, adoptable, err := adoptionRefForSpec(driver, spec)
		if err != nil {
			return nil, err
		}
		if !adoptable {
			continue
		}
		live, err := driver.Read(ctx, ref)
		if err != nil {
			if isIaCNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("%s/%s: read existing resource for adoption: %w", spec.Type, spec.Name, err)
		}
		if live == nil {
			return nil, fmt.Errorf("%s/%s: read existing resource for adoption: driver returned nil resource without error", spec.Type, spec.Name)
		}
		if isNoopStateStore(store) {
			return nil, fmt.Errorf("%s/%s: adoption requires a writable iac.state backend; add an iac.state module before applying existing resources", spec.Type, spec.Name)
		}
		state, err := resourceStateFromLiveOutput(spec, providerType, live)
		if err != nil {
			return nil, err
		}
		if err := validateStateProviderID(provider, providerType, state); err != nil {
			return nil, err
		}
		if saveErr := store.SaveResource(ctx, state); saveErr != nil {
			return nil, fmt.Errorf("%s/%s: persist adopted state: %w", spec.Type, spec.Name, saveErr)
		}
		fmt.Printf("  Adopted existing %s %q (id=%s)\n", spec.Type, spec.Name, state.ProviderID)
		current = append(current, state)
		currentByName[spec.Name] = struct{}{}
	}
	return current, nil
}

func adoptionRefForSpec(driver interfaces.ResourceDriver, spec interfaces.ResourceSpec) (interfaces.ResourceRef, bool, error) {
	if driver == nil {
		return interfaces.ResourceRef{}, false, nil
	}
	if locator, ok := driver.(interfaces.ResourceAdoptionLocator); ok {
		return locator.AdoptionRef(spec)
	}
	if !hasBuiltInAdoptionRef(spec.Type) {
		return interfaces.ResourceRef{}, false, nil
	}
	if spec.Type == "infra.dns" {
		ref := interfaces.ResourceRef{Name: spec.Name, Type: spec.Type}
		if domain, _ := spec.Config["domain"].(string); domain != "" {
			ref.ProviderID = domain
		} else {
			ref.ProviderID = spec.Name
		}
		return ref, true, nil
	}
	return interfaces.ResourceRef{}, false, nil
}

func hasBuiltInAdoptionRef(resourceType string) bool {
	switch resourceType {
	case "infra.dns":
		return true
	default:
		return false
	}
}

func isIaCNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, interfaces.ErrResourceNotFound) {
		return true
	}
	var platformNotFound *platform.ResourceNotFoundError
	return errors.As(err, &platformNotFound)
}

func resourceStateFromLiveOutput(spec interfaces.ResourceSpec, providerType string, live *interfaces.ResourceOutput) (interfaces.ResourceState, error) {
	if live.ProviderID == "" {
		return interfaces.ResourceState{}, fmt.Errorf("%s/%s: live resource returned empty ProviderID; state not persisted", spec.Type, spec.Name)
	}
	appliedConfig := liveConfigFromOutputs(live.Outputs)
	now := time.Now().UTC()
	return interfaces.ResourceState{
		ID:            spec.Name,
		Name:          spec.Name,
		Type:          spec.Type,
		Provider:      providerType,
		ProviderRef:   resourceSpecProviderRef(spec),
		ProviderID:    live.ProviderID,
		ConfigHash:    configHashMap(appliedConfig),
		AppliedConfig: appliedConfig,
		Outputs:       cloneMap(live.Outputs),
		Dependencies:  append([]string(nil), spec.DependsOn...),
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func liveConfigFromOutputs(outputs map[string]any) map[string]any {
	for _, key := range []string{"config", "applied_config", "appliedConfig"} {
		if cfg, ok := mapStringAny(outputs[key]); ok {
			return cfg
		}
	}
	return cloneMap(outputs)
}

func mapStringAny(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return cloneMap(m), true
	case map[string]string:
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// isAbstractSize reports whether s is one of the canonical abstract size tiers
// (xs/s/m/l/xl). Provider-specific slugs such as "db-s-1vcpu-1gb" return false
// so that ResolveSizing is not called for already-concrete values.
func isAbstractSize(s interfaces.Size) bool {
	switch s {
	case interfaces.SizeXS, interfaces.SizeS, interfaces.SizeM, interfaces.SizeL, interfaces.SizeXL:
		return true
	default:
		return false
	}
}
