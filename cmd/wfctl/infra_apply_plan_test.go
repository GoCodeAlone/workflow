package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestInfraApplyConsumesPlan verifies that wfctl infra apply --plan <file>:
//  1. Reads actions from the plan file without calling ComputePlan.
//  2. Calls provider.Apply with exactly the plan from the file (identified by plan ID).
//  3. Does NOT recompute a fresh plan from the config diff.
func TestInfraApplyConsumesPlan(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "test-token"

  - name: my-db
    type: infra.database
    config:
      provider: test-provider
      engine: postgres
      size: s
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Parse the actual specs from the config to get the exact representation
	// that runInfraApply will hash (Size field, etc.).
	specs, err := parseInfraResourceSpecs(cfgPath)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("no infra specs parsed from config")
	}

	// Build plan.json with a known ID and a single create action.
	planID := "precomputed-plan-id-12345"
	plan := interfaces.IaCPlan{
		ID:          planID,
		DesiredHash: desiredStateHash(specs),
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: specs[0]},
		},
		CreatedAt: time.Now().UTC(),
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Mock provider: records the plan passed to Apply.
	fake := &applyCapture{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = origResolve }()

	// Run apply with --plan flag.
	if err := runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	// Apply must have been called.
	if !fake.applyCalled {
		t.Fatal("provider.Apply was not called")
	}
	if fake.appliedPlan == nil {
		t.Fatal("appliedPlan is nil")
	}

	// Verify the plan came from the file (not recomputed).
	// ComputePlan generates a fresh ID ("plan-<timestamp>"); our file has a fixed ID.
	if fake.appliedPlan.ID != planID {
		t.Errorf("plan ID: want %q (from file), got %q (recomputed?)", planID, fake.appliedPlan.ID)
	}

	// Exactly one create action for my-db.
	if got := len(fake.appliedPlan.Actions); got != 1 {
		t.Fatalf("plan actions: want 1, got %d", got)
	}
	a := fake.appliedPlan.Actions[0]
	if a.Action != "create" {
		t.Errorf("action: want create, got %q", a.Action)
	}
	if a.Resource.Name != "my-db" {
		t.Errorf("resource name: want my-db, got %q", a.Resource.Name)
	}
}

// TestInfraApplyConsumesPlan_StaleDetection verifies that wfctl infra apply --plan
// fails with a descriptive error when the plan's DesiredHash no longer matches the
// current desired state (i.e. the config was edited after the plan was generated).
func TestInfraApplyConsumesPlan_StaleDetection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")

	// Initial config: one spec.
	initialCfg := `
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "test-token"

  - name: my-db
    type: infra.database
    config:
      provider: test-provider
      engine: postgres
      size: s
`
	if err := os.WriteFile(cfgPath, []byte(initialCfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Parse specs from the initial config to compute the hash.
	initSpecs, err := parseInfraResourceSpecs(cfgPath)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}

	plan := interfaces.IaCPlan{
		ID:          "stale-plan-id",
		DesiredHash: desiredStateHash(initSpecs),
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: initSpecs[0]},
		},
		CreatedAt: time.Now().UTC(),
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Modify config AFTER plan was generated: change size s → m.
	modifiedCfg := `
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "test-token"

  - name: my-db
    type: infra.database
    config:
      provider: test-provider
      engine: postgres
      size: m
`
	if err := os.WriteFile(cfgPath, []byte(modifiedCfg), 0o600); err != nil {
		t.Fatalf("overwrite config: %v", err)
	}

	// Apply should fail with stale-plan error.
	err = runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath})
	if err == nil {
		t.Fatal("expected error for stale plan, got nil")
	}
	if !strings.Contains(err.Error(), "plan stale") {
		t.Errorf("error should mention 'plan stale', got: %v", err)
	}
	if !strings.Contains(err.Error(), "config hash mismatch") {
		t.Errorf("error should mention 'config hash mismatch', got: %v", err)
	}
}
