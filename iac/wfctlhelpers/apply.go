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
package wfctlhelpers

import (
	"context"
	"fmt"

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
// The function is concurrency-safe with respect to its inputs: result is
// owned by ApplyPlan for the duration of the call and is not shared with
// the provider or driver implementations.
//
// T3.1 ships the dispatch skeleton. T3.1.5 wraps the body with the input-
// drift postcondition; T3.2/T3.3/T3.4 fill the per-action sub-functions
// with their full bodies. Test smoke-coverage verifies dispatch wiring;
// per-action behavior is exercised by the per-task test files.
func ApplyPlan(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	result := &interfaces.ApplyResult{PlanID: plan.ID}
	for _, action := range plan.Actions {
		// Honor cancellation at the loop boundary. Drivers should also
		// check ctx internally for in-flight work, but the loop check
		// guarantees apply stops between actions even if a driver
		// happens to ignore ctx.
		if err := ctx.Err(); err != nil {
			return result, err
		}
		d, err := p.ResourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    fmt.Sprintf("resolve driver: %v", err),
			})
			continue
		}
		if err := dispatchAction(ctx, d, action, result); err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    err.Error(),
			})
		}
	}
	return result, nil
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

// doCreate is the T3.1 skeleton — invokes Create and appends the output
// to result.Resources on success. T3.2 will replace this with the full
// UpsertSupporter recovery path on ErrResourceAlreadyExists.
func doCreate(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	out, err := d.Create(ctx, action.Resource)
	if err != nil {
		return err
	}
	if out != nil {
		result.Resources = append(result.Resources, *out)
	}
	return nil
}

// doUpdate is the T3.1 skeleton — invokes Update with a ResourceRef
// derived from action.Current's ProviderID (when present) and appends the
// output to result.Resources on success. T3.3 will fill in the typed
// pre-condition checks.
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

// doReplace is the T3.1 skeleton — Delete + Create. T3.4 fills in the
// ReplaceIDMap propagation so dependent resources can pick up the new
// ProviderID via JIT substitution in W-5.
func doReplace(ctx context.Context, d interfaces.ResourceDriver, action interfaces.PlanAction, result *interfaces.ApplyResult) error {
	if err := d.Delete(ctx, refFromAction(action)); err != nil {
		return fmt.Errorf("replace: delete: %w", err)
	}
	out, err := d.Create(ctx, action.Resource)
	if err != nil {
		return fmt.Errorf("replace: create: %w", err)
	}
	if out != nil {
		result.Resources = append(result.Resources, *out)
	}
	return nil
}

// doDelete is the T3.1 skeleton — invokes Delete. T3.3 fills in the typed
// pre-condition checks; this skeleton already closes the latent gap noted
// in the design (DOProvider.Apply has no "case delete" today).
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
