// Package wfctlhelpers hosts the wfctl-side dispatch helper for v2 IaC
// plugins. wfctl calls [ApplyPlan] when a plugin manifest declares
// iacProvider.computePlanVersion: v2 (see plugin/sdk.IaCProvider). The
// helper iterates plan.Actions, fetches the matching ResourceDriver from
// the provider, and dispatches each action to a per-action sub-function
// (doCreate, doUpdate, doReplace, doDelete).
//
// Lifecycle inside W-3a:
//
//   - T3.1 (this file's ApplyPlan + dispatch + skeleton sub-functions)
//   - T3.1.5 — wraps ApplyPlan with the input-drift postcondition
//   - T3.2 — fills doCreate with UpsertSupporter recovery
//   - T3.3 — fills doUpdate + doDelete (the latent doDelete bug fix)
//   - T3.4 — fills doReplace and populates ApplyResult.ReplaceIDMap
//
// Until W-3b lands the cmd/wfctl dispatch wiring, [ApplyPlan] has no
// in-tree caller — the helper ships in W-3a as foundation only and is
// exercised solely by this package's tests.
//
// # Per-action error-prefix policy
//
// Sub-functions follow a "decompose-then-prefix" rule for the strings
// recorded in [interfaces.ApplyResult].Errors[].Error:
//
//   - doCreate, doUpdate, doDelete pass driver errors through
//     unchanged. The ActionError struct already carries Resource +
//     Action context fields, so a per-kind prefix would be redundant.
//   - doCreate's upsert recovery path prefixes "upsert: " (e.g.,
//     "upsert: read after conflict: ...") because the failure is
//     specifically about the recovery flow, not the original Create.
//   - doReplace prefixes "replace: delete: " or "replace: create: "
//     because a Replace decomposes into two driver calls — without
//     the prefix, an operator reading result.Errors couldn't tell
//     which sub-step failed.
//
// Tests in apply_update_delete_test.go and apply_replace_test.go
// lock this contract via exact-string assertions; future refactors
// that drop or rename a prefix fail loudly.
package wfctlhelpers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"

	"github.com/GoCodeAlone/workflow/iac/inputsnapshot"
	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ApplyPlan dispatches each plan action to the matching ResourceDriver on
// the provider. Per-action errors are recorded on result.Errors and do NOT
// abort the loop — apply best-effort across actions, surface every failure
// for the operator to triage. Context cancellation between actions IS
// respected: when ctx is canceled or its deadline expires, the loop stops
// at the next iteration boundary and returns ctx.Err() as the top-level
// error so a long apply terminates promptly on Ctrl-C / SIGTERM.
//
// At entry ApplyPlan captures result.InitialInputSnapshot by fingerprinting
// every name listed in plan.InputSnapshot through the OS env. After the
// dispatch loop completes — successfully or not — a deferred postcondition
// computes result.InputDriftReport against an apply-time snapshot taken
// through inputsnapshot.NewTolerantEnvProvider (sub-action env unsets are
// preserved, not flagged as drift). The postcondition is wrapped in
// recover() so a buggy env-provider closure cannot corrupt apply results;
// on panic, InputDriftReport is reset to nil and a warning is logged.
//
// The function is concurrency-safe with respect to its inputs: result is
// owned by ApplyPlan for the duration of the call and is not shared with
// the provider or driver implementations.
//
// T3.1 ships the dispatch skeleton; T3.1.5 added the postcondition above;
// T3.2/T3.3/T3.4 fill the per-action sub-functions with their full bodies.
func ApplyPlan(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return applyPlanWithEnvProvider(ctx, p, plan, nil)
}

// applyPlanWithEnvProvider is the same-package test seam used by
// apply_postcondition_test.go to inject a custom apply-time env provider
// into the deferred drift postcondition (e.g., a panicky closure that
// stresses the recover() guard). When applyTimeEnv is nil, the function
// uses inputsnapshot.NewTolerantEnvProvider(plan.InputSnapshot) — the
// production behavior. The seam stays unexported because the only
// sanctioned external entry point is ApplyPlan.
func applyPlanWithEnvProvider(
	ctx context.Context,
	p interfaces.IaCProvider,
	plan *interfaces.IaCPlan,
	applyTimeEnv func(string) (string, bool),
) (*interfaces.ApplyResult, error) {
	inputNames := snapshotKeys(plan.InputSnapshot)
	result := &interfaces.ApplyResult{
		PlanID:               plan.ID,
		InitialInputSnapshot: inputsnapshot.Snapshot(inputNames, inputsnapshot.OSEnvProvider),
	}

	// Deferred drift postcondition — runs unconditionally (success OR
	// per-action failure), wrapped in recover() so a buggy env-provider
	// closure (e.g., one freed mid-flight) cannot corrupt the apply
	// result. On panic, drop the report rather than ship a partial one.
	defer func() {
		defer func() {
			if r := recover(); r != nil {
				result.InputDriftReport = nil
				log.Printf("warning: input-drift postcondition panicked: %v", r)
			}
		}()
		// Resolve the apply-time env provider lazily so the production
		// path's NewTolerantEnvProvider closure isn't constructed when a
		// test override is in play.
		env := applyTimeEnv
		if env == nil {
			// CRITICAL: factory MUST be invoked here, NOT passed by
			// reference (NewTolerantEnvProvider returns the closure;
			// passing the function value would call the factory itself
			// every Snapshot call and short-circuit the planSnapshot
			// closure-capture). The cycle-4 reviewer caught this exact
			// bug-class in the rev3 pseudo-code.
			env = inputsnapshot.NewTolerantEnvProvider(plan.InputSnapshot)
		}
		applyTimeSnap := inputsnapshot.Snapshot(inputNames, env)
		result.InputDriftReport = inputsnapshot.ComputeDrift(plan.InputSnapshot, applyTimeSnap)
	}()

	// syncedOutputs maps resource Name → flat outputs map (with the
	// canonical "id" key shadowed by ProviderID when set). Pre-populated
	// from every action.Current entry so a NEW action can reference
	// existing in-state modules' outputs from action zero — not just
	// modules whose action has already run in this loop. Mutated below
	// after each successful dispatch so subsequent actions see this-apply
	// outputs (the W-5 design's "JIT resolution reads from STATE" path:
	// new outputs are written per-resource on success and become visible
	// to later actions in the same plan).
	syncedOutputs := buildInitialSyncedOutputs(plan.Actions)

	for i := range plan.Actions {
		action := plan.Actions[i]
		// Honor cancellation at the loop boundary. Drivers should also
		// check ctx internally for in-flight work, but the loop check
		// guarantees apply stops between actions even if a driver
		// happens to ignore ctx. The deferred postcondition still runs
		// on early return so InputDriftReport is populated even on a
		// canceled apply.
		if err := ctx.Err(); err != nil {
			return result, err
		}
		// Per-action JIT substitution — resolve ${VAR} / ${MODULE.field}
		// / ${MODULE.id} in action.Resource.Config against
		// result.ReplaceIDMap (this-apply Replace ProviderIDs) and
		// syncedOutputs (state + this-apply prior outputs). On error,
		// record a per-action diagnostic with the canonical "jit
		// substitution:" prefix and SKIP dispatch — the unresolved spec
		// must not reach the driver. The loop continues to the next
		// action (best-effort apply contract). os.LookupEnv is the
		// production env source; nil-safe inside ResolveSpec — refs that
		// only need replaceIDMap / syncedOutputs still resolve.
		resolved, err := jitsubst.ResolveSpec(action.Resource, result.ReplaceIDMap, syncedOutputs, os.LookupEnv)
		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    fmt.Sprintf("jit substitution: %v", err),
			})
			continue
		}
		action.Resource = resolved
		d, err := p.ResourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    fmt.Sprintf("resolve driver: %v", err),
			})
			continue
		}
		// Capture result.Resources length pre-dispatch so we can identify
		// the entry (if any) that this action appended and propagate its
		// outputs into syncedOutputs for subsequent actions. doCreate /
		// doUpdate / doReplace each append on success; doDelete does not.
		preLen := len(result.Resources)
		if err := dispatchAction(ctx, d, action, result); err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    err.Error(),
			})
		}
		if len(result.Resources) > preLen {
			out := result.Resources[len(result.Resources)-1]
			syncedOutputs[out.Name] = flattenOutputs(out)
		}
	}
	return result, nil
}

// buildInitialSyncedOutputs walks plan.Actions once and returns a map of
// every action.Current entry's outputs (keyed by Name). Used to seed the
// JIT substitution map before the dispatch loop begins so a brand-new
// action created in this plan can reference an in-state sibling module's
// outputs even if the sibling has no action of its own — or if its
// action runs later in the loop. Each entry is a flat copy (output
// fields plus the canonical "id" key shadowed with the state-side
// ProviderID).
func buildInitialSyncedOutputs(actions []interfaces.PlanAction) map[string]map[string]any {
	out := make(map[string]map[string]any)
	for i := range actions {
		a := actions[i]
		if a.Current == nil {
			continue
		}
		out[a.Current.Name] = flattenStateOutputs(a.Current)
	}
	return out
}

// flattenStateOutputs returns a flat outputs map for a ResourceState:
// the state's Outputs entries plus a canonical "id" key set to the
// state's ProviderID. The "id" override is intentional — JIT
// substitution treats ${MODULE.id} as the canonical ProviderID
// reference; if a state's Outputs map happens to also have an "id"
// key, the ProviderID still wins so JIT semantics stay predictable.
func flattenStateOutputs(s *interfaces.ResourceState) map[string]any {
	m := make(map[string]any, len(s.Outputs)+1)
	maps.Copy(m, s.Outputs)
	if s.ProviderID != "" {
		m["id"] = s.ProviderID
	}
	return m
}

// flattenOutputs returns a flat outputs map for a freshly-applied
// ResourceOutput. Mirrors flattenStateOutputs but reads from
// ResourceOutput rather than ResourceState — same canonical "id" rule:
// ProviderID shadows any "id" entry in Outputs.
func flattenOutputs(o interfaces.ResourceOutput) map[string]any {
	m := make(map[string]any, len(o.Outputs)+1)
	maps.Copy(m, o.Outputs)
	if o.ProviderID != "" {
		m["id"] = o.ProviderID
	}
	return m
}

// snapshotKeys returns the keys of m as an unordered slice. ComputeDrift
// sorts its output, and Snapshot iterates in any order, so no key sort
// is needed at this stage. Inlined helper to keep the dependency
// surface minimal and avoid pulling in slices/maps at the postcondition
// call site.
func snapshotKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// dispatchAction routes a single PlanAction to the per-kind sub-function.
// An unknown action kind returns an error which ApplyPlan records on
// result.Errors so an operator running a malformed plan sees a per-action
// diagnostic rather than a silent skip.
func dispatchAction(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	switch action.Action {
	case "create":
		return doCreate(ctx, d, action, result)
	case "update":
		return doUpdate(ctx, d, action, result)
	case "replace":
		return doReplace(ctx, d, action, result)
	case "delete":
		return doDelete(ctx, d, action)
	default:
		return fmt.Errorf("unknown action %q", action.Action)
	}
}

// doCreate invokes Create and, on ErrResourceAlreadyExists, attempts an
// idempotent upsert recovery for drivers that opt in via the
// UpsertSupporter interface. The recovery path is:
//
//  1. Probe the driver for UpsertSupporter — if absent (interface
//     assertion fails) or SupportsUpsert()==false, the original
//     conflict surfaces unchanged.
//  2. Read the existing resource by Name + Type (no ProviderID — the
//     driver's Read is responsible for locating by name when ProviderID
//     is empty; SupportsUpsert()==true is the contract that this works).
//  3. Defensive: if Read returns an empty ProviderID, fail with a named
//     diagnostic — Update with an empty ProviderID would route to a
//     create-by-spec path on most drivers, defeating the upsert.
//  4. Update the existing resource with the desired spec, threading
//     the ProviderID found in step 2.
//
// Recovery is single-pass: if the recovery Update itself returns
// ErrResourceAlreadyExists (a driver bug — Update with a known
// ProviderID should not conflict), the second conflict surfaces
// unchanged rather than retriggering the recovery loop.
//
// Error wrapping (in-package contract):
//
//   - Read-after-conflict failures wrap both the original Create error
//     and the Read error via errors.Join, so callers in this package
//     can match either via errors.Is.
//   - The doCreate return value preserves the wrap chain. ApplyPlan's
//     dispatch loop, however, flattens errors to a string in
//     result.Errors[].Error (see [ApplyPlan]) — external callers
//     reading [interfaces.ApplyResult].Errors lose errors.Is matching
//     and must inspect the canonical "upsert: read after conflict:"
//     prefix instead. This boundary is deliberate: ActionError carries
//     the per-resource action context fields the wrap chain otherwise
//     duplicates.
func doCreate(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	out, err := d.Create(ctx, action.Resource)
	if errors.Is(err, interfaces.ErrResourceAlreadyExists) {
		us, ok := d.(interfaces.UpsertSupporter)
		if !ok || !us.SupportsUpsert() {
			return err // no recovery available; surface the conflict
		}
		ref := interfaces.ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
		existing, readErr := d.Read(ctx, ref)
		if readErr != nil {
			return fmt.Errorf("upsert: read after conflict: %w", errors.Join(err, readErr))
		}
		if existing == nil || existing.ProviderID == "" {
			return fmt.Errorf("upsert: resource %q found by name but ProviderID is empty: %w", ref.Name, err)
		}
		ref.ProviderID = existing.ProviderID
		out, err = d.Update(ctx, ref, action.Resource)
	}
	if err == nil && out != nil {
		result.Resources = append(result.Resources, *out)
	}
	return err
}

// doUpdate invokes Update with a ResourceRef carrying action.Current's
// ProviderID (when action.Current is non-nil), appending the driver's
// returned ResourceOutput to result.Resources on success. Driver errors
// pass through unchanged so the caller's per-action error wrapper
// (ApplyPlan's loop body) records them with the canonical action +
// resource fields.
//
// Defensive contract: doUpdate does NOT synthesize a precondition error
// when action.Current is nil — the driver is the authority on what an
// empty ProviderID means. ComputePlan upstream is responsible for never
// emitting an Update without action.Current; if it does, the driver's
// own typed validation surfaces the bug.
func doUpdate(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	ref := refFromAction(action)
	out, err := d.Update(ctx, ref, action.Resource)
	if err != nil {
		return err
	}
	if out != nil {
		result.Resources = append(result.Resources, *out)
	}
	return nil
}

// doReplace decomposes a Replace action into Delete-then-Create on the
// driver and propagates the new ProviderID through
// result.ReplaceIDMap[action.Resource.Name] so the JIT substitution wired
// into ApplyPlan's loop (T5.2) can patch dependent resources whose
// configs reference the replaced resource by name.
//
// # Cascade contract (T5.3)
//
// When a plan has [Replace parent, X dependent] where dependent's
// Config carries ${parent.id}, the cascade lands automatically:
// doReplace's post-Create write to result.ReplaceIDMap completes
// BEFORE the dispatch loop's next iteration calls
// jitsubst.ResolveSpec on the dependent's spec, so the dependent's
// driver call (Create or Replace's post-Delete Create) sees the
// freshly-resolved parent ProviderID. Delete continues to use
// action.Current.ProviderID via refFromAction — JIT substitution does
// NOT alter action.Current, so Replace's Delete still targets the
// pre-Replace cloud resource.
//
// Verified by apply_replace_cascade_test.go.
//
// Failure semantics:
//   - Delete fails → return wrapped "replace: delete: <err>"; Create
//     does NOT run; ReplaceIDMap is NOT populated for this resource.
//   - Delete succeeds, ctx canceled before Create → return wrapped
//     "replace: canceled after delete: <err>"; Create does NOT run;
//     ReplaceIDMap is NOT populated. The half-replaced state is the
//     operator's recovery surface (same as the Create-fails case).
//   - Delete succeeds, Create fails → return wrapped
//     "replace: create: <err>"; ReplaceIDMap stays empty for this
//     resource. Operators inspect the apply log + the empty-for-this-
//     name slot in ReplaceIDMap to know which resources are in a
//     half-replaced state and need manual cloud restoration.
//   - Both succeed → result.Resources gets the new output appended,
//     result.ReplaceIDMap[action.Resource.Name] = new ProviderID. Map
//     is lazily-initialized on first successful Replace so plans with
//     no Replace actions don't carry an empty map through serialisation.
//
// The "replace: ..." prefix is essential because Replace decomposes
// into two driver calls — without it, an operator reading
// result.Errors couldn't tell whether the Delete or the Create failed.
// Other sub-functions (doCreate non-recovery path, doUpdate, doDelete)
// pass driver errors through unchanged.
func doReplace(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	if err := d.Delete(ctx, refFromAction(action)); err != nil {
		return fmt.Errorf("replace: delete: %w", err)
	}
	// Honor cancellation between Delete and Create. Without this guard
	// a Ctrl-C / SIGTERM that arrives mid-Replace would still trigger
	// the Create call, leaving the operator without the cleanest
	// possible interruption point. The half-replaced state is still
	// recoverable (Delete already happened; Create did not, so
	// ReplaceIDMap stays empty) but cancellation propagates fast.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("replace: canceled after delete: %w", err)
	}
	out, err := d.Create(ctx, action.Resource)
	if err != nil {
		return fmt.Errorf("replace: create: %w", err)
	}
	if out != nil {
		result.Resources = append(result.Resources, *out)
		// Lazy-init: only allocate the map when there's an actual
		// entry to record. ApplyResult.ReplaceIDMap stays nil for
		// plans with no Replace actions, which the omitempty JSON tag
		// then drops from the encoded form (covered by
		// TestApplyResult_OmitEmptyContract in interfaces/iac_state_test.go).
		if result.ReplaceIDMap == nil {
			result.ReplaceIDMap = make(map[string]string)
		}
		result.ReplaceIDMap[action.Resource.Name] = out.ProviderID
	}
	return nil
}

// doDelete invokes Delete with a ResourceRef carrying action.Current's
// ProviderID. This closes the latent gap documented in the design
// (DOProvider.Apply has no "case delete" arm today, so wfctl's
// state-prune action silently skipped cloud-resource deletion through
// the v1 dispatch path); under v2 dispatch wfctlhelpers.ApplyPlan
// always invokes the driver's Delete, ensuring state-prune is paired
// with a real cloud-side mutation.
//
// Driver errors pass through unchanged for the caller's per-action
// error wrapping. doDelete does not append to result.Resources — a
// successful delete has no resource to record.
func doDelete(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction) error {
	return d.Delete(ctx, refFromAction(action))
}

// refFromAction builds a ResourceRef from the action's resource identity,
// threading the ProviderID from action.Current when the plan was built
// from existing state. For net-new actions (action.Current == nil) the
// returned ref has an empty ProviderID, matching the contract that
// drivers locate by Name when ProviderID is absent.
//
// Invariant: callers MUST ensure action.Current's Name/Type match
// action.Resource — Replace plans assume same-name same-type. If a future
// plan generator emits a Replace where Current.Name != Resource.Name
// (e.g., a rename across replace), the Delete would target the new name
// with the old ProviderID — likely a "not found" or wrong-resource bug.
// This function does not enforce the invariant; the contract is upstream
// in ComputePlan.
func refFromAction(action interfaces.PlanAction) interfaces.ResourceRef {
	ref := interfaces.ResourceRef{
		Name: action.Resource.Name,
		Type: action.Resource.Type,
	}
	if action.Current != nil {
		ref.ProviderID = action.Current.ProviderID
	}
	return ref
}
