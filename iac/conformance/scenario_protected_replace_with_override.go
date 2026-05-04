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
// Smoke=false, RequiresCloud=true per design table row 9. The
// "RequiresCloud" gate matches the plan literal even though the
// in-process check itself does not touch cloud — real provider
// plugins running this scenario commonly pair it with a live replace
// to confirm the cloud-side outcome of the override.
//
// cfg.Provider is required by Run's validateConfig precondition but
// is intentionally NOT invoked here; the scenario exercises pure
// platform-side gate logic via wfctlhelpers.
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
		RequiresCloud: true,
		Run:           scenarioProtectedReplaceWithOverride,
	})
}
