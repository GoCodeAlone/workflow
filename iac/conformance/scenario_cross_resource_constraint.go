package conformance

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioCrossResourceConstraintRejection asserts that providers
// implementing the optional interfaces.ProviderValidator return at
// least one Error-severity PlanDiagnostic when given a plan with a
// cross-resource constraint violation. Closes W-3a/W-4 root-cause D
// — region/cross-resource constraints were previously caught only at
// the provider's API call (apply time), forcing operators to debug
// from a 4xx HTTP response instead of a plan-time diagnostic.
//
// Test surface:
//   - The provider is type-asserted to interfaces.ProviderValidator.
//     If it does not implement the optional interface, the scenario is
//     skipped (per the "MAY" wording on the interface godoc). Providers
//     that DO implement it MUST conform to the assertions below.
//   - The plan carries a single create action for a database whose
//     Config references vpc_ref="missing-vpc" — a name no other
//     PlanAction in the plan creates. A compliant provider validator
//     recognises this as a dangling reference and emits at least one
//     diagnostic with Severity=Error.
//   - Every returned diagnostic must carry a non-empty Message (the
//     R-A10 align renderer surfaces this directly to the operator;
//     empty messages would render as a bare "R-A10 [info] :").
//
// Smoke=false, RequiresCloud=false per design table row 6 — the
// validation is in-process; no cloud roundtrip required.
func scenarioCrossResourceConstraintRejection(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	v, ok := p.(interfaces.ProviderValidator)
	if !ok {
		t.Skipf("provider %s does not implement interfaces.ProviderValidator "+
			"(optional, but required for plan-time cross-resource constraint validation)",
			p.Name())
		return
	}

	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "create",
				Resource: interfaces.ResourceSpec{
					Name: "db",
					Type: "infra.database",
					Config: map[string]any{
						"vpc_ref": "missing-vpc",
					},
				},
			},
		},
	}

	diags := v.ValidatePlan(plan)
	if len(diags) == 0 {
		t.Fatalf("ValidatePlan must return at least one diagnostic for a plan "+
			"with a dangling cross-resource reference; got 0 diagnostics from %s",
			p.Name())
	}

	var hasError bool
	for i, d := range diags {
		if d.Message == "" {
			t.Errorf("PlanDiagnostic[%d].Message must be non-empty (R-A10 renderer "+
				"would emit a bare \"R-A10 [info] :\" line); got %+v", i, d)
		}
		if d.Severity == interfaces.PlanDiagnosticError {
			hasError = true
		}
	}
	if !hasError {
		t.Errorf("expected at least one Severity=Error diagnostic for a cross-"+
			"resource constraint violation; got %+v", diags)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_CrossResourceConstraintRejection",
		Smoke:         false,
		RequiresCloud: false,
		Run:           scenarioCrossResourceConstraintRejection,
	})
}
