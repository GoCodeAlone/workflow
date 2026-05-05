package conformance

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioProtectedReplaceWithoutOverride asserts the W-6 protected-
// replace gate: when a plan carries a replace action whose target is
// annotated `protected: true` and no --allow-replace authorization
// covers it, wfctlhelpers.ValidateAllowReplaceProtected returns a
// non-nil error whose message NAMES the resource AND surfaces the
// canonical copy-paste hint (`--allow-replace=<name>`). Closes
// design root-cause issue G — operators previously had to rebuild
// the plan with `protected: false` to recover, losing the audit
// trail that the resource was once protected.
//
// Smoke=false, RequiresCloud=false per design table row 8 — the gate
// is in-process; no cloud roundtrip required.
//
// cfg.Provider is required by Run's validateConfig precondition but
// is intentionally NOT invoked here; the scenario exercises pure
// platform-side gate logic via wfctlhelpers.
func scenarioProtectedReplaceWithoutOverride(t *testing.T, _ Config) {
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

	err := wfctlhelpers.ValidateAllowReplaceProtected(plan, nil)
	if err == nil {
		t.Fatal("ValidateAllowReplaceProtected must return an error for a protected " +
			"replace action without --allow-replace authorization; got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "prod-db") {
		t.Errorf("error must NAME the protected resource (\"prod-db\"); got %q", msg)
	}
	if !strings.Contains(msg, "--allow-replace=prod-db") {
		t.Errorf("error must surface the copy-paste hint \"--allow-replace=prod-db\" "+
			"(operator runs the same command with the flag to authorize); got %q", msg)
	}
	if !strings.Contains(msg, "(replace)") {
		t.Errorf("error must label the action type as \"(replace)\" so operators can "+
			"distinguish replace blockers from delete blockers; got %q", msg)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_ProtectedReplaceWithoutOverride",
		Smoke:         false,
		RequiresCloud: false,
		Run:           scenarioProtectedReplaceWithoutOverride,
	})
}
