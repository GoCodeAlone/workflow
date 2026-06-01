package main

import (
	"context"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// currentApplyOwner is the per-invocation owner identity for generic
// cloud-resource ownership. Empty means the ownership gate is disabled.
var currentApplyOwner string

// currentApplyForceOwner allows an operator to override a mismatched owner for
// one apply. It is ignored unless currentApplyOwner is non-empty.
var currentApplyForceOwner bool

type ownershipGate struct {
	provider interfaces.IaCProvider
	owner    string
	force    bool
}

func ownerFromFlagOrEnv(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("WORKFLOW_RESOURCE_OWNER")
}

func wireOwnershipGateIntoHooks(hooks *wfctlhelpers.ApplyPlanHooks, provider interfaces.IaCProvider) {
	owner := currentApplyOwner
	if owner == "" {
		return
	}
	g := ownershipGate{provider: provider, owner: owner, force: currentApplyForceOwner}
	appendOnBeforeActionHook(hooks, g.beforeAction)

	priorApplied := hooks.OnResourceApplied
	hooks.OnResourceApplied = func(ctx context.Context, driver interfaces.ResourceDriver, action interfaces.PlanAction, out interfaces.ResourceOutput) error {
		if shouldSkipGenericOwnership(action) {
			if priorApplied == nil {
				return nil
			}
			return priorApplied(ctx, driver, action, out)
		}
		ref := interfaces.ResourceRef{Name: out.Name, Type: out.Type, ProviderID: out.ProviderID}
		if ref.Name == "" {
			ref.Name = action.Resource.Name
		}
		if ref.Type == "" {
			ref.Type = action.Resource.Type
		}
		if err := g.setOwner(ctx, ref); err != nil {
			return err
		}
		if priorApplied == nil {
			return nil
		}
		return priorApplied(ctx, driver, action, out)
	}
}

func appendOnBeforeActionHook(hooks *wfctlhelpers.ApplyPlanHooks, next func(context.Context, interfaces.PlanAction) error) {
	if hooks == nil || next == nil {
		return
	}
	prior := hooks.OnBeforeAction
	if prior == nil {
		hooks.OnBeforeAction = next
		return
	}
	hooks.OnBeforeAction = func(ctx context.Context, action interfaces.PlanAction) error {
		if err := prior(ctx, action); err != nil {
			return err
		}
		return next(ctx, action)
	}
}

func (g ownershipGate) beforeAction(ctx context.Context, action interfaces.PlanAction) error {
	if shouldSkipGenericOwnership(action) {
		return nil
	}
	provider, err := g.ownershipProvider()
	if err != nil {
		return err
	}
	ref := ownershipRefForAction(action)
	owner, err := provider.GetOwner(ctx, ref)
	if err != nil {
		if action.Action == "create" && interfaces.IsErrResourceNotFound(err) {
			return nil
		}
		return fmt.Errorf("ownership: read owner for %s/%s: %w", ref.Type, ref.Name, err)
	}
	if owner == nil || owner.Owner == "" {
		if action.Action == "create" {
			return nil
		}
		return g.setOwner(ctx, ref)
	}
	if owner.Owner == g.owner {
		return nil
	}
	if g.force {
		return g.setOwner(ctx, ref)
	}
	return fmt.Errorf("ownership: %s/%s is owned by %q via %s; refusing to apply as %q (rerun with --force-owner to override)",
		ref.Type, ref.Name, owner.Owner, owner.Source, g.owner)
}

func (g ownershipGate) setOwner(ctx context.Context, ref interfaces.ResourceRef) error {
	provider, err := g.ownershipProvider()
	if err != nil {
		return err
	}
	if err := provider.SetOwner(ctx, ref, g.owner); err != nil {
		return fmt.Errorf("ownership: set owner for %s/%s to %q: %w", ref.Type, ref.Name, g.owner, err)
	}
	return nil
}

func (g ownershipGate) ownershipProvider() (interfaces.OwnershipProvider, error) {
	provider, ok := g.provider.(interfaces.OwnershipProvider)
	if !ok {
		return nil, fmt.Errorf("ownership: provider %q does not implement OwnershipProvider", g.provider.Name())
	}
	return provider, nil
}

func ownershipRefForAction(action interfaces.PlanAction) interfaces.ResourceRef {
	ref := interfaces.ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
	if action.Current != nil {
		if action.Current.Name != "" {
			ref.Name = action.Current.Name
		}
		if action.Current.Type != "" {
			ref.Type = action.Current.Type
		}
		ref.ProviderID = action.Current.ProviderID
	}
	return ref
}

func shouldSkipGenericOwnership(action interfaces.PlanAction) bool {
	return action.Resource.Type == "infra.dns" || action.Action == "delete" && action.Current == nil
}
