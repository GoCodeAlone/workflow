package wfctlhelpers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

type sensitiveDispatchDriver struct {
	*fakeDriver
	paths          []string
	declarationErr error
	dispatchErr    error
	mutate         bool
	seenPasswords  []string
}

func (d *sensitiveDispatchDriver) SensitiveInputPaths(context.Context) ([]string, error) {
	return d.paths, d.declarationErr
}

func (d *sensitiveDispatchDriver) record(spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.seenPasswords = append(d.seenPasswords, spec.Config["password"].(string))
	if d.mutate {
		spec.Config["password"] = "driver-only-change"
		spec.Config["nested"].(map[string]any)["values"].([]any)[0] = "driver-only-change"
		spec.DependsOn[0] = "driver-only-change"
		spec.Hints.CPU = "driver-only-change"
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "provider-id"}, d.dispatchErr
}

func (d *sensitiveDispatchDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.record(spec)
}

func (d *sensitiveDispatchDriver) Update(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.record(spec)
}

type sensitiveDispatchProvider struct {
	*fakeProvider
	driver interfaces.ResourceDriver
}

type failingDiscoveryProvider struct {
	*sensitiveDispatchProvider
	err error
}

func (p *failingDiscoveryProvider) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	if resourceType == "infra.unavailable" {
		return nil, p.err
	}
	return p.sensitiveDispatchProvider.ResourceDriver(resourceType)
}

func TestApplyPlan_KnownValueAbsentFromLaterDiscoveryErrors(t *testing.T) {
	const marker = "known-private-prior-action-value"
	t.Setenv("WFCTL_SENSITIVE_DISPATCH_TEST", marker)
	for _, source := range []string{"driver", "declaration"} {
		t.Run(source, func(t *testing.T) {
			driver := &sensitiveDispatchDriver{fakeDriver: &fakeDriver{}, paths: []string{"/password"}}
			provider := &failingDiscoveryProvider{sensitiveDispatchProvider: &sensitiveDispatchProvider{fakeProvider: newFakeProvider(), driver: driver}, err: fmt.Errorf("discovery rejected %s", marker)}
			secondType := "infra.unavailable"
			hooks := ApplyPlanHooks{}
			if source == "declaration" {
				secondType = "infra.database"
				hooks.OnResourceApplied = func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
					driver.declarationErr = provider.err
					return nil
				}
			}
			plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
				{Action: "create", Resource: specWithConfig("first", "infra.database", map[string]any{"password": "${WFCTL_SENSITIVE_DISPATCH_TEST}"})},
				{Action: "create", Resource: specWithConfig("second", secondType, map[string]any{"password": "${WFCTL_SENSITIVE_DISPATCH_TEST}"})},
			}}
			result, err := ApplyPlanWithHooks(t.Context(), provider, plan, hooks)
			if err != nil || len(result.Errors) != 1 || len(result.Actions) != 2 || result.Actions[1].Status != interfaces.ActionStatusSkipped {
				t.Fatalf("best-effort discovery semantics lost: result=%+v err=%v", result, err)
			}
			data, err := json.Marshal(result)
			if err != nil || strings.Contains(string(data), marker) {
				t.Fatalf("prior resolved credential leaked in discovery metadata: %s err=%v", data, err)
			}
		})
	}
}

func (p *sensitiveDispatchProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

func TestApplyPlan_DeclarativeHooksAndResolvedCopy(t *testing.T) {
	const marker = "known-private-runtime-value"
	t.Setenv("WFCTL_SENSITIVE_DISPATCH_TEST", marker)
	for _, actionKind := range []string{"create", "update", "replace"} {
		t.Run(actionKind, func(t *testing.T) {
			config := map[string]any{
				"password": "${WFCTL_SENSITIVE_DISPATCH_TEST}",
				"nested":   map[string]any{"values": []any{"${WFCTL_SENSITIVE_DISPATCH_TEST}"}},
			}
			spec := interfaces.ResourceSpec{
				Name: "database", Type: "infra.database", Config: config,
				Hints: &interfaces.ResourceHints{CPU: "2"}, DependsOn: []string{"parent"},
			}
			current := &interfaces.ResourceState{Name: spec.Name, Type: spec.Type, ProviderID: "prior-id", AppliedConfig: config}
			plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: actionKind, Resource: spec, Current: current}}}
			before, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			driver := &sensitiveDispatchDriver{fakeDriver: &fakeDriver{}, paths: []string{"/password", "/nested/values/*"}, mutate: true}
			provider := &sensitiveDispatchProvider{fakeProvider: newFakeProvider(), driver: driver}
			store := &FSStateStore{dir: t.TempDir()}
			var observed []json.RawMessage
			capture := func(action interfaces.PlanAction) {
				data, err := json.Marshal(action)
				if err != nil {
					t.Fatal(err)
				}
				observed = append(observed, data)
			}
			result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
				OnBeforeAction: func(_ context.Context, action interfaces.PlanAction) error { capture(action); return nil },
				OnResourceApplied: func(ctx context.Context, _ interfaces.ResourceDriver, action interfaces.PlanAction, out interfaces.ResourceOutput) error {
					capture(action)
					return store.SaveResource(ctx, interfaces.ResourceState{ID: spec.Name, Name: spec.Name, Type: spec.Type,
						ProviderID: out.ProviderID, AppliedConfig: action.Resource.Config, ConfigHash: platform.ConfigHash(action.Resource.Config)})
				},
				OnResourceDeleted: func(_ context.Context, action interfaces.PlanAction) error { capture(action); return nil },
				OnActionComplete:  func(_ context.Context, action interfaces.PlanAction, _ interfaces.ActionOutcome) { capture(action) },
			})
			if err != nil || len(result.Errors) != 0 {
				t.Fatalf("apply err=%v result=%+v", err, result)
			}
			if len(driver.seenPasswords) != 1 || driver.seenPasswords[0] != marker {
				t.Fatalf("driver must receive the resolved value once: %v", driver.seenPasswords)
			}
			for _, data := range observed {
				if strings.Contains(string(data), marker) || strings.Contains(string(data), "driver-only-change") {
					t.Fatalf("declarative hook received runtime values: %s", data)
				}
			}
			after, err := json.Marshal(plan)
			if err != nil || string(after) != string(before) {
				t.Fatalf("driver changed original plan/current state: %s -> %s (err=%v)", before, after, err)
			}
			persisted, err := os.ReadFile(filepath.Join(store.dir, spec.Name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(persisted), marker) || strings.Contains(string(persisted), "driver-only-change") {
				t.Fatalf("persisted runtime config: %s", persisted)
			}
			state, err := store.GetResource(t.Context(), spec.Name)
			if err != nil || state.ConfigHash != platform.ConfigHash(config) || state.AppliedConfig["password"] != "${WFCTL_SENSITIVE_DISPATCH_TEST}" {
				t.Fatalf("declarative state/hash lost: state=%+v err=%v", state, err)
			}
		})
	}
}

func TestApplyPlan_KnownValueAbsentFromDispatchErrors(t *testing.T) {
	const marker = "known-private-runtime-error-value"
	t.Setenv("WFCTL_SENSITIVE_DISPATCH_TEST", marker)
	driver := &sensitiveDispatchDriver{fakeDriver: &fakeDriver{}, paths: []string{"/password"}, dispatchErr: fmt.Errorf("provider rejected %s", marker)}
	provider := &sensitiveDispatchProvider{fakeProvider: newFakeProvider(), driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: "create", Resource: specWithConfig("database", "infra.database", map[string]any{"password": "${WFCTL_SENSITIVE_DISPATCH_TEST}"})}}}
	var hookOutcome interfaces.ActionOutcome
	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnActionComplete: func(_ context.Context, _ interfaces.PlanAction, outcome interfaces.ActionOutcome) {
			hookOutcome = outcome
		},
	})
	if err != nil || len(result.Errors) != 1 || hookOutcome.Status != interfaces.ActionStatusError {
		t.Fatalf("default error propagation lost: err=%v result=%+v outcome=%+v", err, result, hookOutcome)
	}
	data, err := json.Marshal(struct {
		Result  *interfaces.ApplyResult
		Outcome interfaces.ActionOutcome
	}{result, hookOutcome})
	if err != nil || strings.Contains(string(data), marker) {
		t.Fatalf("runtime credential in diagnostics: %s err=%v", data, err)
	}
	if len(driver.seenPasswords) != 1 || driver.seenPasswords[0] != marker {
		t.Fatal("driver did not receive the real runtime value")
	}
}

func TestApplyPlan_SensitiveRoutingRejectsBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name           string
		value          string
		declarationErr error
		wantDispatch   bool
	}{
		{"literal", "unapproved-private-literal", nil, false},
		{"discovery denied", "${WFCTL_SENSITIVE_DISPATCH_TEST}", errors.New("discovery denied"), false},
		{"legacy unimplemented", "${WFCTL_SENSITIVE_DISPATCH_TEST}", interfaces.ErrProviderMethodUnimplemented, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WFCTL_SENSITIVE_DISPATCH_TEST", "resolved-fixture")
			driver := &sensitiveDispatchDriver{fakeDriver: &fakeDriver{}, paths: []string{"/password"}, declarationErr: tc.declarationErr}
			provider := &sensitiveDispatchProvider{fakeProvider: newFakeProvider(), driver: driver}
			plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: "create", Resource: specWithConfig("database", "infra.database", map[string]any{"password": tc.value})}}}
			result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{})
			if err != nil || (len(driver.seenPasswords) != 0) != tc.wantDispatch || (len(result.Errors) == 0) != tc.wantDispatch {
				t.Fatalf("declaration gate: err=%v result=%+v calls=%d", err, result, len(driver.seenPasswords))
			}
		})
	}
}

func TestApplyPlan_DeclarativeReferencesUseUnchangedSiblingState(t *testing.T) {
	const marker = "known-private-sibling-runtime-value"
	driver := &sensitiveDispatchDriver{fakeDriver: &fakeDriver{}, paths: []string{"/password"}}
	provider := &sensitiveDispatchProvider{fakeProvider: newFakeProvider(), driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: "create", Resource: specWithConfig("consumer", "infra.database", map[string]any{"password": "${parent.password}"})}}}
	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		CurrentState: []interfaces.ResourceState{{Name: "parent", Type: "infra.database", Outputs: map[string]any{"password": "secret_ref://parent_password"}}},
		ResolveSecret: func(_ context.Context, key string) (string, error) {
			if key != "parent_password" {
				t.Fatalf("lookup key=%q", key)
			}
			return marker, nil
		},
	})
	if err != nil || len(result.Errors) != 0 || len(driver.seenPasswords) != 1 || driver.seenPasswords[0] != marker {
		t.Fatalf("sibling resolution failed: err=%v result=%+v driver=%v", err, result, driver.seenPasswords)
	}
	if plan.Actions[0].Resource.Config["password"] != "${parent.password}" {
		t.Fatal("sibling resolution modified plan")
	}
}

func TestApplyPlan_KnownValueAbsentFromFatalHooksPreservesCause(t *testing.T) {
	const marker = "known-private-runtime-hook-value"
	t.Setenv("WFCTL_SENSITIVE_DISPATCH_TEST", marker)
	for _, source := range []string{"applied", "finalize"} {
		t.Run(source, func(t *testing.T) {
			cause := errors.New("durable hook failure")
			hookErr := fmt.Errorf("cannot persist %s: %w", marker, cause)
			driver := &sensitiveDispatchDriver{fakeDriver: &fakeDriver{}, paths: []string{"/password"}}
			provider := &sensitiveDispatchProvider{fakeProvider: newFakeProvider(), driver: driver}
			plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: "create", Resource: specWithConfig("database", "infra.database", map[string]any{"password": "${WFCTL_SENSITIVE_DISPATCH_TEST}"})}}}
			var outcome interfaces.ActionOutcome
			hooks := ApplyPlanHooks{
				OnActionComplete: func(_ context.Context, _ interfaces.PlanAction, observed interfaces.ActionOutcome) {
					outcome = observed
				},
			}
			if source == "applied" {
				hooks.OnResourceApplied = func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
					return hookErr
				}
			} else {
				hooks.OnPlanComplete = func(context.Context) error { return hookErr }
			}
			result, err := ApplyPlanWithHooks(t.Context(), provider, plan, hooks)
			if !errors.Is(err, cause) || strings.Contains(err.Error(), marker) {
				t.Fatalf("redacted fatal error must retain its cause: %v", err)
			}
			data, marshalErr := json.Marshal(struct {
				Result  *interfaces.ApplyResult
				Outcome interfaces.ActionOutcome
			}{result, outcome})
			if marshalErr != nil || strings.Contains(string(data), marker) {
				t.Fatalf("fatal hook diagnostic leaked: %s err=%v", data, marshalErr)
			}
		})
	}
}
