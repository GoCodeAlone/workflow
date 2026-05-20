// Package wfctlhelpers hosts the wfctl-side dispatch helper for v2 IaC
// plugins. wfctl calls [ApplyPlanWithHooks] (or the legacy [ApplyPlan]) when
// a plugin manifest declares iacProvider.computePlanVersion: v2 (see
// plugin/sdk.IaCProvider). The helper iterates plan.Actions, fetches the
// matching ResourceDriver from the provider, and dispatches each action to
// a per-action sub-function (doCreate, doUpdate, doReplace, doDelete).
//
// # Action lifecycle versions (workflow#640 migration)
//
// Two callers can drive plan execution:
//
//   - [ApplyPlan] (legacy, marked Deprecated) — empty-hooks equivalent
//     of [ApplyPlanWithHooks]. State persistence happens at whole-plan
//     completion only.
//
//   - [ApplyPlanWithHooks] (v2, recommended) — caller-supplied per-action
//     OnResourceApplied / OnResourceDeleted hooks fire at each successful
//     cloud-mutation boundary. Required for #640's invariants.
//
// See docs/migrations/2026-05-16-v2-lifecycle-phase1-inventory.md and
// decisions/0040-v2-action-lifecycle-provider-compatibility.md for the
// migration contract; ApplyPlan will be removed in Phase 5.
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
	"strings"

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
//
// ApplyPlanHooks are optional callbacks invoked immediately after a plan action
// successfully mutates cloud-side state. Hooks let wfctl persist state at the
// action boundary instead of waiting for the whole plan to finish.
type ApplyPlanHooks struct {
	OnResourceApplied func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error
	OnResourceDeleted func(context.Context, interfaces.PlanAction) error
	// OnPlanComplete fires once after the per-action loop reaches its
	// natural success-exit return at the end of
	// applyPlanWithEnvProviderAndHooks, i.e., when the outer function is
	// about to return (result, nil). Used by the DO plugin's deferred-
	// flush integration via IaCProviderFinalizer.FinalizeApply RPC.
	//
	// Does NOT fire on:
	//   - The preflightProviderOwnedReplaceWithDeleteHooks early-return —
	//     loopReached=false, no cloud work happened.
	//   - The per-action loop's `if fatalErr != nil { return ... }`
	//     early-return — outer err != nil. v1 semantic preservation per
	//     cycle-1 plan-review C-3: DOProvider.Apply skips deferred-flush
	//     when wfctlhelpers.ApplyPlan returns a top-level err (the
	//     `if err != nil { return ... }` guard in DOProvider.Apply
	//     immediately after the ApplyPlan call in
	//     workflow-plugin-digitalocean internal/provider.go).
	//   - The post-loop length-invariant check that compares
	//     len(result.Actions) against len(plan.Actions) — outer err != nil.
	//
	// DOES fire on per-action driver-error paths (best-effort; driver
	// errors append to result.Errors but do NOT set fatalErr; the loop
	// continues and reaches the natural success-exit return). Mirrors v1
	// DOProvider.Apply behavior where the deferred-flush ran whenever
	// the wrapped Apply returned nil err, regardless of per-action
	// result.Errors entries.
	//
	// Per workflow#695 Phase 2.5 / ADR 0024 / ADR 0040.
	OnPlanComplete func(context.Context) error
}

// ApplyPlanWithHooks is ApplyPlan plus action-boundary hooks for callers that
// need durable side effects as each cloud mutation succeeds.
func ApplyPlanWithHooks(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan, hooks ApplyPlanHooks) (*interfaces.ApplyResult, error) {
	return applyPlanWithEnvProviderAndHooks(ctx, p, plan, nil, hooks)
}

// applyPlanWithEnvProvider is the same-package test seam used by
// apply_postcondition_test.go to inject a custom apply-time env provider
// into the deferred drift postcondition (e.g., a panicky closure that
// stresses the recover() guard). When applyTimeEnv is nil, the function
// uses inputsnapshot.NewTolerantEnvProvider(plan.InputSnapshot) — the
// production behavior.
//
// Production callers use ApplyPlanWithHooks. This seam is retained
// purely so the postcondition test can inject a panicky env-provider.
func applyPlanWithEnvProvider(
	ctx context.Context,
	p interfaces.IaCProvider,
	plan *interfaces.IaCPlan,
	applyTimeEnv func(string) (string, bool),
) (*interfaces.ApplyResult, error) {
	return applyPlanWithEnvProviderAndHooks(ctx, p, plan, applyTimeEnv, ApplyPlanHooks{})
}

func applyPlanWithEnvProviderAndHooks(
	ctx context.Context,
	p interfaces.IaCProvider,
	plan *interfaces.IaCPlan,
	applyTimeEnv func(string) (string, bool),
	hooks ApplyPlanHooks,
) (result *interfaces.ApplyResult, err error) {
	// loopReached is set to true immediately before the per-action loop
	// opens (below). The deferred OnPlanComplete closure short-circuits
	// when loopReached=false so pre-loop preflight failures skip finalize
	// (no cloud work happened). On loop-reached exits, the closure
	// additionally gates on err == nil per cycle-1 plan-review C-3 v1
	// semantic preservation — see the ApplyPlanHooks.OnPlanComplete
	// godoc above.
	var loopReached bool
	defer func() {
		if !loopReached || hooks.OnPlanComplete == nil {
			return
		}
		if err != nil {
			// v1 semantic preservation: outer-error exits (the per-action
			// loop's `if fatalErr != nil { return ... }` early-return and
			// the post-loop length-invariant check) skip finalize,
			// matching DOProvider.Apply's "return without flushing on
			// top-level err" behavior (the `if err != nil { return ... }`
			// guard immediately after the wrapped ApplyPlan call in
			// workflow-plugin-digitalocean internal/provider.go).
			return
		}
		// Symmetry with the drift defer below: OnPlanComplete is a
		// caller-provided closure (wired by wfctl in Task 5). A panic
		// inside it would propagate past the defer chain and prevent
		// the caller from observing result/err, so wrap with recover()
		// and convert the panic into a finalize-attributed err entry.
		var hookErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					hookErr = fmt.Errorf("OnPlanComplete panicked: %v", r)
				}
			}()
			hookErr = hooks.OnPlanComplete(ctx)
		}()
		if hookErr != nil {
			// Append per-driver-attribution entry so callers iterating
			// result.Errors see the finalize-attributed failure
			// distinctly from per-action driver errors. Pass the raw
			// hookErr.Error() — the structured Resource="<plan-finalize>"
			// + Action="finalize" fields already carry the attribution;
			// a "plan finalize:" string prefix here would double-attribute
			// when callers format as "<Resource>/<Action>: <Error>".
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: "<plan-finalize>",
				Action:   "finalize",
				Error:    hookErr.Error(),
			})
			// Outer err carries the "plan finalize:" prefix because the
			// outer-err caller path lacks the structured Resource/Action
			// fields — the prefix supplies that missing attribution
			// inline. errors.Is round-trips on the wrapped hookErr.
			err = fmt.Errorf("plan finalize: %w", hookErr)
		}
	}()

	deleteHookActive := hooks.OnResourceDeleted != nil
	inputNames := snapshotKeys(plan.InputSnapshot)
	result = &interfaces.ApplyResult{
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

	if deleteHookActive {
		if err := preflightProviderOwnedReplaceWithDeleteHooks(p, plan); err != nil {
			return result, err
		}
	}

	// loopReached gates the deferred OnPlanComplete invocation. Set
	// BEFORE the loop opens so a zero-iteration (empty Actions) plan
	// still triggers finalize — matches v1 DOProvider.Apply behavior
	// where the deferred-flush fires regardless of plan length (stale
	// queued state from prior runs is flushed even when the current
	// plan has no actions).
	loopReached = true

	for i := range plan.Actions {
		action := plan.Actions[i]
		// Phase 2 (workflow#640 + ADR 0040 invariant 1): per-PlanAction
		// ActionOutcome MUST be appended to result.Actions exactly once,
		// regardless of which continue / fatal-return branch the iteration
		// took. Cycle-1 plan-review C-1 caught the false-positive where
		// jit-error / driver-resolve-error continue paths skip the append
		// and the post-loop length-assert mis-fires. The deferred-closure
		// pattern below records the outcome unconditionally on every exit
		// from the inner func; the surrounding for loop then bubbles
		// fatalErr (hook failures, ctx cancellation) to the caller.
		var iterErr error                      // best-effort error: action failed, continue
		var iterStatus interfaces.ActionStatus // Phase 2.3 (workflow#698): assigned at each error site
		var fatalErr error                     // hook/ctx error: stop the whole apply

		func() {
			defer func() {
				// Phase 2.3 (workflow#698): success-path default — if no
				// error site assigned a phase-specific status, the action
				// completed cleanly. Otherwise iterStatus was set by the
				// path that raised iterErr to the correct phase-specific
				// value (SKIPPED / Error / DeleteFailed / CompensationFailed).
				if iterErr == nil {
					iterStatus = interfaces.ActionStatusSuccess
				}
				errStr := ""
				if iterErr != nil {
					errStr = iterErr.Error()
				}
				result.Actions = append(result.Actions, interfaces.ActionOutcome{
					//nolint:gosec // ActionIndex is loop counter bound by len(plan.Actions); G115 false positive.
					ActionIndex: uint32(i),
					Status:      iterStatus,
					Error:       errStr,
				})
			}()

			// Honor cancellation at the loop boundary. Drivers should also
			// check ctx internally for in-flight work, but the loop check
			// guarantees apply stops between actions even if a driver
			// happens to ignore ctx. The deferred postcondition still runs
			// on early return so InputDriftReport is populated even on a
			// canceled apply. Phase 2.3 (#698): ctx-cancel is pre-dispatch
			// — action was never attempted; cloud-side state unchanged.
			if err := ctx.Err(); err != nil {
				iterErr = err
				iterStatus = statusForPreDispatchSkip()
				fatalErr = err
				return
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
			// only need replaceIDMap / syncedOutputs still resolve. Phase 2.3
			// (#698): JIT-fail is pre-dispatch — no driver call yet.
			resolved, err := jitsubst.ResolveSpec(action.Resource, result.ReplaceIDMap, syncedOutputs, os.LookupEnv)
			if err != nil {
				result.Errors = append(result.Errors, interfaces.ActionError{
					Resource: action.Resource.Name,
					Action:   action.Action,
					Error:    fmt.Sprintf("jit substitution: %v", err),
				})
				iterErr = fmt.Errorf("jit substitution: %v", err)
				iterStatus = statusForPreDispatchSkip()
				return
			}
			action.Resource = resolved
			// Phase 2.3 (#698): driver-resolve-fail is pre-dispatch — no
			// driver method has been called yet.
			d, err := p.ResourceDriver(action.Resource.Type)
			if err != nil {
				result.Errors = append(result.Errors, interfaces.ActionError{
					Resource: action.Resource.Name,
					Action:   action.Action,
					Error:    fmt.Sprintf("resolve driver: %v", err),
				})
				iterErr = fmt.Errorf("resolve driver: %v", err)
				iterStatus = statusForPreDispatchSkip()
				return
			}
			// Capture result.Resources length pre-dispatch so we can identify
			// the entry (if any) that this action appended and propagate its
			// outputs into syncedOutputs for subsequent actions. doCreate /
			// doUpdate / doReplace each append on success; doDelete does not.
			preLen := len(result.Resources)
			actionHooks := hooks
			actionHooks.OnResourceDeleted = func(ctx context.Context, action interfaces.PlanAction) error {
				if hooks.OnResourceDeleted != nil {
					if err := hooks.OnResourceDeleted(ctx, action); err != nil {
						return err
					}
				}
				delete(syncedOutputs, action.Resource.Name)
				return nil
			}
			if err := dispatchAction(ctx, d, action, result, actionHooks, deleteHookActive); err != nil {
				var hookErr hookDispatchError
				if errors.As(err, &hookErr) {
					// Phase 2.3 (#698): hookDispatchError wraps a hook
					// (typically driver-layer Delete hook) that ran AFTER
					// the cloud-side action — cloud-side work IS done;
					// hook failure is post-hook semantically.
					fatalErr = fmt.Errorf("%s/%s: %w", action.Resource.Type, action.Resource.Name, hookErr.err)
					iterErr = hookErr.err
					iterStatus = statusForPostHookFailure()
					return
				}
				// Phase 2.3 (#698): generic dispatch error — driver's
				// Create/Update/Delete RPC returned err. Action attempted;
				// cloud-side state may be partially mutated.
				result.Errors = append(result.Errors, interfaces.ActionError{
					Resource: action.Resource.Name,
					Action:   action.Action,
					Error:    err.Error(),
				})
				iterErr = err
				iterStatus = statusForDispatchError(action.Action)
				return
			}
			if action.Action == "delete" {
				// Phase 2.3 (#698): post-delete-hook ran AFTER cloud-side
				// delete succeeded — cloud-side work IS done; hook failure
				// is post-hook semantically.
				if err := actionHooks.OnResourceDeleted(ctx, action); err != nil {
					fatalErr = fmt.Errorf("%s/%s: post-delete hook: %w", action.Resource.Type, action.Resource.Name, err)
					iterErr = err
					iterStatus = statusForPostHookFailure()
					return
				}
			}
			if len(result.Resources) > preLen {
				out := result.Resources[len(result.Resources)-1]
				out = fillMissingOutputIdentity(action.Resource, out)
				result.Resources[len(result.Resources)-1] = out
				if hooks.OnResourceApplied != nil {
					// Phase 2.3 (#698): post-apply-hook ran AFTER cloud-side
					// create/update succeeded — cloud-side work IS done;
					// hook failure is post-hook semantically.
					if err := hooks.OnResourceApplied(ctx, d, action, out); err != nil {
						fatalErr = fmt.Errorf("%s/%s: post-apply hook: %w", action.Resource.Type, action.Resource.Name, err)
						iterErr = err
						iterStatus = statusForPostHookFailure()
						return
					}
				}
				syncedOutputs[out.Name] = flattenOutputs(out)
			}
		}()

		if fatalErr != nil {
			// Early-return path: ActionOutcome for the offending action
			// has already been appended by the deferred closure; the
			// post-loop length-assert is skipped on fatal exits (length
			// will be < len(plan.Actions), correctly so).
			return result, fatalErr
		}
	}

	// Phase 2 engine invariant (workflow#640 + ADR 0040 invariant 1): on
	// a normally-completed loop, len(result.Actions) MUST equal
	// len(plan.Actions). Length validation lives engine-side here, where
	// it always has — the previous reference to a wfctl-side
	// applyResultFromPB decoder is moot post-workflow#699 (the v1
	// plugin-Apply dispatch path is gone).
	if len(result.Actions) != len(plan.Actions) {
		return result, fmt.Errorf("internal: ApplyPlanWithHooks produced %d ActionOutcomes for %d plan actions (engine invariant violation per ADR 0040)", len(result.Actions), len(plan.Actions))
	}

	return result, nil
}

// Phase 2.3 (workflow#698): replaced single mapDispatchErrToStatus with
// 3 phase-specific helpers — each per-action exit path now assigns its
// status directly from the call site (clearer than late-mapping in defer).
// The single-helper version conflated pre-dispatch failures (ctx-cancel /
// JIT-fail / driver-resolve-fail) and post-hook failures with
// dispatch-level errors, breaking ADR 0040 invariant 2 (failed-delete
// preservation requires distinguishing "delete dispatch failed" from
// "delete never attempted").

// statusForPreDispatchSkip returns SKIPPED — action was never attempted
// at the driver. Used for ctx-cancel, JIT-substitution-fail, and
// driver-resolve-fail paths. Cloud-side state unchanged from pre-apply.
// Per workflow#698 Phase 2.3.
func statusForPreDispatchSkip() interfaces.ActionStatus {
	return interfaces.ActionStatusSkipped
}

// statusForDispatchError returns Error (non-delete) or DeleteFailed (delete).
// Used when driver's Create/Update/Delete RPC returned an error. Action was
// attempted; cloud-side state may be partially mutated. For delete actions,
// DELETE_FAILED instructs wfctl to preserve state.
// Per workflow#698 Phase 2.3 (extracted from prior mapDispatchErrToStatus).
func statusForDispatchError(actionType string) interfaces.ActionStatus {
	if actionType == "delete" {
		return interfaces.ActionStatusDeleteFailed
	}
	return interfaces.ActionStatusError
}

// statusForPostHookFailure returns COMPENSATION_FAILED — driver succeeded
// but wfctl-side hook (OnResourceApplied / OnResourceDeleted) failed (or
// the driver's own hook layer returned hookDispatchError after touching
// cloud-side state). Cloud-side work IS done; operator must verify state;
// may need manual compensation. State preservation required regardless of
// action type.
// Per workflow#698 Phase 2.3.
func statusForPostHookFailure() interfaces.ActionStatus {
	return interfaces.ActionStatusCompensationFailed
}

func preflightProviderOwnedReplaceWithDeleteHooks(p interfaces.IaCProvider, plan *interfaces.IaCPlan) error {
	for i := range plan.Actions {
		action := plan.Actions[i]
		if action.Action != "replace" {
			continue
		}
		d, err := p.ResourceDriver(action.Resource.Type)
		if err != nil {
			return fmt.Errorf("%s/%s: replace preflight resolve driver: %w", action.Resource.Type, action.Resource.Name, err)
		}
		if _, ok := d.(interfaces.ResourceReplacer); ok {
			return fmt.Errorf("%s/%s: replace: driver-owned ResourceReplacer is disabled while delete state hooks are active; state hooks require engine-owned delete-step visibility", action.Resource.Type, action.Resource.Name)
		}
	}
	return nil
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

func fillMissingOutputIdentity(spec interfaces.ResourceSpec, out interfaces.ResourceOutput) interfaces.ResourceOutput {
	if out.Name == "" {
		out.Name = spec.Name
	}
	if out.Type == "" {
		out.Type = spec.Type
	}
	return out
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
func dispatchAction(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult, hooks ApplyPlanHooks, deleteHookActive bool) error {
	switch action.Action {
	case "create":
		return doCreate(ctx, d, action, result)
	case "update":
		return doUpdate(ctx, d, action, result)
	case "replace":
		return doReplace(ctx, d, action, result, hooks, deleteHookActive)
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

// DefaultReplace is the engine's default Replace dispatcher: Delete the
// resource at action.Current, then Create from action.Resource. Public
// so drivers that opt into ResourceReplacer can delegate a particular
// spec to engine-default behavior without a sentinel-error round-trip.
//
// Decomposes a Replace action into Delete-then-Create on the driver and
// propagates the new ProviderID through
// result.ReplaceIDMap[action.Resource.Name] so the JIT substitution wired
// into ApplyPlan's loop (T5.2) can patch dependent resources whose
// configs reference the replaced resource by name.
//
// # Cascade contract (T5.3)
//
// When a plan has [Replace parent, X dependent] where dependent's
// Config carries ${parent.id}, the cascade lands automatically:
// DefaultReplace's post-Create write to result.ReplaceIDMap completes
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
func DefaultReplace(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	return defaultReplaceWithHooks(ctx, d, action, result, ApplyPlanHooks{})
}

func defaultReplaceWithHooks(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult, hooks ApplyPlanHooks) error {
	if err := d.Delete(ctx, refFromAction(action)); err != nil {
		return fmt.Errorf("replace: delete: %w", err)
	}
	if hooks.OnResourceDeleted != nil {
		if err := hooks.OnResourceDeleted(ctx, action); err != nil {
			return hookDispatchError{err: fmt.Errorf("replace: delete hook: %w", err)}
		}
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

type hookDispatchError struct {
	err error
}

func (e hookDispatchError) Error() string {
	return e.err.Error()
}

func (e hookDispatchError) Unwrap() error {
	return e.err
}

// doReplace is the dispatch entry point for Replace actions. It probes
// the driver for the optional ResourceReplacer interface and routes to
// the driver's Replace implementation when present. Drivers that do not
// implement ResourceReplacer fall back to DefaultReplace.
func doReplace(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult, hooks ApplyPlanHooks, deleteHookActive bool) error {
	if deleteHookActive {
		if _, ok := d.(interfaces.ResourceReplacer); ok {
			return hookDispatchError{err: fmt.Errorf("replace: driver-owned ResourceReplacer is disabled while delete state hooks are active; state hooks require engine-owned delete-step visibility")}
		}
	}
	if r, ok := d.(interfaces.ResourceReplacer); ok {
		out, err := r.Replace(ctx, refFromAction(action), action.Resource)
		if err != nil {
			return wrapDriverReplaceError(err)
		}
		return finalizeReplace(out, action.Resource.Name, result)
	}
	return defaultReplaceWithHooks(ctx, d, action, result, hooks)
}

// finalizeReplace runs the post-Replace bookkeeping that DefaultReplace's
// own Create branch does inline. When a driver-owned Replacer returns a
// ResourceOutput, this function appends it to result.Resources and
// populates result.ReplaceIDMap[name] so downstream JIT substitution
// (apply_replace_cascade) sees the new ProviderID. Lazy-init matches
// DefaultReplace's existing map-initialization logic.
func finalizeReplace(out *interfaces.ResourceOutput, name string, result *interfaces.ApplyResult) error {
	if out != nil {
		result.Resources = append(result.Resources, *out)
		if result.ReplaceIDMap == nil {
			result.ReplaceIDMap = make(map[string]string)
		}
		result.ReplaceIDMap[name] = out.ProviderID
	}
	return nil
}

// wrapDriverReplaceError ensures driver-owned Replace errors carry a
// recognizable prefix so operators can attribute failures consistently
// regardless of whether engine-default or driver-owned paths fired.
// Drivers that already wrap with a recognized prefix family pass
// through unchanged; non-conforming drivers get a "replace: driver: "
// backstop applied at the dispatch boundary.
//
// See hasReplaceErrorPrefix for the accepted prefix families.
func wrapDriverReplaceError(err error) error {
	if err == nil {
		return nil
	}
	if hasReplaceErrorPrefix(err) {
		return err
	}
	return fmt.Errorf("replace: driver: %w", err)
}

// hasReplaceErrorPrefix returns true when err.Error() begins with one
// of the recognized prefix families (case-sensitive prefix match):
//   - "replace:"                    (engine-default + the backstop wrapper itself)
//   - "<resource-type> replace "    (driver-owned, e.g. "droplet replace ")
//
// The accepted prefix families are intentionally closed to keep error
// attribution predictable; new families require a workflow-side change.
// Drivers that adopt either family are exempt from re-wrapping.
func hasReplaceErrorPrefix(err error) bool {
	msg := err.Error()
	if strings.HasPrefix(msg, "replace:") {
		return true
	}
	// "<word> replace " form — require at least one non-space char
	// before " replace ", and the substring " replace " in the prefix.
	// We don't bind specific resource types so future plugins can adopt
	// freely.
	idx := strings.Index(msg, " replace ")
	if idx <= 0 {
		return false
	}
	head := msg[:idx]
	// head must be a single token with no spaces or colons — e.g. "droplet",
	// "vpc", "database". Other characters (hyphens, underscores, digits) are
	// accepted intentionally so resource types like "infra.droplet" or
	// "k8s-node" can adopt the prefix freely.
	for _, r := range head {
		if r == ' ' || r == ':' {
			return false
		}
	}
	return true
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
