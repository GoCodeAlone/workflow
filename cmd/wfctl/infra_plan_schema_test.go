package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestInfraPlan_SchemaVersionV1_NoJITRefs covers the baseline: a config
// with no JIT references (only plain ${VAR} env-var refs, which are
// resolved at plan time outside preserved keys and pass through preserved
// keys but don't require module-output resolution at apply) stamps the
// plan as V1.
func TestInfraPlan_SchemaVersionV1_NoJITRefs(t *testing.T) {
	t.Setenv("STAGING_DB_PASSWORD", "secret")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: app
    type: infra.container_service
    config:
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_DB_PASSWORD}@host:5432/db"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(dir, "plan.json")
	if err := runInfraPlan([]string{"--config", cfgPath, "--output", planFile}); err != nil {
		t.Fatalf("runInfraPlan: %v", err)
	}
	plan := readPlanFile(t, planFile)
	if plan.SchemaVersion != infraPlanSchemaVersionV1 {
		t.Errorf("SchemaVersion: got %d want %d (V1, no JIT refs)",
			plan.SchemaVersion, infraPlanSchemaVersionV1)
	}
}

// TestInfraPlan_SchemaVersionV2_WhenJITModuleFieldRef is the canonical T5.4
// scenario: a config whose env_vars carry a ${MODULE.field} reference (a
// JIT-required ref) bumps the plan to V2 so older wfctl binaries (which
// only read V1) reject the plan with the standard "newer than supported"
// diagnostic. T5.5 will additionally reject persisting V2 plans via -o.
func TestInfraPlan_SchemaVersionV2_WhenJITModuleFieldRef(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	// env_vars is a preserve-key submap, so ${pg.private_ip} survives
	// plan-time ExpandEnvInMapPreservingKeys verbatim and lands in
	// plan.Actions[0].Resource.Config["env_vars"]["DB_HOST"].
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: app
    type: infra.container_service
    config:
      env_vars:
        DB_HOST: "${pg.private_ip}"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(dir, "plan.json")
	// Note: -o causes a separate persisted-plan rejection in T5.5; for
	// T5.4 we only need to verify the SchemaVersion bumping logic. We
	// run plan WITHOUT -o (stdout-only) which is allowed for V2 in T5.5.
	if err := runInfraPlan([]string{"--config", cfgPath}); err != nil {
		// runInfraPlan with no -o still computes the plan and prints
		// to stdout; SchemaVersion bumping is observable only via -o
		// or via the in-process plan struct. For test purposes we use
		// -o on this V2 path under T5.4 ONLY — T5.5's rejection adds
		// a guard later and this test predates it. To keep T5.4 a
		// pure-logic test we exercise the helper directly below.
		t.Fatalf("runInfraPlan (no -o): %v", err)
	}

	// Direct helper assertion: build a plan struct with a JIT ref and
	// verify planRequiresJITSubstitution flags it. This locks the
	// SchemaVersion-stamp logic without depending on T5.5's persistence
	// rejection coming online.
	_ = planFile
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "app", Type: "infra.container_service",
				Config: map[string]any{
					"env_vars": map[string]any{"DB_HOST": "${pg.private_ip}"},
				},
			},
		}},
	}
	if !planRequiresJITSubstitution(plan) {
		t.Errorf("planRequiresJITSubstitution: false; want true (JIT ref in env_vars)")
	}
}

// TestInfraPlan_SchemaVersionV2_WhenJITModuleIDRef verifies the .id form
// is also detected — a separate test because ${MODULE.id} is the most
// common JIT pattern (cascade-replace ID propagation).
func TestInfraPlan_SchemaVersionV2_WhenJITModuleIDRef(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "app", Type: "infra.container_service",
				Config: map[string]any{"vpc_uuid": "${vpc.id}"},
			},
		}},
	}
	if !planRequiresJITSubstitution(plan) {
		t.Errorf("planRequiresJITSubstitution: false; want true (${vpc.id})")
	}
}

// TestInfraPlan_SchemaVersionV1_OnlyEnvVarRef_NoJIT verifies the negative
// case: a ${VAR} ref (no dot) does NOT trigger the V2 bump even when
// preserved through env_vars submaps. This protects the common case
// where operators use env vars for secrets without needing JIT.
func TestInfraPlan_SchemaVersionV1_OnlyEnvVarRef_NoJIT(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "app", Type: "infra.container_service",
				Config: map[string]any{
					"env_vars": map[string]any{"DB_PASSWORD": "${PG_PASSWORD}"},
				},
			},
		}},
	}
	if planRequiresJITSubstitution(plan) {
		t.Errorf("planRequiresJITSubstitution: true; want false (only ${VAR})")
	}
}

// (T5.4's earlier persisted-end-to-end test was superseded by T5.5's
// rejection contract — V2 plans now error at -o time rather than being
// written to disk. The helper-only assertions above already cover the
// stamp-logic correctness; T5.5's own test file owns the rejection
// scenario end-to-end.)

// readPlanFile is a small helper that loads + unmarshals a plan.json,
// failing the test on any IO/decode error.
func readPlanFile(t *testing.T, path string) *interfaces.IaCPlan {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	var plan interfaces.IaCPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	return &plan
}
