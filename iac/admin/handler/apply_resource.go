package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// Enforcer is the server-side RBAC interface that the authz.casbin module
// implements. The variadic extra ...string matches the concrete Casbin
// wrapper's method signature (plan-review C-NEW-1). See module.Enforcer.
type Enforcer interface {
	Enforce(sub, obj, act string, extra ...string) (bool, error)
}

// ApplyResource implements the ApplyResource RPC.
//
// Security gates (in order):
//  1. authzError: default-deny if evidence is missing or unchecked.
//  2. Server-side Enforcer: if authz is non-nil, call
//     Enforce(subject,"infra:apply","allow") — the client-body
//     evidence is NOT trusted for RBAC; it is audit-only.
//  3. TOCTOU: re-plan and recompute desired_hash; reject if it
//     diverges from in.DesiredHash. This prevents a stale plan from
//     being applied after config changes.
//  4. ValidateAllowReplaceProtected: reject replace/delete on
//     resources marked protected: true unless in.AllowReplace lists them.
//
// The providers map is keyed by module name; the first entry is used
// (single-provider model for v1.1). When no providers are registered,
// Output.error is set.
func ApplyResource(
	ctx context.Context,
	store interfaces.IaCStateStore, //nolint:revive // nil ok for no-state deploys
	providers map[string]interfaces.IaCProvider,
	authz Enforcer,
	subject string,
	cfg *config.WorkflowConfig, //nolint:revive // reserved for hash parity
	desiredSpecs []interfaces.ResourceSpec,
	in *adminpb.AdminApplyInput,
) (*adminpb.AdminApplyOutput, error) {
	// Gate 1: default-deny.
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminApplyOutput{Error: msg}, ErrAuthzDenied
	}

	// Gate 2: server-side RBAC (NOT the client's evidence.granted_permissions).
	if authz != nil {
		ok, enforceErr := authz.Enforce(subject, "infra:apply", "allow")
		if enforceErr != nil {
			return &adminpb.AdminApplyOutput{Error: "apply: authz enforce error"}, nil //nolint:nilerr
		}
		if !ok {
			// Generic denial — do NOT reflect the authenticated subject in the
			// response body. Subject is captured by the module-layer audit log.
			return &adminpb.AdminApplyOutput{Error: "apply: infra:apply denied"}, ErrAuthzDenied
		}
	}

	if len(providers) == 0 {
		return &adminpb.AdminApplyOutput{Error: "apply: no iac.provider registered"}, nil
	}

	// Select the first provider.
	var prov interfaces.IaCProvider
	for _, p := range providers {
		prov = p
		break
	}

	// Load current state.
	var current []interfaces.ResourceState
	if store != nil {
		var err error
		current, err = store.ListResources(ctx)
		if err != nil {
			return &adminpb.AdminApplyOutput{Error: "apply: list state: " + err.Error()}, nil //nolint:nilerr
		}
	}

	// Gate 3: TOCTOU — recompute hash and compare.
	currentHash := handlerDesiredHash(cfg, desiredSpecs, current)
	if currentHash != in.GetDesiredHash() {
		return &adminpb.AdminApplyOutput{Error: "apply: plan is stale (desired_hash mismatch)"}, nil
	}

	// Compute the plan (same as PlanResource to get the full action set).
	filtered := filterPlanSpecs(desiredSpecs, in.GetAppContext(), "")
	plan, err := prov.Plan(ctx, filtered, current)
	if err != nil {
		return &adminpb.AdminApplyOutput{Error: "apply: plan: " + err.Error()}, nil //nolint:nilerr
	}
	if plan == nil {
		plan = &interfaces.IaCPlan{}
	}

	// Gate 4: replace-protected validation.
	allowSet := make(map[string]struct{}, len(in.GetAllowReplace()))
	for _, n := range in.GetAllowReplace() {
		allowSet[n] = struct{}{}
	}
	if err := handlerValidateAllowReplaceProtected(*plan, allowSet); err != nil {
		return &adminpb.AdminApplyOutput{Error: "apply: " + err.Error()}, nil //nolint:nilerr
	}

	// Execute the plan via the provider's ResourceDriver.
	// Pass store so successful create/update actions are persisted to state.
	result, applyErr := handlerApplyPlan(ctx, prov, plan, store)
	if applyErr != nil {
		return &adminpb.AdminApplyOutput{Error: "apply: " + applyErr.Error()}, nil //nolint:nilerr
	}

	// Map apply result to proto.
	out := &adminpb.AdminApplyOutput{}
	for i := range result.Resources {
		r := &result.Resources[i]
		out.Applied = append(out.Applied, &adminpb.AdminResourceSummary{
			Name:   r.Name,
			Type:   r.Type,
			Status: "active",
		})
	}
	for i := range result.Errors {
		e := &result.Errors[i]
		out.Errors = append(out.Errors, &adminpb.AdminActionError{
			Resource: e.Resource,
			Action:   e.Action,
			Error:    redactCredentials(e.Error),
		})
	}
	return out, nil
}

// handlerApplyPlan is a simplified apply loop that calls
// provider.ResourceDriver + driver.Create/Update/Delete per action.
// When store is non-nil, successful create/update actions persist the
// resulting ResourceState to the state store (assertion (1) from T10 spec).
// Provider errors are collected in result.Errors (best-effort, no early-return).
func handlerApplyPlan(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan, store interfaces.IaCStateStore) (*interfaces.ApplyResult, error) {
	result := &interfaces.ApplyResult{}
	for i := range plan.Actions {
		a := &plan.Actions[i]
		drv, err := p.ResourceDriver(a.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: a.Resource.Name,
				Action:   a.Action,
				Error:    fmt.Sprintf("resolve driver: %s", err.Error()),
			})
			continue
		}
		if drv == nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: a.Resource.Name,
				Action:   a.Action,
				Error:    "no resource driver for type " + a.Resource.Type,
			})
			continue
		}

		switch a.Action {
		case "create":
			out, cerr := drv.Create(ctx, a.Resource)
			switch {
			case cerr != nil:
				result.Errors = append(result.Errors, interfaces.ActionError{Resource: a.Resource.Name, Action: a.Action, Error: cerr.Error()})
			case out != nil:
				result.Resources = append(result.Resources, interfaces.ResourceOutput{Name: out.Name, Type: out.Type, ProviderID: out.ProviderID})
				persistState(ctx, store, a.Resource, out.ProviderID)
			default:
				result.Resources = append(result.Resources, interfaces.ResourceOutput{Name: a.Resource.Name, Type: a.Resource.Type})
				persistState(ctx, store, a.Resource, "")
			}
		case "update":
			ref := interfaces.ResourceRef{Name: a.Resource.Name, Type: a.Resource.Type}
			if a.Current != nil {
				ref.ProviderID = a.Current.ProviderID
			}
			out, uerr := drv.Update(ctx, ref, a.Resource)
			switch {
			case uerr != nil:
				result.Errors = append(result.Errors, interfaces.ActionError{Resource: a.Resource.Name, Action: a.Action, Error: uerr.Error()})
			case out != nil:
				result.Resources = append(result.Resources, interfaces.ResourceOutput{Name: out.Name, Type: out.Type, ProviderID: out.ProviderID})
				persistState(ctx, store, a.Resource, out.ProviderID)
			default:
				result.Resources = append(result.Resources, interfaces.ResourceOutput{Name: a.Resource.Name, Type: a.Resource.Type})
				persistState(ctx, store, a.Resource, ref.ProviderID)
			}
		case "delete", "replace":
			// For delete, the Current carries the ref.
			ref := interfaces.ResourceRef{Name: a.Resource.Name, Type: a.Resource.Type}
			if a.Current != nil {
				ref.ProviderID = a.Current.ProviderID
			}
			if derr := drv.Delete(ctx, ref); derr != nil {
				result.Errors = append(result.Errors, interfaces.ActionError{Resource: a.Resource.Name, Action: a.Action, Error: derr.Error()})
			}
		}
	}
	return result, nil
}

// persistState writes a ResourceState to the store after a successful
// create or update. Errors are silently discarded — the apply itself
// succeeded at the provider level; a state-write failure is surfaced
// on the next read (stale state) rather than rolling back the cloud op.
// nil store is a no-op (test-only / store-less deploys).
func persistState(ctx context.Context, store interfaces.IaCStateStore, spec interfaces.ResourceSpec, providerID string) {
	if store == nil {
		return
	}
	_ = store.SaveResource(ctx, interfaces.ResourceState{
		Name:          spec.Name,
		Type:          spec.Type,
		ProviderID:    providerID,
		AppliedConfig: spec.Config,
	})
}

// handlerValidateAllowReplaceProtected inlines wfctlhelpers.ValidateAllowReplaceProtected
// to avoid the iac/admin/handler → wfctlhelpers → module → iac/admin/handler import cycle.
func handlerValidateAllowReplaceProtected(plan interfaces.IaCPlan, allow map[string]struct{}) error {
	type blocker struct{ name, action string }
	var blockers []blocker
	for i := range plan.Actions {
		a := &plan.Actions[i]
		if a.Action != "replace" && a.Action != "delete" {
			continue
		}
		protected := false
		if a.Resource.Config != nil {
			if p, ok := a.Resource.Config["protected"].(bool); ok && p {
				protected = true
			}
		}
		if !protected && a.Current != nil && a.Current.AppliedConfig != nil {
			if p, ok := a.Current.AppliedConfig["protected"].(bool); ok && p {
				protected = true
			}
		}
		if !protected {
			continue
		}
		if _, ok := allow[a.Resource.Name]; ok {
			continue
		}
		blockers = append(blockers, blocker{name: a.Resource.Name, action: a.Action})
	}
	if len(blockers) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "plan would require destructive action on %d protected resource(s):", len(blockers))
	names := make([]string, 0, len(blockers))
	for _, blk := range blockers {
		fmt.Fprintf(&b, "\n  %s (%s)", blk.name, blk.action)
		names = append(names, blk.name)
	}
	fmt.Fprintf(&b, "\nto authorize, re-run with:\n  --allow-replace=%s", strings.Join(names, ","))
	return errors.New(b.String())
}

// redactCredentials is a minimal guard that replaces DSN-style patterns
// (userinfo@ in URLs) to prevent credential leakage via error messages
// routed through Output.error. Not exhaustive — see the caveat in authz.go.
func redactCredentials(msg string) string {
	// Simple heuristic: replace anything that looks like user:pass@host.
	if !strings.Contains(msg, "@") || !strings.Contains(msg, "://") {
		return msg
	}
	return "(provider error redacted — may contain credentials)"
}
