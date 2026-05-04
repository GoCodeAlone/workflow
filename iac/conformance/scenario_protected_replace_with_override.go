package conformance

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioProtectedReplaceWithOverride is the positive-path companion
// to T7.9: when --allow-replace=<name> covers the protected resource,
// wfctlhelpers.ValidateAllowReplaceProtected returns nil so the apply
// proceeds. Together with T7.9 these two scenarios pin the W-6 gate
// contract end-to-end:
//
//   - Without override → error, names the resource, surfaces hint.
//   - With override   → no error, dispatch unblocks.
//
// Smoke=false, RequiresCloud=false per round-3 review correction. The
// design table row 9 originally listed RequiresCloud=true to align
// with cloud-side replace verification, but this scenario is a pure
// in-process check against wfctlhelpers.ValidateAllowReplaceProtected
// and never calls cfg.Provider(). Marking it cloud-only would silently
// skip one of the non-cloud gate contracts in offline / local runs
// (runWithScenarios skips RequiresCloud=true when LiveCloud=false).
// Real provider plugins that want to additionally confirm the cloud-
// side outcome of the override are encouraged to layer their own
// scenario on top; this one stays in the offline lane so the gate
// logic is exercised on every run.
func scenarioProtectedReplaceWithOverride(t *testing.T, _ Config) {
	t.Helper()

	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "replace",
				Resource: interfaces.ResourceSpec{
					Name: "prod-db",
					Type: "infra.database",
					Config: map[string]any{
						"protected": true,
						"region":    "nyc3",
					},
				},
			},
		},
	}

	allow := map[string]struct{}{"prod-db": {}}
	if err := wfctlhelpers.ValidateAllowReplaceProtected(plan, allow); err != nil {
		t.Errorf("ValidateAllowReplaceProtected must return nil when --allow-replace "+
			"includes the protected resource; got %v", err)
	}

	// Negative cross-check: an allow-list that names a DIFFERENT resource
	// must NOT unblock the protected one. Catches a class of bug where a
	// faulty implementation returns nil whenever any allow entry is set
	// (rather than checking the specific resource name).
	allowOther := map[string]struct{}{"some-other-resource": {}}
	if err := wfctlhelpers.ValidateAllowReplaceProtected(plan, allowOther); err == nil {
		t.Errorf("allow-list naming a different resource must NOT unblock prod-db; " +
			"got nil error (gate must check per-resource name, not just any-allow)")
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_ProtectedReplaceWithOverride",
		Smoke:         false,
		RequiresCloud: false,
		Run:           scenarioProtectedReplaceWithOverride,
	})
}
