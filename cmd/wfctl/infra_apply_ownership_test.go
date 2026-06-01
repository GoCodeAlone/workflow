package main

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestOwnershipGate_MissingOwnerSetsBeforeMutation(t *testing.T) {
	provider := &ownershipProviderStub{owners: map[string]string{}}
	action := ownershipAction("update")
	hooks := wfctlhelpers.ApplyPlanHooks{}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("OnBeforeAction: %v", err)
		}
	})
	if got := provider.setOwners["infra.container_service/app"]; got != "team-a" {
		t.Fatalf("owner set before mutation = %q, want team-a", got)
	}
}

func TestOwnershipGate_MismatchedOwnerBlocks(t *testing.T) {
	provider := &ownershipProviderStub{owners: map[string]string{"infra.container_service/app": "team-b"}}
	action := ownershipAction("update")
	hooks := wfctlhelpers.ApplyPlanHooks{}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		err := hooks.OnBeforeAction(context.Background(), action)
		if err == nil {
			t.Fatal("expected mismatched owner error")
		}
		if !strings.Contains(err.Error(), "owned by \"team-b\"") || !strings.Contains(err.Error(), "--force-owner") {
			t.Fatalf("error = %v, want owner + force hint", err)
		}
	})
}

func TestOwnershipGate_ForceOverridesMismatchedOwner(t *testing.T) {
	provider := &ownershipProviderStub{owners: map[string]string{"infra.container_service/app": "team-b"}}
	action := ownershipAction("update")
	hooks := wfctlhelpers.ApplyPlanHooks{}
	withApplyOwner(t, "team-a", true, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("OnBeforeAction: %v", err)
		}
	})
	if got := provider.setOwners["infra.container_service/app"]; got != "team-a" {
		t.Fatalf("force owner set = %q, want team-a", got)
	}
}

func TestOwnershipGate_CreateSetsOwnerAfterMutationBeforePersist(t *testing.T) {
	provider := &ownershipProviderStub{owners: map[string]string{}}
	action := ownershipAction("create")
	var priorCalled bool
	hooks := wfctlhelpers.ApplyPlanHooks{
		OnResourceApplied: func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
			priorCalled = true
			if got := provider.setOwners["infra.container_service/app"]; got != "team-a" {
				t.Fatalf("owner not set before prior apply hook; got %q", got)
			}
			return nil
		},
	}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if hooks.OnBeforeAction == nil {
			t.Fatal("ownership OnBeforeAction was not installed")
		}
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("create OnBeforeAction should pass: %v", err)
		}
		err := hooks.OnResourceApplied(context.Background(), nil, action, interfaces.ResourceOutput{
			Name:       "app",
			Type:       "infra.container_service",
			ProviderID: "app-1",
		})
		if err != nil {
			t.Fatalf("OnResourceApplied: %v", err)
		}
	})
	if !priorCalled {
		t.Fatal("prior apply hook was not called")
	}
}

func TestOwnershipGate_UpdateDoesNotSetOwnerAfterMutation(t *testing.T) {
	provider := &ownershipProviderStub{owners: map[string]string{"infra.container_service/app": "team-a"}}
	action := ownershipAction("update")
	var priorCalled bool
	hooks := wfctlhelpers.ApplyPlanHooks{
		OnResourceApplied: func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
			priorCalled = true
			if len(provider.setOwners) != 0 {
				t.Fatalf("update post-apply hook set owner unexpectedly: %+v", provider.setOwners)
			}
			return nil
		},
	}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("OnBeforeAction: %v", err)
		}
		if err := hooks.OnResourceApplied(context.Background(), nil, action, interfaces.ResourceOutput{
			Name:       "app",
			Type:       "infra.container_service",
			ProviderID: "app-1",
		}); err != nil {
			t.Fatalf("OnResourceApplied: %v", err)
		}
	})
	if !priorCalled {
		t.Fatal("prior apply hook was not called")
	}
}

func TestOwnershipGate_SkipsDNSDelegation(t *testing.T) {
	provider := &ownershipProviderStub{owners: map[string]string{}}
	action := ownershipAction("update")
	action.Resource.Type = "infra.dns_delegation"
	action.Current.Type = "infra.dns_delegation"
	var priorCalled bool
	hooks := wfctlhelpers.ApplyPlanHooks{
		OnResourceApplied: func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
			priorCalled = true
			return nil
		},
	}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("OnBeforeAction: %v", err)
		}
		if err := hooks.OnResourceApplied(context.Background(), nil, action, interfaces.ResourceOutput{
			Name:       "app",
			Type:       "infra.dns_delegation",
			ProviderID: "delegation-1",
		}); err != nil {
			t.Fatalf("OnResourceApplied: %v", err)
		}
	})
	if !priorCalled {
		t.Fatal("prior apply hook was not called")
	}
	if len(provider.setOwners) != 0 {
		t.Fatalf("dns_delegation should not set owner; got %+v", provider.setOwners)
	}
}

func TestOwnershipGate_SkipsUnsupportedProvider(t *testing.T) {
	provider := &iactest.NoopProvider{ProviderName: "plain"}
	action := ownershipAction("update")
	var priorCalled bool
	hooks := wfctlhelpers.ApplyPlanHooks{
		OnResourceApplied: func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
			priorCalled = true
			return nil
		},
	}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("OnBeforeAction: %v", err)
		}
		if err := hooks.OnResourceApplied(context.Background(), nil, action, interfaces.ResourceOutput{
			Name:       "app",
			Type:       "infra.container_service",
			ProviderID: "app-1",
		}); err != nil {
			t.Fatalf("OnResourceApplied: %v", err)
		}
	})
	if !priorCalled {
		t.Fatal("prior apply hook was not called")
	}
}

func TestOwnershipGate_SkipsTypedAdapterUnsupportedSentinel(t *testing.T) {
	provider := &ownershipUnsupportedProvider{}
	action := ownershipAction("update")
	var priorCalled bool
	hooks := wfctlhelpers.ApplyPlanHooks{
		OnResourceApplied: func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
			priorCalled = true
			return nil
		},
	}
	withApplyOwner(t, "team-a", false, func() {
		wireOwnershipGateIntoHooks(&hooks, provider)
		if err := hooks.OnBeforeAction(context.Background(), action); err != nil {
			t.Fatalf("OnBeforeAction: %v", err)
		}
		if err := hooks.OnResourceApplied(context.Background(), nil, action, interfaces.ResourceOutput{
			Name:       "app",
			Type:       "infra.container_service",
			ProviderID: "app-1",
		}); err != nil {
			t.Fatalf("OnResourceApplied: %v", err)
		}
	})
	if !priorCalled {
		t.Fatal("prior apply hook was not called")
	}
}

func TestOwnerFromFlagOrEnv(t *testing.T) {
	t.Setenv("WORKFLOW_RESOURCE_OWNER", "from-env")
	if got := ownerFromFlagOrEnv("from-flag"); got != "from-flag" {
		t.Fatalf("flag owner = %q", got)
	}
	if got := ownerFromFlagOrEnv(""); got != "from-env" {
		t.Fatalf("env owner = %q", got)
	}
}

func withApplyOwner(t *testing.T, owner string, force bool, fn func()) {
	t.Helper()
	prevOwner, prevForce := currentApplyOwner, currentApplyForceOwner
	currentApplyOwner, currentApplyForceOwner = owner, force
	t.Cleanup(func() {
		currentApplyOwner, currentApplyForceOwner = prevOwner, prevForce
	})
	fn()
}

func ownershipAction(kind string) interfaces.PlanAction {
	return interfaces.PlanAction{
		Action: kind,
		Resource: interfaces.ResourceSpec{
			Name: "app",
			Type: "infra.container_service",
		},
		Current: &interfaces.ResourceState{
			Name:       "app",
			Type:       "infra.container_service",
			ProviderID: "app-1",
		},
	}
}

type ownershipProviderStub struct {
	iactest.NoopProvider
	owners    map[string]string
	setOwners map[string]string
}

func (p *ownershipProviderStub) GetOwner(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOwner, error) {
	owner := p.owners[ownershipKey(ref)]
	return &interfaces.ResourceOwner{Ref: ref, Owner: owner, Source: "tag:managed-by"}, nil
}

func (p *ownershipProviderStub) SetOwner(_ context.Context, ref interfaces.ResourceRef, owner string) error {
	if p.setOwners == nil {
		p.setOwners = map[string]string{}
	}
	p.setOwners[ownershipKey(ref)] = owner
	return nil
}

func (p *ownershipProviderStub) ListOwners(_ context.Context, filter interfaces.OwnerFilter) ([]interfaces.ResourceOwner, error) {
	return []interfaces.ResourceOwner{{
		Ref:    interfaces.ResourceRef{Name: "app", Type: filter.ResourceType, ProviderID: "app-1"},
		Owner:  filter.Owner,
		Source: "tag:managed-by",
	}}, nil
}

func ownershipKey(ref interfaces.ResourceRef) string {
	return ref.Type + "/" + ref.Name
}

type ownershipUnsupportedProvider struct {
	iactest.NoopProvider
}

func (p *ownershipUnsupportedProvider) GetOwner(context.Context, interfaces.ResourceRef) (*interfaces.ResourceOwner, error) {
	return nil, interfaces.ErrProviderMethodUnimplemented
}

func (p *ownershipUnsupportedProvider) SetOwner(context.Context, interfaces.ResourceRef, string) error {
	return interfaces.ErrProviderMethodUnimplemented
}

func (p *ownershipUnsupportedProvider) ListOwners(context.Context, interfaces.OwnerFilter) ([]interfaces.ResourceOwner, error) {
	return nil, interfaces.ErrProviderMethodUnimplemented
}
