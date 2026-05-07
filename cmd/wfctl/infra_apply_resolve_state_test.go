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

// writeApplyStateFile creates a state JSON file for the apply tests.
// Duplicated here rather than shared because test helpers are package-scoped
// and we want to keep each test file self-contained.
func writeApplyStateFile(t *testing.T, dir, name, resourceType, providerID string, outputs map[string]any, cfg map[string]any) {
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

// TestRunInfraApply_ResolvesSpecsBeforeComputePlan is the TC2 regression test
// for the apply path: when vpc_uuid is an infra_output secret pointing at
// core-dump-vpc.id, apply should resolve it to the real ID before computing
// the diff plan — meaning the droplet should see no change and NOT be replaced.
//
// The test uses secrets.generate (type: infra_output) and does NOT inject the
// env var via t.Setenv, so the only resolution path is the state-output resolver.
// This ensures the test fails if resolveSpecsAgainstState's infra_output handling
// is broken (rather than succeeding vacuously via os.LookupEnv).
func TestRunInfraApply_ResolvesSpecsBeforeComputePlan(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	const vpcID = "14badc41-1234-5678-abcd-ef0123456789"
	writeApplyStateFile(t, stateDir, "core-dump-vpc", "infra.vpc", vpcID,
		map[string]any{"id": vpcID, "cidr": "10.0.0.0/16"},
		map[string]any{"provider": "do-provider", "cidr": "10.0.0.0/16"})
	writeApplyStateFile(t, stateDir, "coredump-staging-pg", "infra.droplet", "droplet-99",
		map[string]any{"vpc_uuid": vpcID},
		map[string]any{"provider": "do-provider", "vpc_uuid": vpcID, "size": "s-1vcpu-2gb"})

	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
infra:
  auto_bootstrap: false

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
  provider: env
  generate:
    - key: STAGING_VPC_UUID
      type: infra_output
      source: core-dump-vpc.id
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Track what actions computeInfraPlan receives.
	var capturedActions []interfaces.PlanAction

	// Override computeInfraPlan with TC2 field-compare logic. This is the
	// load-bearing seam for testing pre-plan resolution: before PR-1,
	// spec.Config["vpc_uuid"] == "${STAGING_VPC_UUID}" (literal template)
	// which differs from the stored vpc_uuid → replace fires.
	// After PR-1, resolveSpecsAgainstState collapses it to the real UUID
	// before this function is called → no diff → no replace.
	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
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
			anyDiff := false
			for k, desiredVal := range spec.Config {
				if k == "provider" {
					continue
				}
				stateVal, hasField := rs.AppliedConfig[k]
				if !hasField {
					stateVal, hasField = rs.Outputs[k]
				}
				sv := ""
				if hasField {
					sv = fmt.Sprintf("%v", stateVal)
				}
				if !hasField || fmt.Sprintf("%v", desiredVal) != sv {
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
		capturedActions = actions
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	// Override resolveIaCProvider to avoid real plugin load.
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return &applyCapture{}, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = origResolve })

	if err := runInfraApply([]string{"--config", cfgPath, "--auto-approve"}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	// Key assertion: no spurious replace on coredump-staging-pg.
	for _, a := range capturedActions {
		if a.Action == "replace" && a.Resource.Name == "coredump-staging-pg" {
			t.Errorf("spurious replace fired on coredump-staging-pg — plan-time resolution did not collapse ${STAGING_VPC_UUID}")
		}
	}
}
