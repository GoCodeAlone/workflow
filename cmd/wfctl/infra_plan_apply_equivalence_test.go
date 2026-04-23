package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// recordingProvider captures every ResourceSpec passed to Apply actions so we
// can verify that the names plan displays are the same names apply uses.
type recordingProvider struct {
	applyCapture
	applied []interfaces.ResourceSpec
}

func (r *recordingProvider) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, action := range plan.Actions {
		r.applied = append(r.applied, action.Resource)
	}
	// Return a zero-value result so apply succeeds without cloud calls.
	return &interfaces.ApplyResult{}, nil
}

// TestPlanApplyEquivalence_EnvOverrideNames is the regression gate for Bug #32
// and the class of env-override name divergences. It:
//  1. Builds a BMW-shaped infra.yaml with env overrides that rename every resource.
//  2. Calls planResourcesForEnv("staging") to capture what the plan renderer sees.
//  3. Calls applyInfraModules via a recording fake provider to capture actual spec names.
//  4. Asserts the two name sets are identical.
//
// This test FAILS before Fix #1 (ResolveForEnv name lift) and PASSES after.
func TestPlanApplyEquivalence_EnvOverrideNames(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: bmw-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: bmw-vpc
    type: infra.vpc
    config:
      provider: bmw-provider
      cidr: "10.0.0.0/24"
    environments:
      staging:
        config:
          name: bmw-staging-vpc

  - name: bmw-firewall
    type: infra.firewall
    config:
      provider: bmw-provider
    environments:
      staging:
        config:
          name: bmw-staging-firewall

  - name: bmw-db
    type: infra.database
    config:
      provider: bmw-provider
      engine: postgres
    environments:
      staging:
        config:
          name: bmw-staging-db

  - name: bmw-app
    type: infra.container_service
    config:
      provider: bmw-provider
      image: registry/app:latest
    environments:
      staging:
        config:
          name: bmw-staging-app
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// ── Step 1: what plan sees ────────────────────────────────────────────────
	planned, err := planResourcesForEnv(cfgPath, "staging")
	if err != nil {
		t.Fatalf("planResourcesForEnv: %v", err)
	}
	plannedNames := map[string]bool{}
	for _, rm := range planned {
		if rm.Type == "iac.provider" {
			continue // skip provider modules — they don't become ResourceSpecs
		}
		plannedNames[rm.Name] = true
	}
	wantNames := map[string]bool{
		"bmw-staging-vpc":      true,
		"bmw-staging-firewall": true,
		"bmw-staging-db":       true,
		"bmw-staging-app":      true,
	}
	if !reflect.DeepEqual(plannedNames, wantNames) {
		t.Errorf("plan names = %v, want %v", plannedNames, wantNames)
	}

	// ── Step 2: what apply actually sends to the provider ────────────────────
	rp := &recordingProvider{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return rp, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	if err := applyInfraModules(context.Background(), cfgPath, "staging"); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	rp.mu.Lock()
	appliedSpecs := rp.applied
	rp.mu.Unlock()

	actualNames := map[string]bool{}
	for _, s := range appliedSpecs {
		actualNames[s.Name] = true
	}

	// ── Step 3: assert plan names == apply names ──────────────────────────────
	if !reflect.DeepEqual(plannedNames, actualNames) {
		t.Errorf("plan-vs-apply name divergence:\n  plan:  %v\n  apply: %v", plannedNames, actualNames)
	}
}
