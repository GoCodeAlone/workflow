package conformance

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioUpsertOnAlreadyExists asserts the W-3a UpsertSupporter
// recovery contract: when a Create action targets a resource that
// already exists on the cloud (driver.Create returns
// ErrResourceAlreadyExists), and the driver implements
// UpsertSupporter with SupportsUpsert()==true, ApplyPlan recovers by
// resolving the existing resource via Read (with empty ProviderID)
// then calling Update — yielding an idempotent upsert without
// surfacing the original conflict to the caller.
//
// Drivers that do NOT implement UpsertSupporter (or return false)
// must surface the original ErrResourceAlreadyExists unchanged —
// ApplyPlan does NOT silently swallow the conflict. The negative
// assertion (without UpsertSupporter, error propagates) is covered
// by the unit tests in iac/wfctlhelpers/apply_create_test.go; this
// scenario pins the positive recovery path.
//
// Portable assertions:
//
//  1. ApplyPlan returns no top-level error.
//  2. result.Errors carries no entry for the upsert action.
//  3. The action's resolved ProviderID surfaces in
//     result.Resources (a recovery that fell through the conflict
//     without producing observable state would be a regression).
//
// Smoke=false, RequiresCloud=true per design table row 12. Real
// provider plugins exercise the recovery against a pre-existing
// cloud resource; the in-tree self-test uses an upsertingFakeDriver
// (defined in scenarios_test.go) that returns ErrResourceAlreadyExists
// from Create and resolves to a fixed ProviderID via Read.
func scenarioUpsertOnAlreadyExists(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	// Preflight: skip when the provider does not expose a driver for
	// the well-known "infra.compute" probe type. The documented type
	// set (DOCUMENTATION.md) does not include "infra.compute" — it is
	// a conformance-suite-only probe that providers opt into when they
	// surface a compute primitive. Without the driver, ApplyPlan would
	// fail for type-resolution reasons unrelated to the upsert
	// recovery contract this scenario pins.
	//
	// Two skip signals are honored: (a) ResourceDriver returns nil
	// without error, or (b) ResourceDriver returns an error (the
	// canonical idiom — e.g., *platform.ResourceDriverNotFoundError).
	// Either path is read as "provider did not opt in" rather than a
	// hard conformance failure.
	d, err := p.ResourceDriver("infra.compute")
	if err != nil {
		t.Skipf("provider %s does not expose a ResourceDriver for infra.compute "+
			"(upsert-recovery probe is opt-in for providers with a compute primitive): %v",
			p.Name(), err)
		return
	}
	if d == nil {
		t.Skipf("provider %s does not expose a ResourceDriver for infra.compute "+
			"(upsert-recovery probe is opt-in for providers with a compute primitive)",
			p.Name())
		return
	}

	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "create",
				Resource: interfaces.ResourceSpec{
					Name: "existing-resource",
					Type: "infra.compute",
					Config: map[string]any{
						"region": "nyc3",
					},
				},
			},
		},
	}

	result, err := wfctlhelpers.ApplyPlan(context.Background(), p, plan)
	if err != nil {
		t.Fatalf("ApplyPlan returned top-level error: %v", err)
	}
	if result == nil {
		t.Fatal("ApplyPlan returned nil result")
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no per-action errors (UpsertSupporter must recover from "+
			"ErrResourceAlreadyExists via Read+Update); got result.Errors=%+v",
			result.Errors)
	}
	// The recovered upsert should yield an observable resource entry.
	// A regression that swallowed the conflict without dispatching
	// Update would leave result.Resources empty even though Errors is
	// also empty.
	var found bool
	for _, r := range result.Resources {
		if r.Name == "existing-resource" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected upserted resource \"existing-resource\" in "+
			"result.Resources; got %+v", result.Resources)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_UpsertOnAlreadyExists",
		Smoke:         false,
		RequiresCloud: true,
		Run:           scenarioUpsertOnAlreadyExists,
	})
}
