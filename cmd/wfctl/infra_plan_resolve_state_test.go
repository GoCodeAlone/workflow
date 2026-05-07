package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// writeStateFile writes an iacStateRecord to the given dir with filename
// derived from the resource name. Mirrors the format used by fsWfctlStateStore.
func writeStateFile(t *testing.T, dir, name, resourceType, providerID string, outputs map[string]any, cfg map[string]any) {
	t.Helper()
	rec := iacStateRecord{
		ResourceID:   name,
		ResourceType: resourceType,
		Provider:     "test-cloud",
		ProviderID:   providerID,
		Status:       "applied",
		Config:       cfg,
		Outputs:      outputs,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("marshal state for %q: %v", name, err)
	}
	fname := filepath.Join(dir, sanitizeStateID(name)+".json")
	if err := os.WriteFile(fname, data, 0o600); err != nil {
		t.Fatalf("write state file for %q: %v", name, err)
	}
}

// tc2MockProvider is an IaCProvider that returns a Diff-based plan
// simulating the DO DropletDriver behavior: if the desired config's
// vpc_uuid field differs from the state's, it emits a replace action.
// This faithfully reproduces the TC2 spurious-replace root cause.
type tc2MockProvider struct {
	applyCapture
}

func (p *tc2MockProvider) Plan(_ context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	// Build a lookup of current state by name.
	currentMap := make(map[string]interfaces.ResourceState, len(current))
	for _, s := range current {
		currentMap[s.Name] = s
	}
	var actions []interfaces.PlanAction
	for _, spec := range desired {
		rs, exists := currentMap[spec.Name]
		if !exists {
			actions = append(actions, interfaces.PlanAction{Action: "create", Resource: spec})
			continue
		}
		// Simulate DropletDriver.Diff: for each field in desired config,
		// compare against the stored state config. If any field differs,
		// emit a replace action (ForceNew semantics for vpc_uuid).
		//
		// This faithfully reproduces the TC2 root cause: before plan-time
		// resolution, spec.Config["vpc_uuid"] was "${STAGING_VPC_UUID}"
		// but rs.Config["vpc_uuid"] was the real UUID — mismatch → replace.
		//
		// After plan-time resolution, spec.Config["vpc_uuid"] is the real
		// UUID (matching rs.Config["vpc_uuid"]) → no action.
		anyDiff := false
		for k, desiredVal := range spec.Config {
			if k == "provider" {
				continue // provider ref is metadata, not a cloud field
			}
			stateVal, hasField := rs.AppliedConfig[k]
			if !hasField {
				// Check state Config (iacStateRecord.config maps to AppliedConfig
				// but some backends persist as Outputs). Check outputs too.
				stateVal, hasField = rs.Outputs[k]
			}
			if !hasField || fmt.Sprintf("%v", desiredVal) != fmt.Sprintf("%v", stateVal) {
				anyDiff = true
				break
			}
		}
		if anyDiff {
			actions = append(actions, interfaces.PlanAction{
				Action:   "replace",
				Resource: spec,
				Current:  &rs,
			})
		}
	}
	plan := &interfaces.IaCPlan{Actions: actions}
	return plan, nil
}

func (p *tc2MockProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}

// Satisfy the IaCProvider interface (reuse applyCapture no-ops).
func (p *tc2MockProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return &interfaces.ApplyResult{}, nil
}

// TestRunInfraPlan_TC2RegressionScenario: VPC in state, droplet referencing
// ${STAGING_VPC_UUID} (an infra_output-typed secret sourced from vpc.id).
// After plan-time resolution the droplet's vpc_uuid equals the VPC's provider
// ID, so the mock DropletDriver sees matching vpc_uuid → plan MUST have 0 actions.
//
// Before PR-1 this scenario produces 1 replace action (spurious, because
// the driver compared the literal "${STAGING_VPC_UUID}" vs the real ID).
func TestRunInfraPlan_TC2RegressionScenario(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	// Pre-populate state: VPC already applied, droplet already applied with
	// the correct vpc_uuid value. The VPC state also includes its config
	// so the mock provider's field-compare sees no change for the VPC.
	const vpcID = "14badc41-1234-5678-abcd-ef0123456789"
	writeStateFile(t, stateDir, "core-dump-vpc", "infra.vpc", vpcID,
		map[string]any{"id": vpcID, "cidr": "10.0.0.0/16"},
		map[string]any{"provider": "do-provider", "cidr": "10.0.0.0/16"})
	writeStateFile(t, stateDir, "coredump-staging-pg", "infra.droplet", "droplet-99",
		map[string]any{"vpc_uuid": vpcID},
		map[string]any{"provider": "do-provider", "vpc_uuid": vpcID, "size": "s-1vcpu-2gb"})

	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: test-cloud

  - name: do-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`

  - name: core-dump-vpc
    type: infra.vpc
    config:
      provider: do-provider
      cidr: "10.0.0.0/16"

  - name: coredump-staging-pg
    type: infra.droplet
    config:
      provider: do-provider
      vpc_uuid: "${STAGING_VPC_UUID}"
      size: s-1vcpu-2gb

secrets:
  generate:
    - key: STAGING_VPC_UUID
      type: infra_output
      source: core-dump-vpc.id
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Inject the TC2 mock provider to avoid real plugin load.
	mock := &tc2MockProvider{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return mock, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = origResolve })

	planFile := filepath.Join(dir, "plan.json")
	if err := runInfraPlan([]string{"--config", cfgPath, "--output", planFile}); err != nil {
		t.Fatalf("runInfraPlan: %v", err)
	}

	plan := readPlanFile(t, planFile)
	// All refs resolved at plan time → SchemaVersion=1 (not V2/JIT).
	if plan.SchemaVersion == infraPlanSchemaVersionJIT {
		t.Errorf("SchemaVersion: got JIT (%d), want V1 — all refs should resolve from state", infraPlanSchemaVersionJIT)
	}
	// Key assertion: no spurious replace.
	for _, a := range plan.Actions {
		t.Errorf("unexpected plan action %q on %q — should be no-change after plan-time resolution", a.Action, a.Resource.Name)
	}
}

// TestInfraPlan_SchemaVersionV1_WhenAllRefsResolveAtPlanTime verifies that
// planRequiresJITSubstitution returns false when the action config carries
// only already-resolved literal values (plan-time resolver succeeded for all refs).
func TestInfraPlan_SchemaVersionV1_WhenAllRefsResolveAtPlanTime(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name:   "pg",
				Config: map[string]any{"vpc_uuid": "14badc41-1234"}, // already resolved
			},
		}},
	}
	if planRequiresJITSubstitution(&plan) {
		t.Errorf("plan with all refs resolved should NOT require JIT")
	}
}
