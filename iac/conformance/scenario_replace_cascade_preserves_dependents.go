package conformance

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioReplaceCascadePreservesDependents asserts the W-5/T5.3
// cascade contract: when a plan includes a Replace of a parent
// resource followed by a Create of a dependent that references the
// parent via ${parent.id}, the apply path's per-action JIT
// substitution sees the post-Delete Create's freshly-minted
// ProviderID via result.ReplaceIDMap, so the dependent's driver
// Create receives the RESOLVED parent ID — never the unresolved
// "${parent.id}" literal.
//
// Closes design root-cause: pre-W-5 the cascade silently substituted
// the stale state ProviderID (or blank string), so dependents
// pointed at a tombstoned parent.
//
// Portable assertions:
//
//  1. ApplyPlan returns no top-level error.
//  2. result.Errors is empty (no per-action error — a regression in
//     JIT would surface here as "${parent.id}: env var not set" or
//     similar from the dependent's Create).
//  3. result.ReplaceIDMap[parent] is non-empty (T3.4's contract:
//     doReplace records the new ProviderID).
//
// Smoke=false, RequiresCloud=true per design table row 11. Real
// provider plugins exercise the cascade against live cloud
// resources; the in-tree self-test uses a fake whose Create returns
// a configured ProviderID for the parent.
func scenarioReplaceCascadePreservesDependents(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	// Preflight: skip when the provider does not expose drivers for
	// both the documented infra.vpc parent AND the conformance-suite-
	// only infra.app dependent. infra.app is not in the published
	// type set (DOCUMENTATION.md); providers opt in when they surface
	// an application primitive (DO AppPlatform, AWS Beanstalk, etc.).
	// Without the driver, ApplyPlan fails for type-resolution reasons
	// unrelated to the cascade contract this scenario pins.
	//
	// Two skip signals are honored per probe: (a) ResourceDriver
	// returns nil without error, or (b) returns an error (the canonical
	// idiom — e.g., *platform.ResourceDriverNotFoundError). Either
	// path is read as "provider did not opt in" rather than a hard
	// conformance failure.
	parent, parentErr := p.ResourceDriver("infra.vpc")
	dependent, dependentErr := p.ResourceDriver("infra.app")
	if parentErr != nil || dependentErr != nil || parent == nil || dependent == nil {
		t.Skipf("provider %s lacks driver(s) for replace-cascade probe "+
			"(infra.vpc=%v err=%v, infra.app=%v err=%v); cascade-"+
			"preserves-dependents is opt-in for providers exposing both primitives",
			p.Name(), parent != nil, parentErr, dependent != nil, dependentErr)
		return
	}

	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "replace",
				Resource: interfaces.ResourceSpec{
					Name: "parent", Type: "infra.vpc",
					Config: map[string]any{"cidr": "10.0.0.0/16"},
				},
				Current: &interfaces.ResourceState{
					Name:       "parent",
					Type:       "infra.vpc",
					ProviderID: "vpc-old",
				},
			},
			{
				Action: "create",
				Resource: interfaces.ResourceSpec{
					Name: "dependent", Type: "infra.app",
					Config: map[string]any{
						"vpc_ref": "${parent.id}",
					},
				},
			},
		},
	}

	result, err := wfctlhelpers.ApplyPlanWithHooks(context.Background(), p, plan, wfctlhelpers.ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("ApplyPlan returned top-level error: %v", err)
	}
	if result == nil {
		t.Fatal("ApplyPlan returned nil result")
		return
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no per-action errors (cascade JIT must resolve "+
			"${parent.id} from result.ReplaceIDMap before dependent Create); "+
			"got result.Errors=%+v", result.Errors)
	}
	if got, ok := result.ReplaceIDMap["parent"]; !ok || got == "" {
		t.Errorf("result.ReplaceIDMap[\"parent\"] must be populated by doReplace "+
			"so subsequent JIT substitution sees the new ProviderID; got %q (full map: %+v)",
			got, result.ReplaceIDMap)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_ReplaceCascadePreservesDependents",
		Smoke:         false,
		RequiresCloud: true,
		Run:           scenarioReplaceCascadePreservesDependents,
	})
}
