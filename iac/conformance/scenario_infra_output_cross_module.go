package conformance

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/jitsubst"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioInfraOutputCrossModuleResolution asserts the W-5 JIT-
// substitution contract: when Module B's Config contains
// ${moduleA.field} references, jitsubst.ResolveSpec resolves them
// against syncedOutputs at apply time so the driver sees fully-
// substituted values. Closes design issue: cross-module references
// previously had no apply-time resolution path, so dependent modules
// either failed (unresolvable string-with-placeholder) or silently
// substituted empty (os.ExpandEnv-style) — neither correct.
//
// Scenario assertions:
//
//  1. ${moduleA.id} resolves to syncedOutputs["moduleA"]["id"].
//  2. ${moduleA.field} resolves to the matching field value, with the
//     surrounding string preserved (resolution is in-string, not whole-
//     value replacement).
//  3. Static values without ${...} markers pass through unchanged
//     (the deep-copy contract preserves untouched leaves).
//  4. Unresolvable references return a non-nil error and the input
//     spec is returned unmodified (strict contract — partial-state
//     spec must not be returned to callers).
//
// Smoke=false, RequiresCloud=false per round-3 review correction. The
// design table row 7 originally listed RequiresCloud=true, but this
// scenario is a pure in-process check against jitsubst.ResolveSpec
// and never calls cfg.Provider() or any cloud API. Marking it cloud-
// only would silently skip the ${module.field} resolution contract
// in non-cloud runs (runWithScenarios skips RequiresCloud=true when
// LiveCloud=false), reducing offline coverage. The scenario stays in
// the suite's offline lane so local + non-cloud full-suite runs
// observe the JIT contract.
func scenarioInfraOutputCrossModuleResolution(t *testing.T, _ Config) {
	t.Helper()

	specB := interfaces.ResourceSpec{
		Name: "moduleB",
		Type: "infra.database",
		Config: map[string]any{
			"vpc_ref":    "${moduleA.id}",
			"endpoint":   "${moduleA.endpoint}/api",
			"static_val": "no-substitution",
		},
	}

	syncedOutputs := map[string]map[string]any{
		"moduleA": {
			"id":       "vpc-12345",
			"endpoint": "https://vpc.example.com",
		},
	}

	resolved, err := jitsubst.ResolveSpec(specB, nil, syncedOutputs, nil)
	if err != nil {
		t.Fatalf("ResolveSpec failed for valid cross-module references: %v", err)
	}

	if got, want := resolved.Config["vpc_ref"], "vpc-12345"; got != want {
		t.Errorf("vpc_ref = %v, want %q (${moduleA.id} should resolve to syncedOutputs[\"moduleA\"][\"id\"])",
			got, want)
	}
	if got, want := resolved.Config["endpoint"], "https://vpc.example.com/api"; got != want {
		t.Errorf("endpoint = %v, want %q (in-string substitution must preserve surrounding text)",
			got, want)
	}
	if got, want := resolved.Config["static_val"], "no-substitution"; got != want {
		t.Errorf("static_val = %v, want %q (deep-copy must preserve unmarked leaves)",
			got, want)
	}

	// Negative case: an unresolvable reference must error and leave the
	// returned spec untouched. A buggy implementation that returns a
	// half-resolved spec on error would mask the failure for callers
	// that ignore err.
	specBad := interfaces.ResourceSpec{
		Name: "moduleC",
		Type: "infra.app",
		Config: map[string]any{
			"vpc_ref": "${missingModule.id}",
		},
	}
	_, err = jitsubst.ResolveSpec(specBad, nil, syncedOutputs, nil)
	if err == nil {
		t.Errorf("ResolveSpec must return error for ${missingModule.id} reference; got nil")
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_InfraOutputCrossModuleResolution",
		Smoke:         false,
		RequiresCloud: false,
		Run:           scenarioInfraOutputCrossModuleResolution,
	})
}
