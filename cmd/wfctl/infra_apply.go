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
	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/secrets"
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

// applyV2ApplyPlanWithHooksFn is the indirection seam through which apply
// dispatches the v2 path. Defaults to the production helper; tests override
// it to assert routing decisions without standing up a real plugin or
// executing real driver calls. Same var-seam pattern as computeInfraPlan /
// resolveIaCProvider.
var applyV2ApplyPlanWithHooksFn = wfctlhelpers.ApplyPlanWithHooks

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

// hasPlatformModules reports whether cfgFile contains any modules with the
// platform.* type prefix (e.g., platform.kubernetes, platform.ecs).
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
// applyInfraModules applies infra.* modules and returns the hydrated
// routed-secret map for in-process hand-off to post-apply consumers
// (syncInfraOutputSecrets). The map may be empty when no driver emitted
// sensitive outputs in this run, or when no secrets.Provider is
// configured. Returning a separate map (rather than threading via a
// callback) keeps the caller's flow explicit: apply → consume hydrated.
func applyInfraModules(ctx context.Context, cfgFile, envName string) (map[string]string, error) { //nolint:cyclop
	// Resolve specs (env overrides applied when envName is set).
	specs, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return nil, fmt.Errorf("parse infra resource specs: %w", err)
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
		return nil, nil
	}
	if err := validateUniqueInfraResourceNames(infraSpecs); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("load current state: %w", err)
	}

	// Validate and apply the include filter to both specs and state before
	// grouping by provider so that out-of-scope resources are never passed
	// down to any provider.
	if err := validateIncludeSet(includeSet, infraSpecs, current); err != nil {
		return nil, err
	}
	infraSpecs = filterSpecsByInclude(infraSpecs, includeSet)
	current = filterStatesByInclude(current, includeSet)

	// Load full config to resolve iac.provider module definitions.
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Plan-time JIT resolution (PR-1): substitute ${MODULE.field} and
	// ${SECRET} refs against current state so driver.Diff sees real
	// values instead of literal templates. Apply does not print the
	// diagnostics — they're plan-output sugar only.
	infraSpecs, _, err = resolveSpecsAgainstState(infraSpecs, current, cfg, envName)
	if err != nil {
		return nil, fmt.Errorf("resolve specs against state: %w", err)
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
		return nil, err
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
		return nil, fmt.Errorf("open state store: %w", storeErr)
	}

	// runHydrated accumulates routed-secret values from this apply across all
	// provider groups. Passed to syncInfraOutputSecrets after the loop so
	// post-apply consumers can read just-routed values via in-memory hand-off
	// (works on write-only providers like GitHub Actions).
	runHydrated := make(map[string]string)

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
		return applyWithProviderAndStore(ctx, provider, g.provType, g.specs, scopedCurrent, store, os.Stderr, envName, cfgFile, runHydrated)
	}
	for _, moduleRef := range groupOrder {
		if err := applyGroup(moduleRef, groups[moduleRef]); err != nil {
			return runHydrated, fmt.Errorf("provider %q: %w", moduleRef, err)
		}
	}
	return runHydrated, nil
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
	return resolveIaCProviderRef(st.AppliedConfig)
}

func resourceSpecProviderRef(spec interfaces.ResourceSpec) string {
	if spec.Config == nil {
		return ""
	}
	return resolveIaCProviderRef(spec.Config)
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
func applyWithProviderAndStore(ctx context.Context, provider interfaces.IaCProvider, providerType string, specs []interfaces.ResourceSpec, current []interfaces.ResourceState, store infraStateStore, w io.Writer, envName string, cfgFile string, hydratedOut map[string]string) error {
	if store == nil {
		store = &noopStateStore{}
	}
	secretsProvider, secretsErr := loadSecretsProviderForRouting(cfgFile)
	if secretsErr != nil {
		return secretsErr
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
	current, err = adoptExistingResources(ctx, provider, providerType, specs, current, store, secretsProvider, hydratedOut)
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

	// Soft-warn if any update/delete action targets a resource whose ProviderID
	// does not match the driver's declared format. The driver may self-heal, so
	// we log and continue rather than blocking the apply.
	validateInputProviderIDs(provider, &plan)
	fmt.Printf("  Plan: %d action(s) to execute.\n", len(plan.Actions))

	// v2 is the only supported dispatch per ADR 0024 + workflow#699.
	// IaCProvider.Apply was hard-deleted from the interface; all routing
	// goes through wfctlhelpers.ApplyPlanWithHooks (Replace + drift
	// postcondition + IaCProviderFinalizer fan-out).
	hooks := statePersistenceHooks(store, secretsProvider, provider, providerType, plan.ID, hydratedOut)
	result, err := applyV2ApplyPlanWithHooksFn(ctx, provider, &plan, hooks)
	// printDriftReportIfAny surfaces input-drift to the operator on
	// success OR partial failure — silently no-ops on empty reports.
	if result != nil {
		printDriftReportIfAny(w, result)
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
		if len(result.Errors) > 0 {
			msgs := make([]string, 0, len(result.Errors))
			for _, ae := range result.Errors {
				msgs = append(msgs, fmt.Sprintf("%s/%s: %s", ae.Action, ae.Resource, ae.Error))
			}
			finalErr := fmt.Errorf("%d resource(s) failed: %s", len(result.Errors), strings.Join(msgs, "; "))
			emitImageNotInRegistryHint(os.Stderr, finalErr)
			return finalErr
		}
	}
	return nil
}

func statePersistenceHooks(
	store infraStateStore,
	secretsProvider secrets.Provider,
	provider interfaces.IaCProvider,
	providerType string,
	planID string,
	hydratedOut map[string]string,
) wfctlhelpers.ApplyPlanHooks {
	return wfctlhelpers.ApplyPlanHooks{
		OnResourceApplied: func(ctx context.Context, driver interfaces.ResourceDriver, action interfaces.PlanAction, out interfaces.ResourceOutput) error {
			hyd, persistErr := persistAppliedResourceOutput(ctx, store, secretsProvider, provider, providerType, driver, action, out)
			if persistErr != nil {
				return persistErr
			}
			fmt.Printf("  ✓ %s (%s)\n", action.Resource.Name, action.Resource.Type)
			if hydratedOut != nil {
				for k, v := range hyd {
					hydratedOut[k] = v
				}
			}
			return nil
		},
		OnResourceDeleted: func(ctx context.Context, action interfaces.PlanAction) error {
			return deleteStateAfterCloudDelete(store, action.Resource.Name)
		},
		// OnPlanComplete is the workflow#695 Phase 2.5 hook that bridges
		// the v2 apply path to the plugin's optional IaCProviderFinalizer
		// service. Fires exactly once on the natural success-exit return
		// of applyPlanWithEnvProviderAndHooks (v1 semantic preservation
		// per cycle-1 plan-review C-3 — see ApplyPlanHooks.OnPlanComplete
		// godoc for fire/no-fire enumeration).
		//
		// No-op paths (preserve pre-Phase-2.5 behavior):
		//   - provider is not a *typedIaCAdapter (in-process fakes,
		//     legacy provider shapes): no Finalizer() accessor available;
		//     skip silently.
		//   - adapter.Finalizer() returns nil (plugin did not register
		//     IaCProviderFinalizer per ADR 0024): skip silently. Plugins
		//     opt in via service registration; absence = no firing.
		//
		// Fire path: invoke FinalizeApply RPC; on gRPC transport error
		// surface wrapped; on per-driver errors in response, aggregate
		// the per-driver attribution into a single err message that
		// preserves the Resource/Action/Error shape from the v1 wrapper
		// (per ADR 0040 invariant on per-driver attribution). The engine
		// closure in apply.go's deferred OnPlanComplete handler appends
		// the returned err to result.Errors as the "<plan-finalize>"
		// entry and surfaces wrapped to outer caller err.
		OnPlanComplete: func(ctx context.Context) error {
			adapter, ok := provider.(*typedIaCAdapter)
			if !ok {
				return nil
			}
			fin := adapter.Finalizer()
			if fin == nil {
				return nil
			}
			resp, callErr := fin.FinalizeApply(ctx, &pb.FinalizeApplyRequest{PlanId: planID})
			if callErr != nil {
				return fmt.Errorf("FinalizeApply gRPC: %w", callErr)
			}
			if len(resp.GetErrors()) > 0 {
				msgs := make([]string, 0, len(resp.GetErrors()))
				for _, e := range resp.GetErrors() {
					// Field order is Resource/Action (matches proto field
					// declaration order in ActionError + apply.go's
					// ActionError construction). NOTE: applyWithProviderAndStore's
					// existing per-resource aggregator above uses the inverse
					// Action/Resource order — pre-existing file-level
					// inconsistency, not introduced here. Reconciliation
					// (flipping the older site to Resource/Action canonical
					// order) is tracked separately; do NOT "fix" this site
					// back to Action/Resource without flipping the other too.
					msgs = append(msgs, fmt.Sprintf("%s/%s: %s", e.GetResource(), e.GetAction(), e.GetError()))
				}
				return fmt.Errorf("plugin finalize: %d driver(s) failed: %s", len(resp.GetErrors()), strings.Join(msgs, "; "))
			}
			return nil
		},
	}
}

func deleteStateAfterCloudDelete(store infraStateStore, name string) error {
	stateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return store.DeleteResource(stateCtx, name)
}

func persistAppliedResourceOutput(
	ctx context.Context,
	store infraStateStore,
	secretsProvider secrets.Provider,
	provider interfaces.IaCProvider,
	providerType string,
	driver interfaces.ResourceDriver,
	action interfaces.PlanAction,
	out interfaces.ResourceOutput,
) (map[string]string, error) {
	spec := action.Resource
	compensate := actionCreatesReplacementResource(action)
	normalized, err := normalizeAppliedOutputIdentity(spec, out)
	if err != nil {
		if !compensate {
			return nil, fmt.Errorf("state write rejected: %w", err)
		}
		compErr := compensateAfterIdentityValidationFailure(driver, spec, out)
		if compErr != nil {
			return nil, fmt.Errorf("state write rejected: %w (compensating delete failed: %v)", err, compErr)
		}
		return nil, fmt.Errorf("state write rejected: %w (compensating delete succeeded)", err)
	}
	out = normalized
	if err := validateOutputProviderIDWithDriver(providerType, driver, &out); err != nil {
		if !compensate {
			return nil, fmt.Errorf("state write rejected: %w", err)
		}
		rs := interfaces.ResourceState{Name: spec.Name, Type: spec.Type, ProviderID: out.ProviderID}
		compErr := compensateAfterValidationFailure(driver, rs)
		if compErr != nil {
			return nil, fmt.Errorf("state write rejected: %w (compensating delete failed: %v)", err, compErr)
		}
		return nil, fmt.Errorf("state write rejected: %w (compensating delete succeeded)", err)
	}
	now := time.Now().UTC()
	rs := interfaces.ResourceState{
		ID:                  spec.Name,
		Name:                spec.Name,
		Type:                spec.Type,
		Provider:            providerType,
		ProviderRef:         resolveIaCProviderRef(spec.Config),
		ProviderID:          out.ProviderID,
		ConfigHash:          configHashMap(spec.Config),
		AppliedConfig:       spec.Config,
		AppliedConfigSource: "apply",
		// Outputs is set by persistResourceWithSecretRouting after Route.
		Dependencies: append([]string(nil), spec.DependsOn...),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	mode := persistModeApplyNoCompensate
	if compensate {
		mode = persistModeApply
	}
	return persistResourceWithSecretRouting(ctx, store, secretsProvider, driver, rs, out, mode)
}

func actionCreatesReplacementResource(action interfaces.PlanAction) bool {
	return action.Action == "create" || action.Action == "replace"
}

func normalizeAppliedOutputIdentity(spec interfaces.ResourceSpec, out interfaces.ResourceOutput) (interfaces.ResourceOutput, error) {
	if out.Name == "" {
		out.Name = spec.Name
	} else if out.Name != spec.Name {
		return out, fmt.Errorf("driver returned output name %q for resource %q", out.Name, spec.Name)
	}
	if out.Type == "" {
		out.Type = spec.Type
	} else if out.Type != spec.Type {
		return out, fmt.Errorf("driver returned output type %q for resource %q (expected %q)", out.Type, spec.Name, spec.Type)
	}
	return out, nil
}

func adoptExistingResources(ctx context.Context, provider interfaces.IaCProvider, providerType string, specs []interfaces.ResourceSpec, current []interfaces.ResourceState, store infraStateStore, secretsProvider secrets.Provider, hydratedOut map[string]string) ([]interfaces.ResourceState, error) {
	if len(specs) == 0 {
		return current, nil
	}
	currentByName := make(map[string]struct{}, len(current))
	for i := range current {
		currentByName[current[i].Name] = struct{}{}
	}

	// Precompute prior-state lookup once per adoption pass. persistReadMode
	// inherits routed-secret placeholders from prior state for sensitive
	// keys. Without the cache, each Read would re-ListResources + linear
	// scan — O(n²) on filesystem-backed stores. priorByName remains stable
	// for the duration of this loop (newly-adopted records are appended to
	// store and to current[] but the placeholder-inheritance lookup only
	// needs the pre-loop snapshot).
	priorByName := make(map[string]*interfaces.ResourceState)
	if all, listErr := store.ListResources(ctx); listErr == nil {
		for i := range all {
			priorByName[all[i].Name] = &all[i]
		}
	}

	drivers := make(map[string]interfaces.ResourceDriver)
	for _, spec := range specs {
		if _, exists := currentByName[spec.Name]; exists {
			continue
		}
		explicitAdoptable := hasBuiltInAdoptionRef(spec.Type) || boolFromAny(spec.Config["adopt_existing"])
		driver, ok := drivers[spec.Type]
		if !ok {
			var err error
			driver, err = provider.ResourceDriver(spec.Type)
			if err != nil {
				if !explicitAdoptable {
					continue
				}
				return nil, fmt.Errorf("%s/%s: resolve resource driver: %w", spec.Type, spec.Name, err)
			}
			if driver == nil {
				if !explicitAdoptable {
					continue
				}
				return nil, fmt.Errorf("%s/%s: resolve resource driver: driver returned nil", spec.Type, spec.Name)
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
		mode := persistModeRead
		routeProvider := secrets.Provider(nil)
		if secretsProvider != nil && hasSensitiveOutputs(live) {
			// Adoption is a live cloud read, but when a secrets provider is
			// configured the same apply process must route newly discovered
			// sensitive outputs. Otherwise infra_output generators cannot
			// consume write-only stores like GitHub Actions secrets.
			mode = persistModeAdoptRoute
			routeProvider = secretsProvider
		}
		hydrated, err := persistResourceWithSecretRoutingCachedPrior(ctx, store, routeProvider, driver, state, *live, mode, priorByName)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: persist adopted state: %w", spec.Type, spec.Name, err)
		}
		if hydratedOut != nil {
			for k, v := range hydrated {
				hydratedOut[k] = v
			}
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
	if spec.Type == "infra.dns" {
		ref := interfaces.ResourceRef{Name: spec.Name, Type: spec.Type}
		if domain, _ := spec.Config["domain"].(string); domain != "" {
			ref.ProviderID = domain
		} else {
			ref.ProviderID = spec.Name
		}
		return ref, true, nil
	}
	if boolFromAny(spec.Config["adopt_existing"]) {
		if spec.Name == "" {
			return interfaces.ResourceRef{}, false, fmt.Errorf("%s adoption requires resource name", spec.Type)
		}
		if !driverSupportsConfigAdoption(driver) {
			return interfaces.ResourceRef{}, false, fmt.Errorf("%s/%s: adopt_existing requires a driver that supports name-based adoption", spec.Type, spec.Name)
		}
		return interfaces.ResourceRef{Name: spec.Name, Type: spec.Type}, true, nil
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

type configAdoptionSupporter interface {
	SupportsConfigAdoption() bool
}

func driverSupportsConfigAdoption(driver interfaces.ResourceDriver) bool {
	if supporter, ok := driver.(configAdoptionSupporter); ok {
		return supporter.SupportsConfigAdoption()
	}
	if supporter, ok := driver.(interfaces.UpsertSupporter); ok {
		return supporter.SupportsUpsert()
	}
	return false
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

// persistMode controls how persistResourceWithSecretRouting handles a
// driver output: persistModeApply routes sensitive fields through the
// provider; persistModeRead is sanitize-only (no provider writes — used
// by adoption / refresh paths to avoid cache pollution).
type persistMode int

const (
	persistModeApply persistMode = iota
	persistModeRead
	persistModeApplyNoCompensate
	persistModeAdoptRoute
)

// persistResourceWithSecretRouting builds rs.Outputs from out (routing
// sensitive fields through provider in apply mode), calls
// store.SaveResource, and returns the hydrated routed-secret map for
// in-process hand-off. On sensitive-route or SaveResource failure after
// cloud mutation (apply mode only), invokes driver.Delete + provider.Delete
// to compensate the partial cloud-resource creation. Returns a wrapped
// error naming both the original failure and the compensating-Delete outcome.
//
// In read mode, the helper does NOT call provider.Set; instead it consults
// the prior state via store.GetResource and inherits any
// PlaceholderPrefix entries; new sensitive keys (declared by the driver
// at Read time but not previously routed) are dropped from sanitized.
//
// Returns nil hydrated map in read mode (consumers don't need post-apply
// hand-off for a Read).
//
// store is the wfctl-internal infraStateStore interface, not
// interfaces.IaCStateStore. The helper lives in cmd/wfctl; using the
// package-private interface keeps the boundary clean and matches existing
// callers (applyWithProviderAndStore, adoptExistingResources).
func persistResourceWithSecretRouting(
	ctx context.Context,
	store infraStateStore,
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
	mode persistMode,
) (map[string]string, error) {
	switch mode {
	case persistModeApply:
		return persistApplyMode(ctx, store, provider, driver, rs, out, true)
	case persistModeApplyNoCompensate:
		return persistApplyMode(ctx, store, provider, driver, rs, out, false)
	case persistModeAdoptRoute:
		return persistAdoptRouteMode(ctx, store, provider, rs, out)
	case persistModeRead:
		return nil, persistReadMode(ctx, store, rs, out, nil)
	default:
		return nil, fmt.Errorf("persistResourceWithSecretRouting: unknown mode %d", mode)
	}
}

// persistResourceWithSecretRoutingCachedPrior is the loop-friendly variant for
// callers that drive multiple Reads in a tight loop (refresh, adoption). It
// accepts a precomputed prior-state lookup map so persistReadMode does not
// have to ListResources + linear-scan per call (O(n²) on filesystem-backed
// stores). When mode != persistModeRead the priorByName map is ignored.
//
// Pass priorByName=nil to fall back to per-call ListResources behaviour
// (equivalent to persistResourceWithSecretRouting).
func persistResourceWithSecretRoutingCachedPrior(
	ctx context.Context,
	store infraStateStore,
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
	mode persistMode,
	priorByName map[string]*interfaces.ResourceState,
) (map[string]string, error) {
	switch mode {
	case persistModeApply:
		return persistApplyMode(ctx, store, provider, driver, rs, out, true)
	case persistModeApplyNoCompensate:
		return persistApplyMode(ctx, store, provider, driver, rs, out, false)
	case persistModeAdoptRoute:
		return persistAdoptRouteMode(ctx, store, provider, rs, out)
	case persistModeRead:
		return nil, persistReadMode(ctx, store, rs, out, priorByName)
	default:
		return nil, fmt.Errorf("persistResourceWithSecretRoutingCachedPrior: unknown mode %d", mode)
	}
}

func persistApplyMode(
	ctx context.Context,
	store infraStateStore,
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
	compensate bool,
) (map[string]string, error) {
	sanitized, hydrated, err := sensitive.Route(ctx, provider, rs.Name, &out)
	if err != nil {
		if !compensate {
			return nil, fmt.Errorf("%s/%s: route sensitive outputs: %w", rs.Type, rs.Name, err)
		}
		compErr := compensateAfterSaveFailure(provider, driver, rs, hydrated)
		if compErr != nil {
			return nil, fmt.Errorf("%s/%s: route sensitive outputs: %w (compensating delete failed: %v)", rs.Type, rs.Name, err, compErr)
		}
		return nil, fmt.Errorf("%s/%s: route sensitive outputs: %w (compensating delete succeeded)", rs.Type, rs.Name, err)
	}
	rs.Outputs = sanitized
	if saveErr := store.SaveResource(ctx, rs); saveErr != nil {
		if !compensate {
			return nil, fmt.Errorf("%s/%s: persist state after apply: %w", rs.Type, rs.Name, saveErr)
		}
		// Compensating Delete: the matching cloud resource is real but
		// the state record didn't land. Roll back so a re-Apply doesn't
		// double-create.
		compErr := compensateAfterSaveFailure(provider, driver, rs, hydrated)
		if compErr != nil {
			return nil, fmt.Errorf("%s/%s: persist state after apply: %w (compensating delete failed: %v)", rs.Type, rs.Name, saveErr, compErr)
		}
		return nil, fmt.Errorf("%s/%s: persist state after apply: %w (compensating delete succeeded)", rs.Type, rs.Name, saveErr)
	}
	return hydrated, nil
}

func persistAdoptRouteMode(
	ctx context.Context,
	store infraStateStore,
	provider secrets.Provider,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
) (map[string]string, error) {
	sanitized, hydrated, err := sensitive.Route(ctx, provider, rs.Name, &out)
	if err != nil {
		if compErr := cleanupRoutedSecrets(provider, hydrated); compErr != nil {
			return nil, fmt.Errorf("%s/%s: route sensitive outputs: %w (routed-secret cleanup failed: %v)", rs.Type, rs.Name, err, compErr)
		}
		return nil, fmt.Errorf("%s/%s: route sensitive outputs: %w", rs.Type, rs.Name, err)
	}
	rs.Outputs = sanitized
	if saveErr := store.SaveResource(ctx, rs); saveErr != nil {
		if compErr := cleanupRoutedSecrets(provider, hydrated); compErr != nil {
			return nil, fmt.Errorf("%s/%s: persist adopted state: %w (routed-secret cleanup failed: %v)", rs.Type, rs.Name, saveErr, compErr)
		}
		return nil, fmt.Errorf("%s/%s: persist adopted state: %w", rs.Type, rs.Name, saveErr)
	}
	return hydrated, nil
}

func persistReadMode(
	ctx context.Context,
	store infraStateStore,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
	priorByName map[string]*interfaces.ResourceState,
) error {
	// Sanitize-only: inherit placeholders from prior state for sensitive
	// keys; drop newly-declared sensitive keys. Do NOT call provider.Set.
	//
	// Prior state lookup: prefer the caller-supplied map (precomputed once
	// per refresh/adoption pass) to avoid O(n²) ListResources scans. Fall
	// back to ListResources + filter when the cache is nil — keeps the
	// helper safe for ad-hoc one-shot callers (infraStateStore has no
	// GetResource).
	var prior *interfaces.ResourceState
	if priorByName != nil {
		prior = priorByName[rs.Name]
	} else if all, listErr := store.ListResources(ctx); listErr == nil {
		for i := range all {
			if all[i].Name == rs.Name {
				prior = &all[i]
				break
			}
		}
	}
	sanitized := make(map[string]any, len(out.Outputs))
	for k, v := range out.Outputs {
		sanitized[k] = v
	}
	for k, flag := range out.Sensitive {
		if !flag {
			continue
		}
		// If prior state has a placeholder for this key, preserve it.
		if prior != nil {
			if pv, ok := prior.Outputs[k]; ok && sensitive.IsPlaceholder(pv) {
				sanitized[k] = pv
				continue
			}
		}
		// Otherwise drop the field from sanitized — we don't have a
		// previously-routed secret and we won't pollute the provider
		// from a Read.
		delete(sanitized, k)
	}
	rs.Outputs = sanitized
	if err := store.SaveResource(ctx, rs); err != nil {
		return fmt.Errorf("%s/%s: persist state after read: %w", rs.Type, rs.Name, err)
	}
	return nil
}

func cleanupRoutedSecrets(provider secrets.Provider, hydrated map[string]string) error {
	if provider == nil || len(hydrated) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []error
	for secretName := range hydrated {
		if delErr := provider.Delete(ctx, secretName); delErr != nil && !errors.Is(delErr, secrets.ErrNotFound) {
			errs = append(errs, fmt.Errorf("provider.Delete(%s): %w", secretName, delErr))
		}
	}
	return errors.Join(errs...)
}

// compensateAfterSaveFailure rolls back routed secrets and the underlying
// cloud resource after an apply-mode failure where the just-mutated resource is
// known to be newly created or replacement-created. Uses a fresh 30-second
// context: the apply context may already be canceled (operator Ctrl-C), but
// compensation must proceed to avoid orphaning cloud resources + routed
// secrets.
func compensateAfterSaveFailure(
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	hydrated map[string]string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []error
	if driver == nil {
		errs = append(errs, errors.New("driver.Delete unavailable"))
	} else {
		ref := interfaces.ResourceRef{Name: rs.Name, Type: rs.Type, ProviderID: rs.ProviderID}
		if delErr := driver.Delete(ctx, ref); delErr != nil {
			if rs.ProviderID == "" {
				errs = append(errs, fmt.Errorf("driver.Delete: %w", delErr))
			} else {
				nameRef := interfaces.ResourceRef{Name: rs.Name, Type: rs.Type}
				if nameDelErr := driver.Delete(ctx, nameRef); nameDelErr != nil {
					errs = append(errs, fmt.Errorf("driver.Delete: %w", errors.Join(delErr, nameDelErr)))
				}
			}
		}
	}
	if provider != nil {
		for secretName := range hydrated {
			if delErr := provider.Delete(ctx, secretName); delErr != nil && !errors.Is(delErr, secrets.ErrNotFound) {
				errs = append(errs, fmt.Errorf("provider.Delete(%s): %w", secretName, delErr))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// compensateAfterValidationFailure rolls back a resource whose output failed
// identity or ProviderID validation. The ProviderID may be malformed, so a
// name-only delete is used only when the ProviderID delete does not
// conclusively succeed.
func compensateAfterValidationFailure(driver interfaces.ResourceDriver, rs interfaces.ResourceState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if driver == nil {
		return errors.New("driver.Delete unavailable")
	}
	var errs []error
	if rs.ProviderID != "" {
		idRef := interfaces.ResourceRef{Name: rs.Name, Type: rs.Type, ProviderID: rs.ProviderID}
		if delErr := driver.Delete(ctx, idRef); delErr != nil {
			if !errors.Is(delErr, interfaces.ErrResourceNotFound) {
				errs = append(errs, fmt.Errorf("driver.Delete(%s): %w", rs.ProviderID, delErr))
			}
		} else {
			return nil
		}
	}
	nameRef := interfaces.ResourceRef{Name: rs.Name, Type: rs.Type}
	if delErr := driver.Delete(ctx, nameRef); delErr != nil {
		if !errors.Is(delErr, interfaces.ErrResourceNotFound) {
			errs = append(errs, fmt.Errorf("driver.Delete(name-only): %w", delErr))
		}
		return errors.Join(errs...)
	}
	return nil
}

func compensateAfterIdentityValidationFailure(driver interfaces.ResourceDriver, spec interfaces.ResourceSpec, out interfaces.ResourceOutput) error {
	var errs []error
	var succeeded bool
	seen := map[string]struct{}{}
	add := func(name, typ string) {
		if name == "" && typ == "" {
			return
		}
		key := name + "\x00" + typ
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		rs := interfaces.ResourceState{Name: name, Type: typ, ProviderID: out.ProviderID}
		if err := compensateAfterValidationFailure(driver, rs); err != nil {
			errs = append(errs, err)
		} else {
			succeeded = true
		}
	}
	add(out.Name, out.Type)
	add(spec.Name, spec.Type)
	if succeeded {
		return nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// loadSecretsProviderForRouting returns the configured secrets.Provider
// for this apply run, or (nil, nil) when secretsCfg is absent (the
// caller's downstream sensitive.Route will hard-fail if any driver
// emits sensitive outputs).
func loadSecretsProviderForRouting(cfgFile string) (secrets.Provider, error) {
	if cfgFile == "" {
		return nil, nil
	}
	cfg, err := parseSecretsConfig(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("parse secrets config for sensitive routing: %w", err)
	}
	if cfg == nil {
		return nil, nil
	}
	return resolveSecretsProvider(cfg)
}

// hasSensitiveOutputs reports whether r has any Sensitive[k]==true entry.
func hasSensitiveOutputs(r *interfaces.ResourceOutput) bool {
	for _, v := range r.Sensitive {
		if v {
			return true
		}
	}
	return false
}

// sensitiveKeysFor returns the sorted list of keys with Sensitive[k]==true.
func sensitiveKeysFor(r *interfaces.ResourceOutput) []string {
	var keys []string
	for k, v := range r.Sensitive {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// requireSecretsProviderForSensitiveOutputs returns an error if any of the
// resources in result has sensitive outputs but provider is nil. Surfaced
// before the per-resource persistence loop so an apply with sensitive
// drivers and no provider fails fast with a complete diagnostic instead
// of partial state writes.
func requireSecretsProviderForSensitiveOutputs(provider secrets.Provider, result *interfaces.ApplyResult) error {
	if provider != nil || result == nil {
		return nil
	}
	for i := range result.Resources {
		if hasSensitiveOutputs(&result.Resources[i]) {
			return fmt.Errorf(
				"secrets.Provider not configured but driver emitted sensitive outputs (resource %q has Sensitive keys %v); add `secrets:` block to your config or use `secrets: { provider: env }`",
				result.Resources[i].Name, sensitiveKeysFor(&result.Resources[i]))
		}
	}
	return nil
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
//
// The same-process hydrated routed-secret map (sensitive.Route output) is
// accumulated across provider groups and returned to the caller so the
// post-apply syncInfraOutputSecrets step can rehydrate placeholders without
// having to re-Get from a write-only secrets provider (e.g. GitHub Actions
// secrets, where Set succeeds but Get returns ErrUnsupported). Returns an
// empty (non-nil) map when no driver emitted sensitive outputs.
func applyFromPrecomputedPlan(ctx context.Context, plan interfaces.IaCPlan, cfgFile, envName string) (map[string]string, error) { //nolint:cyclop
	// runHydrated accumulates routed-secret values across all provider groups
	// in this plan. Returned to the caller so the post-apply
	// syncInfraOutputSecrets step can rehydrate placeholders without re-Get
	// from write-only secrets providers. Mirrors applyInfraModules's pattern.
	runHydrated := make(map[string]string)

	// Load full config to resolve iac.provider module definitions.
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return runHydrated, fmt.Errorf("load config: %w", err)
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
		return runHydrated, fmt.Errorf("open state store: %w", storeErr)
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
		moduleRef := resolveIaCProviderRef(action.Resource.Config)
		// delete actions from ComputePlan carry an empty Resource.Config — the
		// provider ref must be recovered from the recorded current state instead.
		if moduleRef == "" && action.Current != nil {
			moduleRef = action.Current.ProviderRef
			if moduleRef == "" {
				moduleRef = resolveIaCProviderRef(action.Current.AppliedConfig)
			}
		}
		if moduleRef == "" {
			return runHydrated, fmt.Errorf("plan action for %q: missing 'iac_provider' or 'provider' field in resource config (delete actions require a current state record)", action.Resource.Name)
		}
		if _, exists := groups[moduleRef]; !exists {
			def, ok := providerDefs[moduleRef]
			if !ok {
				return runHydrated, fmt.Errorf("plan action for %q references iac.provider module %q (resolved from iac_provider/provider field) which is not declared as an iac.provider module", action.Resource.Name, moduleRef)
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
			return runHydrated, fmt.Errorf("provider %q (%s): load provider: %w", moduleRef, g.provType, err)
		}
		applyErr := applyPrecomputedPlanWithStore(ctx, groupPlan, provider, g.provType, store, os.Stderr, envName, cfgFile, runHydrated)
		if closer != nil {
			if cerr := closer.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", g.provType, cerr)
			}
		}
		if applyErr != nil {
			return runHydrated, fmt.Errorf("provider %q: %w", moduleRef, applyErr)
		}
	}

	n := len(plan.Actions)
	if n == 1 {
		fmt.Printf("applied 1 action from plan\n")
	} else {
		fmt.Printf("applied %d actions from plan\n", n)
	}
	return runHydrated, nil
}

// applyPrecomputedPlanWithStore executes a pre-computed plan group via
// provider.Apply and persists state for each provisioned resource. It is the
// precomputed-plan counterpart of applyWithProviderAndStore, skipping
// ComputePlan / adoptExistingResources entirely.
func applyPrecomputedPlanWithStore(ctx context.Context, plan interfaces.IaCPlan, provider interfaces.IaCProvider, providerType string, store infraStateStore, w io.Writer, envName string, cfgFile string, hydratedOut map[string]string) error {
	if store == nil {
		store = &noopStateStore{}
	}
	secretsProvider, secretsErr := loadSecretsProviderForRouting(cfgFile)
	if secretsErr != nil {
		return secretsErr
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
	// v2 is the only supported dispatch per ADR 0024 + workflow#699.
	hooks := statePersistenceHooks(store, secretsProvider, provider, providerType, plan.ID, hydratedOut)
	result, err := applyV2ApplyPlanWithHooksFn(ctx, provider, &plan, hooks)
	if result != nil {
		printDriftReportIfAny(w, result)
	}
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
		if len(result.Errors) > 0 {
			msgs := make([]string, 0, len(result.Errors))
			for _, ae := range result.Errors {
				msgs = append(msgs, fmt.Sprintf("%s/%s: %s", ae.Action, ae.Resource, ae.Error))
			}
			finalErr := fmt.Errorf("%d resource(s) failed: %s", len(result.Errors), strings.Join(msgs, "; "))
			emitImageNotInRegistryHint(os.Stderr, finalErr)
			return finalErr
		}
	}
	return nil
}
