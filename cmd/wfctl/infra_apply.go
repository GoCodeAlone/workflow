package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/inputsnapshot"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// printDriftReportIfAny writes the canonical FormatStaleError output to w
// when result.InputDriftReport is non-empty. Safe to call when result is
// nil or InputDriftReport is empty/nil — both yield a no-op so callers
// don't need to defensively check the field before calling.
//
// Wired in by W-3a/T3.1.5 as a standalone helper; the actual call site in
// applyWithProviderAndStore (or its successor) lands when W-3b/T3.7
// switches the in-process apply path through wfctlhelpers.ApplyPlan for
// v2 plugins. Until then this helper is exercised solely by the
// in-process drift test, NOT yet by any production caller — preserving
// the W-3a "zero runtime change for v1 plugins" invariant.
func printDriftReportIfAny(w io.Writer, result *interfaces.ApplyResult) {
	if result == nil || len(result.InputDriftReport) == 0 {
		return
	}
	// FormatStaleError emits the multi-line "plan stale: N input(s) changed"
	// header + per-key fingerprint diff + trailing hint. Println adds the
	// terminating newline so the next stderr line is not glued to the hint.
	fmt.Fprintln(w, inputsnapshot.FormatStaleError(result.InputDriftReport))
}

// infraApplyTroubleshootTimeout is the budget for a Troubleshoot call when
// infra apply fails. Kept separate so tests can override it.
var infraApplyTroubleshootTimeout = 30 * time.Second

// computeInfraPlan is the indirection seam through which apply dispatches
// the diff plan. Defaults to platform.ComputePlan; tests override it to
// observe the provider arg without standing up a real gRPC plugin
// (mirroring resolveIaCProvider/loadIaCPlugin in deploy_providers.go).
var computeInfraPlan = platform.ComputePlan

// applyV2ApplyPlanFn is the indirection seam through which apply
// dispatches the v2 path (wfctlhelpers.ApplyPlan). Defaults to the
// production helper; tests override it to assert routing decisions
// without standing up a real plugin or executing real driver calls.
// Same var-seam pattern as computeInfraPlan / resolveIaCProvider.
var applyV2ApplyPlanFn = wfctlhelpers.ApplyPlan

// applyAllowReplaceSet is the per-invocation allow-list of resource
// names whose `protected: true` annotation is overridden for this
// apply. Populated by runInfraApply from the --allow-replace=<csv>
// flag value via parseAllowReplaceFlag; reset to nil at the top of
// every runInfraApply so the gate fails closed on subsequent
// invocations that do not pass the flag.
//
// Read inside validateAllowReplaceProtected, called from both apply
// dispatch paths (live-diff and --plan). nil/empty set means no
// override — every replace/delete on a protected resource errors
// before dispatch.
var applyAllowReplaceSet map[string]struct{}

// parseAllowReplaceFlag turns a comma-separated --allow-replace=<csv>
// flag value into a name-set. Empty input → nil (the canonical
// "no override" value, indistinguishable from the flag never being
// passed). Whitespace around each name is trimmed so operators can
// copy-paste the design's plan-output format
// (`--allow-replace=name1,name2`) without it falling apart on
// stray spaces.
func parseAllowReplaceFlag(raw string) map[string]struct{} {
	if raw == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validateAllowReplaceProtected delegates to
// wfctlhelpers.ValidateAllowReplaceProtected — the gate logic was
// promoted to the iac/wfctlhelpers package in W-7/T7.9 so the
// iac/conformance suite can exercise the contract without importing
// cmd/wfctl (package main). The thin wrapper keeps every existing
// in-package call site (this file + infra_apply_*.go tests)
// unchanged. Behavior and error format are byte-identical with the
// pre-W-7 implementation.
func validateAllowReplaceProtected(plan interfaces.IaCPlan, allow map[string]struct{}) error {
	return wfctlhelpers.ValidateAllowReplaceProtected(plan, allow)
}

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

	// --include: apply scope filter. Resolve the include set from the package-
	// level flag var (set by runInfraApply). State is loaded first so that
	// state-only resources (eligible for delete) are accepted in the include
	// set. Filtering happens before grouping so provider groups only see the
	// scoped specs.
	includeSet := parseIncludeFlag(currentApplyIncludeFlag)

	// Load current state once; nil on first run is valid (no prior state).
	current, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("load current state: %w", err)
	}

	// Validate and apply the include filter to both specs and state before
	// grouping by provider so that out-of-scope resources are never passed
	// down to any provider.
	if err := validateIncludeSet(includeSet, infraSpecs, current); err != nil {
		return err
	}
	infraSpecs = filterSpecsByInclude(infraSpecs, includeSet)
	current = filterStatesByInclude(current, includeSet)

	// Load full config to resolve iac.provider module definitions.
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Plan-time JIT resolution (PR-1): substitute ${MODULE.field} and
	// ${SECRET} refs against current state so driver.Diff sees real
	// values instead of literal templates. Apply does not print the
	// diagnostics — they're plan-output sugar only.
	infraSpecs, _, err = resolveSpecsAgainstState(infraSpecs, current, cfg, envName)
	if err != nil {
		return fmt.Errorf("resolve specs against state: %w", err)
	}

	// Build a lookup table of iac.provider module name → (providerType, providerCfg).
	// Also track which providers are explicitly disabled for this env so we can
	// emit a precise error if an infra module references one.
	// Per-env overrides are applied when envName is set so that provider credentials
	// or regions declared under environments: are respected.
	providerDefs, providerTypeCounts, disabledProviders := resolveProviderDefs(cfg, envName)

	// Group infra specs by iac.provider module name, preserving first-reference order.
	// Uses the already-filtered infraSpecs so provider groups only contain
	// in-scope resources.
	groupOrder, groups, err := groupSpecsByProviderRef(infraSpecs, providerDefs, disabledProviders, envName)
	if err != nil {
		return err
	}

	// Supplement with state-only groups when infraSpecs is empty after include
	// filtering. Without this, an --include set that names only state-only
	// resources (eligible for delete) would become a silent no-op because
	// groupSpecsByProviderRef finds no provider groups from an empty spec
	// list. Merge: state groups only add entries not already in groups.
	if len(infraSpecs) == 0 && len(current) > 0 {
		stateOrder, stateGroups := groupStatesByProviderRef(current, providerDefs, disabledProviders)
		for _, ref := range stateOrder {
			if _, exists := groups[ref]; !exists {
				groups[ref] = stateGroups[ref]
				groupOrder = append(groupOrder, ref)
			}
		}
	}

	// Resolve the state store once. A missing iac.state module resolves to a
	// noop store, but a configured backend that cannot be opened is fatal:
	// applying cloud mutations without durable state risks losing provider_ref
	// ownership metadata.
	store, storeErr := resolveStateStore(cfgFile, envName)
	if storeErr != nil {
		return fmt.Errorf("open state store: %w", storeErr)
	}

	// Apply each provider group in first-reference order (the order in which
	// each group's first spec appeared in infraSpecs, not iac.provider declaration order).
	applyGroup := func(moduleRef string, g *specGroup) error {
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

	// Compute the diff plan via the loaded provider so platform.ComputePlan
	// can dispatch ResourceDriver.Diff over the live plugin process for
	// honest Replace-action classification (T3.6e). Indirected through
	// computeInfraPlan so tests can spy on the provider arg without
	// standing up a real gRPC plugin (var-seam pattern matches
	// resolveIaCProvider/loadIaCPlugin in deploy_providers.go).
	plan, err := computeInfraPlan(ctx, provider, specs, current)
	if err != nil {
		return fmt.Errorf("compute plan: %w", err)
	}
	if len(plan.Actions) == 0 {
		fmt.Println("  No changes — infrastructure is up-to-date.")
		return nil
	}

	// W-6/T6.1: gate replace and delete actions on `protected: true`
	// resources behind --allow-replace. Without an explicit per-resource
	// opt-in, the apply errors before any provider Apply / wfctlhelpers
	// dispatch — destructive actions on protected infrastructure must
	// be intentional. T6.2 swaps this fail-fast for an aggregated
	// multi-blocker report.
	if err := validateAllowReplaceProtected(plan, applyAllowReplaceSet); err != nil {
		return err
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

	// W-3b T3.7: branch on the loaded plugin's manifest. Providers
	// declaring iacProvider.computePlanVersion: v2 in plugin.json route
	// through wfctlhelpers.ApplyPlan (Replace + drift postcondition);
	// everything else takes the legacy provider.Apply path. NO env-var
	// gate (rev2/rev3 fix per cycle-2 — there is no transitional
	// WFCTL_USE_V2_APPLY); the choice is plugin-author-controlled at
	// load time and surfaced via the optional
	// wfctlhelpers.ComputePlanVersionDeclarer interface.
	var result *interfaces.ApplyResult
	// DispatchVersionFor centralises the type-assertion + default; pass the
	// raw provider rather than re-asserting ComputePlanVersionDeclarer at the
	// call site (per dispatch.go contract).
	if wfctlhelpers.DispatchVersionFor(provider) == wfctlhelpers.DispatchVersionV2 {
		result, err = applyV2ApplyPlanFn(ctx, provider, &plan)
		// printDriftReportIfAny was added unwired in W-3a/T3.1.5; the
		// v2 dispatch is the production caller that surfaces input
		// drift to the operator. Run on success OR partial failure
		// (the operator most needs the drift diagnostic when an apply
		// fails — "which input went stale during the failed apply?"
		// — so we print whenever a result was produced rather than
		// gating on err == nil). Silently no-ops when the report is
		// empty, so unconditional-on-result-non-nil is safe.
		if result != nil {
			printDriftReportIfAny(w, result)
		}
	} else {
		result, err = provider.Apply(ctx, &plan)
	}
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
		// Provider.Apply can surface a top-level error (vs. populating
		// result.Errors[]). Emit the actionable hint here too if the
		// top-level error is/wraps interfaces.ErrImageNotInRegistry —
		// otherwise plugin paths that bubble the sentinel via err escape
		// the result.Errors[]-only hint below. See infra_image_presence_hint.go.
		emitImageNotInRegistryHint(os.Stderr, err)
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
				ID:                  r.Name,
				Name:                r.Name,
				Type:                r.Type,
				Provider:            providerType,
				ProviderRef:         providerRef,
				ProviderID:          r.ProviderID,
				ConfigHash:          configHashMap(appliedCfg),
				AppliedConfig:       appliedCfg,
				AppliedConfigSource: "apply",
				Outputs:             r.Outputs,
				Dependencies:        dependencies,
				CreatedAt:           now,
				UpdatedAt:           now,
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
			finalErr := fmt.Errorf("%d resource(s) failed: %s", len(result.Errors), strings.Join(msgs, "; "))
			// Emit an actionable hint to stderr if any per-resource error
			// matches interfaces.ErrImageNotInRegistry (typed in-process or
			// string-match across gRPC boundary). See infra_image_presence_hint.go.
			emitImageNotInRegistryHint(os.Stderr, finalErr)
			return finalErr
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
		ID:                  spec.Name,
		Name:                spec.Name,
		Type:                spec.Type,
		Provider:            providerType,
		ProviderRef:         resourceSpecProviderRef(spec),
		ProviderID:          live.ProviderID,
		ConfigHash:          configHashMap(appliedConfig),
		AppliedConfig:       appliedConfig,
		AppliedConfigSource: "adoption",
		Outputs:             cloneMap(live.Outputs),
		Dependencies:        append([]string(nil), spec.DependsOn...),
		CreatedAt:           now,
		UpdatedAt:           now,
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

// desiredStateHash returns a stable SHA-256 hex digest of the canonical
// desired-state inputs: specs sorted by name and JSON-serialised. It is
// embedded in plan.json by runInfraPlan and verified by runInfraApply
// --plan to detect plans that are stale relative to the current config.
func desiredStateHash(specs []interfaces.ResourceSpec) string {
	// Do NOT short-circuit for empty specs: a plan that removes all resources
	// ("delete all") has a valid, deterministic hash (sha256("[]")). Returning ""
	// for empty specs would block such plans with a misleading "no hash" error.
	// The "" sentinel is reserved exclusively for marshal failures below.
	sorted := make([]interfaces.ResourceSpec, len(specs))
	copy(sorted, specs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	data, err := json.Marshal(sorted)
	if err != nil {
		// Should never happen for YAML-decoded structs, but return the empty
		// sentinel rather than silently hashing nil bytes — callers treat ""
		// as "hash unavailable" and will reject the plan with a clear error.
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// loadPlanFromFile reads and deserialises a plan.json written by wfctl infra plan -o.
func loadPlanFromFile(path string) (interfaces.IaCPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return interfaces.IaCPlan{}, fmt.Errorf("read plan file %q: %w", path, err)
	}
	var plan interfaces.IaCPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return interfaces.IaCPlan{}, fmt.Errorf("parse plan file %q: %w", path, err)
	}
	return plan, nil
}

// applyFromPrecomputedPlan dispatches a pre-computed plan without calling
// ComputePlan. It loads IaCProvider plugins from cfgFile, groups plan actions
// by iac.provider module, and calls provider.Apply for each group. State is
// persisted exactly as in the live-diff path.
func applyFromPrecomputedPlan(ctx context.Context, plan interfaces.IaCPlan, cfgFile, envName string) error { //nolint:cyclop
	// Load full config to resolve iac.provider module definitions.
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build provider lookup (mirrors applyInfraModules).
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
		providerDefs[m.Name] = providerDef{provType: pt, provCfg: modCfg}
	}

	// Resolve state store (noop when no iac.state module is configured).
	store, storeErr := resolveStateStore(cfgFile, envName)
	if storeErr != nil {
		return fmt.Errorf("open state store: %w", storeErr)
	}

	// Group plan actions by iac.provider module, preserving action order.
	type actionGroup struct {
		provType string
		provCfg  map[string]any
		actions  []interfaces.PlanAction
	}
	groups := map[string]*actionGroup{}
	var groupOrder []string

	for i := range plan.Actions {
		action := &plan.Actions[i]
		moduleRef, _ := action.Resource.Config["provider"].(string)
		// delete actions from ComputePlan carry an empty Resource.Config — the
		// provider ref must be recovered from the recorded current state instead.
		if moduleRef == "" && action.Current != nil {
			moduleRef = action.Current.ProviderRef
			if moduleRef == "" {
				moduleRef, _ = action.Current.AppliedConfig["provider"].(string)
			}
		}
		if moduleRef == "" {
			return fmt.Errorf("plan action for %q: missing 'provider' field in resource config (delete actions require a current state record)", action.Resource.Name)
		}
		if _, exists := groups[moduleRef]; !exists {
			def, ok := providerDefs[moduleRef]
			if !ok {
				return fmt.Errorf("plan action for %q references provider %q which is not declared as an iac.provider module", action.Resource.Name, moduleRef)
			}
			groups[moduleRef] = &actionGroup{provType: def.provType, provCfg: def.provCfg}
			groupOrder = append(groupOrder, moduleRef)
		}
		groups[moduleRef].actions = append(groups[moduleRef].actions, *action)
	}

	// Apply each provider group in declaration order.
	for _, moduleRef := range groupOrder {
		g := groups[moduleRef]
		groupPlan := interfaces.IaCPlan{
			ID:        plan.ID,
			Actions:   g.actions,
			CreatedAt: plan.CreatedAt,
		}
		fmt.Printf("Applying %d resource(s) via provider %q (%s) from plan...\n", len(g.actions), moduleRef, g.provType)
		provider, closer, err := resolveIaCProvider(ctx, g.provType, g.provCfg)
		if err != nil {
			return fmt.Errorf("provider %q (%s): load provider: %w", moduleRef, g.provType, err)
		}
		applyErr := applyPrecomputedPlanWithStore(ctx, groupPlan, provider, g.provType, store, os.Stderr, envName)
		if closer != nil {
			if cerr := closer.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", g.provType, cerr)
			}
		}
		if applyErr != nil {
			return fmt.Errorf("provider %q: %w", moduleRef, applyErr)
		}
	}

	n := len(plan.Actions)
	if n == 1 {
		fmt.Printf("applied 1 action from plan\n")
	} else {
		fmt.Printf("applied %d actions from plan\n", n)
	}
	return nil
}

// applyPrecomputedPlanWithStore executes a pre-computed plan group via
// provider.Apply and persists state for each provisioned resource. It is the
// precomputed-plan counterpart of applyWithProviderAndStore, skipping
// ComputePlan / adoptExistingResources entirely.
func applyPrecomputedPlanWithStore(ctx context.Context, plan interfaces.IaCPlan, provider interfaces.IaCProvider, providerType string, store infraStateStore, w io.Writer, envName string) error {
	if store == nil {
		store = &noopStateStore{}
	}
	if len(plan.Actions) == 0 {
		fmt.Println("  No changes — infrastructure is up-to-date.")
		return nil
	}

	// W-6/T6.1: same protected-resource gate as the live-diff path. The
	// --plan path skips ComputePlan entirely but the safety guarantee
	// must hold regardless of how the plan was produced.
	if err := validateAllowReplaceProtected(plan, applyAllowReplaceSet); err != nil {
		return err
	}

	// Collect delete-action resource names for post-apply state cleanup.
	deleteNames := make(map[string]struct{})
	for i := range plan.Actions {
		if plan.Actions[i].Action == "delete" {
			deleteNames[plan.Actions[i].Resource.Name] = struct{}{}
		}
	}

	// Resolve abstract sizing tiers into concrete provider-specific values,
	// mirroring the live-diff path in applyWithProviderAndStore. Without this,
	// specs with Size:"m" would reach the provider unresolved.
	for i := range plan.Actions {
		if plan.Actions[i].Action == "delete" {
			continue // deletes carry no spec to resolve
		}
		spec := &plan.Actions[i].Resource
		if spec.Size == "" || !isAbstractSize(spec.Size) {
			continue
		}
		sizing, sErr := provider.ResolveSizing(spec.Type, spec.Size, spec.Hints)
		if sErr != nil {
			return fmt.Errorf("%s/%s: resolve sizing: %w", spec.Type, spec.Name, sErr)
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

	validateInputProviderIDs(provider, &plan)
	fmt.Printf("  Plan: %d action(s) to execute.\n", len(plan.Actions))
	result, err := provider.Apply(ctx, &plan)
	if err != nil {
		ref := interfaces.ResourceRef{}
		if len(plan.Actions) == 1 {
			ref.Name = plan.Actions[0].Resource.Name
			ref.Type = plan.Actions[0].Resource.Type
		} else if len(plan.Actions) > 1 {
			// Fall back to first action so the troubleshooter has at least
			// a name/type to work with on multi-action failures.
			ref.Name = plan.Actions[0].Resource.Name
			ref.Type = plan.Actions[0].Resource.Type
		}
		em := detectCIProvider()
		var diags []interfaces.Diagnostic
		if ref.Type != "" {
			if rd, rdErr := provider.ResourceDriver(ref.Type); rdErr == nil {
				diags = troubleshootAfterFailure(ctx, w, rd, ref, err, infraApplyTroubleshootTimeout, em)
			}
		}
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
		// Provider.Apply can surface a top-level error (vs. populating
		// result.Errors[]). Emit the actionable hint here too if the
		// top-level error is/wraps interfaces.ErrImageNotInRegistry —
		// otherwise plugin paths that bubble the sentinel via err escape
		// the result.Errors[]-only hint below. See infra_image_presence_hint.go.
		emitImageNotInRegistryHint(os.Stderr, err)
		return fmt.Errorf("apply: %w", err)
	}

	if result != nil {
		for _, r := range result.Resources {
			fmt.Printf("  ✓ %s (%s)\n", r.Name, r.Type)

			if err := validateOutputProviderID(provider, providerType, &r); err != nil {
				return fmt.Errorf("state write rejected: %w", err)
			}

			// Retrieve spec metadata from the plan action for state persistence.
			var appliedCfg map[string]any
			var providerRef string
			var dependencies []string
			for i := range plan.Actions {
				if plan.Actions[i].Resource.Name == r.Name {
					appliedCfg = plan.Actions[i].Resource.Config
					providerRef, _ = plan.Actions[i].Resource.Config["provider"].(string)
					dependencies = append([]string(nil), plan.Actions[i].Resource.DependsOn...)
					break
				}
			}

			now := time.Now().UTC()
			rs := interfaces.ResourceState{
				ID:                  r.Name,
				Name:                r.Name,
				Type:                r.Type,
				Provider:            providerType,
				ProviderRef:         providerRef,
				ProviderID:          r.ProviderID,
				ConfigHash:          configHashMap(appliedCfg),
				AppliedConfig:       appliedCfg,
				AppliedConfigSource: "apply",
				Outputs:             r.Outputs,
				Dependencies:        dependencies,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			if saveErr := store.SaveResource(ctx, rs); saveErr != nil {
				return fmt.Errorf("%s/%s: persist state after apply: %w", r.Type, r.Name, saveErr)
			}
		}

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
			finalErr := fmt.Errorf("%d resource(s) failed: %s", len(result.Errors), strings.Join(msgs, "; "))
			// Emit an actionable hint to stderr if any per-resource error
			// matches interfaces.ErrImageNotInRegistry (typed in-process or
			// string-match across gRPC boundary). See infra_image_presence_hint.go.
			emitImageNotInRegistryHint(os.Stderr, finalErr)
			return finalErr
		}
	}
	return nil
}
