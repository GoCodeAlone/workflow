package wfctlhelpers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestApplyPlanWithHooks_PersistsBeforeLaterAction(t *testing.T) {
	driver := &hookOrderingDriver{}
	provider := &hookProvider{driver: driver}
	driver.create = func(spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
		switch spec.Name {
		case "first":
			return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-first"}, nil
		case "second":
			if !driver.hookRan {
				t.Fatal("second action ran before first action hook")
			}
			return nil, errors.New("second failed")
		default:
			t.Fatalf("unexpected resource %q", spec.Name)
			return nil, nil
		}
	}

	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: interfaces.ResourceSpec{Name: "first", Type: "infra.test"}},
		{Action: "create", Resource: interfaces.ResourceSpec{Name: "second", Type: "infra.test"}},
	}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceApplied: func(_ context.Context, _ interfaces.ResourceDriver, action interfaces.PlanAction, out interfaces.ResourceOutput) error {
			if action.Resource.Name == "first" && out.ProviderID == "id-first" {
				driver.hookRan = true
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %d, want 1", len(result.Errors))
	}
	if !driver.hookRan {
		t.Fatal("first action hook did not run")
	}
}

func TestApplyPlanWithHooks_DefaultReplaceDeleteHookRunsBeforeCreateFailure(t *testing.T) {
	driver := &replaceDeleteHookDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.test"},
		Current:  &interfaces.ResourceState{Name: "db", Type: "infra.test", ProviderID: "old-id"},
	}}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(_ context.Context, action interfaces.PlanAction) error {
			if action.Resource.Name == "db" {
				driver.deleteHookRan = true
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %d, want 1", len(result.Errors))
	}
	if !driver.deleteHookRan {
		t.Fatal("replace delete hook did not run before create failure")
	}
}

func TestApplyPlanWithHooks_DefaultReplaceDeleteHookErrorIsTopLevel(t *testing.T) {
	driver := &replaceDeleteHookDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.test"},
		Current:  &interfaces.ResourceState{Name: "db", Type: "infra.test", ProviderID: "old-id"},
	}}}
	hookErr := errors.New("state store down")

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error {
			return hookErr
		},
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("error = %v, want hookErr", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("result.Errors = %+v, want no per-action driver errors for hook failure", result.Errors)
	}
}

func TestApplyPlanWithHooks_FailedDeleteDoesNotRunDeleteHook(t *testing.T) {
	driver := &failedDeleteHookDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "delete",
		Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.test"},
		Current:  &interfaces.ResourceState{Name: "db", Type: "infra.test", ProviderID: "old-id"},
	}}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error {
			driver.deleteHookRan = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %d, want 1", len(result.Errors))
	}
	if driver.deleteHookRan {
		t.Fatal("delete hook ran after failed delete")
	}
}

func TestApplyPlanWithHooks_DeleteRemovesResourceFromLaterJIT(t *testing.T) {
	driver := &deleteThenDependentDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{
			Action:   "delete",
			Resource: interfaces.ResourceSpec{Name: "old", Type: "infra.test"},
			Current: &interfaces.ResourceState{
				Name:       "old",
				Type:       "infra.test",
				ProviderID: "id-old",
			},
		},
		{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name:   "dependent",
				Type:   "infra.test",
				Config: map[string]any{"target": "${old.id}"},
			},
		},
	}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if driver.dependentCreateCalled {
		t.Fatal("dependent create reached driver with stale deleted resource output")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %+v, want one JIT error", result.Errors)
	}
	if !strings.Contains(result.Errors[0].Error, "jit substitution:") || !strings.Contains(result.Errors[0].Error, "old") {
		t.Fatalf("result error = %q, want unresolved old reference", result.Errors[0].Error)
	}
}

type hookOrderingDriver struct {
	iactest.NoopDriver
	create  func(interfaces.ResourceSpec) (*interfaces.ResourceOutput, error)
	hookRan bool
}

func (d *hookOrderingDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if d.create != nil {
		return d.create(spec)
	}
	return d.NoopDriver.Create(ctx, spec)
}

type hookProvider struct {
	iactest.NoopProvider
	driver interfaces.ResourceDriver
}

func (p *hookProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

type replaceDeleteHookDriver struct {
	iactest.NoopDriver
	deleteHookRan bool
}

func (d *replaceDeleteHookDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.NoopDriver.Delete(ctx, ref)
}

func (d *replaceDeleteHookDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if !d.deleteHookRan {
		return nil, errors.New("delete hook had not run")
	}
	return nil, errors.New("create failed")
}

type failedDeleteHookDriver struct {
	iactest.NoopDriver
	deleteHookRan bool
}

func (d *failedDeleteHookDriver) Delete(context.Context, interfaces.ResourceRef) error {
	return errors.New("delete failed")
}

type deleteThenDependentDriver struct {
	iactest.NoopDriver
	dependentCreateCalled bool
}

func (d *deleteThenDependentDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.NoopDriver.Delete(ctx, ref)
}

func (d *deleteThenDependentDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if spec.Name == "dependent" {
		d.dependentCreateCalled = true
		return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-dependent"}, nil
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-" + spec.Name}, nil
}
