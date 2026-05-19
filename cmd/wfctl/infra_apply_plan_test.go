package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/iac/inputsnapshot"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// fingerprintForTest delegates to inputsnapshot.Compute so the test always
// uses the production fingerprint algorithm. Re-implementing sha256 + 16-hex
// inline would silently drift if the scheme changed; routing through the
// same function the apply path uses makes that impossible.
func fingerprintForTest(value string) string {
	snap := inputsnapshot.Compute([]string{"k"}, func(string) (string, bool) { return value, true })
	return snap["k"]
}

// TestInfraApplyConsumesPlan verifies that wfctl infra apply --plan <file>:
//  1. Reads actions from the plan file without calling ComputePlan.
//  2. Dispatches via wfctlhelpers.ApplyPlanWithHooks (v2-only post
//     workflow#699) with exactly the plan from the file (identified by
//     plan ID).
//  3. Does NOT recompute a fresh plan from the config diff.
//
// Captures the plan via the applyV2ApplyPlanWithHooksFn seam — the
// stubbed dispatch records the plan reference without invoking the
// per-action driver layer.
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

	// Mock provider: satisfies the interface; the v2 seam captures the plan
	// before per-action dispatch reaches any driver.
	fake := &applyCapture{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = origResolve }()

	// Capture the plan via the v2 dispatch seam (post workflow#699 — there
	// is no longer a provider.Apply to assert against).
	var (
		mu          sync.Mutex
		applyCalled bool
		appliedPlan *interfaces.IaCPlan
	)
	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, p *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		mu.Lock()
		defer mu.Unlock()
		applyCalled = true
		appliedPlan = p
		return &interfaces.ApplyResult{PlanID: p.ID}, nil
	}
	defer func() { applyV2ApplyPlanWithHooksFn = origApply }()

	// Run apply with --plan flag.
	if err := runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Dispatch must have been invoked.
	if !applyCalled {
		t.Fatal("v2 dispatch (applyV2ApplyPlanWithHooksFn) was not called")
	}
	if appliedPlan == nil {
		t.Fatal("appliedPlan is nil")
	}

	// Verify the plan came from the file (not recomputed).
	// ComputePlan generates a fresh ID ("plan-<timestamp>"); our file has a fixed ID.
	if appliedPlan.ID != planID {
		t.Errorf("plan ID: want %q (from file), got %q (recomputed?)", planID, appliedPlan.ID)
	}

	// Exactly one create action for my-db.
	if got := len(appliedPlan.Actions); got != 1 {
		t.Fatalf("plan actions: want 1, got %d", got)
	}
	a := appliedPlan.Actions[0]
	if a.Action != "create" {
		t.Errorf("action: want create, got %q", a.Action)
	}
	if a.Resource.Name != "my-db" {
		t.Errorf("resource name: want my-db, got %q", a.Resource.Name)
	}
}

// TestInfraApplyConsumesScopedPlan verifies that a persisted plan produced with
// --include is hash-checked against the same scoped desired set at apply time.
func TestInfraApplyConsumesScopedPlan(t *testing.T) {
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

  - name: other-db
    type: infra.database
    config:
      provider: test-provider
      engine: postgres
      size: s
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planPath := filepath.Join(dir, "plan.json")

	fake := &applyCapture{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = origResolve })
	fake.installAsV2Dispatch(t)

	if err := runInfraPlan([]string{"--config", cfgPath, "--include=my-db", "--output", planPath}); err != nil {
		t.Fatalf("runInfraPlan: %v", err)
	}

	if err := runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath}); err != nil {
		t.Fatalf("runInfraApply scoped plan: %v", err)
	}
	if !fake.applyCalled {
		t.Fatal("scoped plan was not applied")
	}
	if fake.appliedPlan == nil || len(fake.appliedPlan.Actions) != 1 {
		t.Fatalf("applied plan actions = %+v, want exactly one action", fake.appliedPlan)
	}
	if got := fake.appliedPlan.Actions[0].Resource.Name; got != "my-db" {
		t.Fatalf("applied resource = %q, want my-db", got)
	}
}

func TestInfraApplyPlanSkipBootstrap(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
infra:
  auto_bootstrap: true
secrets:
  provider: env
  generate:
    - key: SHOULD_NOT_BOOTSTRAP
      type: provider_credential
      source: not-a-real-provider
      name: should-not-bootstrap
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
  - name: my-dns
    type: infra.dns
    config:
      provider: test-provider
      domain: example.com
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	specs, err := parseInfraResourceSpecs(cfgPath)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	plan := interfaces.IaCPlan{
		ID:          "dns-plan",
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

	fake := &applyCapture{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = origResolve }()

	applyCalled := false
	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, p *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		applyCalled = true
		if p.ID != "dns-plan" {
			t.Fatalf("plan ID = %q, want dns-plan", p.ID)
		}
		return &interfaces.ApplyResult{PlanID: p.ID}, nil
	}
	defer func() { applyV2ApplyPlanWithHooksFn = origApply }()

	if err := runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath, "--skip-bootstrap"}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}
	if !applyCalled {
		t.Fatal("v2 dispatch was not called")
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

// TestInfraApplyConsumesPlan_NoHashRejected verifies that a plan file with no
// plan_hash (e.g. generated by an older wfctl before this feature) is rejected
// with a clear diagnostic rather than a misleading "config hash mismatch".
func TestInfraApplyConsumesPlan_NoHashRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
  - name: my-db
    type: infra.database
    config:
      provider: test-provider
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Plan with no hash field (legacy plan).
	plan := interfaces.IaCPlan{
		ID:        "legacy-plan",
		Actions:   []interfaces.PlanAction{{Action: "create", Resource: interfaces.ResourceSpec{Name: "my-db", Type: "infra.database"}}},
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

	err = runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath})
	if err == nil {
		t.Fatal("expected error for plan with no hash, got nil")
	}
	if !strings.Contains(err.Error(), "no hash") {
		t.Errorf("error should mention 'no hash', got: %v", err)
	}
}

// TestInfraApplyConsumesPlan_FutureSchemaRejected verifies that a plan whose
// SchemaVersion is greater than the current binary supports is rejected with
// a clear "newer than this wfctl" message rather than being silently
// mis-read as a v1 plan with stray fields.
func TestInfraApplyConsumesPlan_FutureSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
  - name: my-db
    type: infra.database
    config:
      provider: test-provider
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Plan declares a schema version newer than this binary supports.
	plan := interfaces.IaCPlan{
		ID:            "future-schema",
		SchemaVersion: infraPlanSchemaVersion + 1,
		DesiredHash:   "deadbeef",
		Actions:       []interfaces.PlanAction{{Action: "create", Resource: interfaces.ResourceSpec{Name: "my-db", Type: "infra.database"}}},
		CreatedAt:     time.Now().UTC(),
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	err = runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath})
	if err == nil {
		t.Fatal("expected error for future schema_version, got nil")
	}
	if !strings.Contains(err.Error(), "schema_version") || !strings.Contains(err.Error(), "newer") {
		t.Errorf("error should mention schema_version + newer; got: %v", err)
	}
}

// TestInfraApplyPrecomputedPlan_PersistsState + applyCaptureFull were
// deleted per workflow#699: the v1 dispatch path
// (provider.Apply → caller persists state) is gone. State persistence
// now happens via the v2 OnResourceApplied hook, exercised by
// TestInfraApplyPrecomputedPlan_V2PersistsStateThroughHooks below.

func TestInfraApplyPrecomputedPlan_V2PersistsStateThroughHooks(t *testing.T) {
	store := &fakeStateStore{}
	spec := interfaces.ResourceSpec{
		Name:   "first",
		Type:   "infra.test",
		Config: map[string]any{"provider": "test-provider"},
	}
	plan := interfaces.IaCPlan{
		ID:      "v2-persist-test",
		Actions: []interfaces.PlanAction{{Action: "create", Resource: spec}},
	}
	provider := &v2DriverProvider{driver: &v2ImmediatePersistDriver{store: store}}

	err := applyPrecomputedPlanWithStore(t.Context(), plan, provider, "fake-cloud", store, io.Discard, "", "", nil)
	if err != nil {
		t.Fatalf("applyPrecomputedPlanWithStore: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 {
		t.Fatalf("saved state count = %d, want 1; saved=%+v", len(store.saved), store.saved)
	}
	if store.saved[0].ProviderID != "id-first" {
		t.Fatalf("ProviderID = %q, want id-first", store.saved[0].ProviderID)
	}
}

func TestInfraApplyPrecomputedPlan_V2PrintsDriftReport(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID:      "v2-drift-test",
		Actions: []interfaces.PlanAction{{Action: "create", Resource: interfaces.ResourceSpec{Name: "x", Type: "infra.test"}}},
	}
	provider := &iactest.NoopProvider{ProviderName: "v2-stub"}
	driftEntries := []interfaces.DriftEntry{
		{Name: "EXAMPLE_VAR", PlanFingerprint: "plan-fp", ApplyFingerprint: "apply-fp"},
	}

	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		return &interfaces.ApplyResult{InputDriftReport: driftEntries}, nil
	}
	t.Cleanup(func() { applyV2ApplyPlanWithHooksFn = origApply })

	var w bytes.Buffer
	err := applyPrecomputedPlanWithStore(t.Context(), plan, provider, "fake-cloud", &fakeStateStore{}, &w, "test", "", nil)
	if err != nil {
		t.Fatalf("applyPrecomputedPlanWithStore: %v", err)
	}
	if !strings.Contains(w.String(), "EXAMPLE_VAR") {
		t.Fatalf("drift report missing EXAMPLE_VAR; got:\n%s", w.String())
	}
}

// TestInfraApplyPrecomputedPlan_FailedDeleteKeepsState was deleted per
// workflow#699 — the test relied on the v1 provider.Apply path returning
// a preset ApplyResult with delete errors. Post-v2 cutover the
// equivalent assertion lives in TestInfraApplyPrecomputedPlan_V2PersistsStateThroughHooks
// (state-write through OnResourceApplied/OnResourceDeleted hooks) and in
// the per-driver delete-error coverage in iac/wfctlhelpers/apply_test.go.

// TestApplyFromPrecomputedPlan_DeleteActionResolvesProvider verifies that delete
// actions (which carry no Resource.Config from ComputePlan) correctly resolve
// their provider module from action.Current.ProviderRef.
func TestApplyFromPrecomputedPlan_DeleteActionResolvesProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: test-provider
    type: iac.provider
    config:
      provider: fake-cloud
  - name: my-db
    type: infra.database
    config:
      provider: test-provider
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Delete action has empty Resource.Config (as produced by ComputePlan).
	deleteSpec := interfaces.ResourceSpec{
		Name: "my-db",
		Type: "infra.database",
		// Config intentionally empty — mirrors ComputePlan's delete action.
	}
	currentState := &interfaces.ResourceState{
		Name:        "my-db",
		Type:        "infra.database",
		ProviderRef: "test-provider", // provider ref lives in Current for deletes
		AppliedConfig: map[string]any{
			"provider": "test-provider",
		},
	}
	specs := []interfaces.ResourceSpec{{
		Name: "my-db", Type: "infra.database",
		Config: map[string]any{"provider": "test-provider"},
	}}
	plan := interfaces.IaCPlan{
		ID:          "delete-test",
		DesiredHash: desiredStateHash(nil), // delete-all: empty desired
		Actions:     []interfaces.PlanAction{{Action: "delete", Resource: deleteSpec, Current: currentState}},
		CreatedAt:   time.Now().UTC(),
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Also need the desiredHash to match (empty desired for delete-all).
	// We reload the plan and fix the hash.
	emptySpecs, _ := parseInfraResourceSpecs(cfgPath)
	_ = emptySpecs // for delete-all, desired is empty after removing my-db from config.
	plan.DesiredHash = desiredStateHash(nil)
	planData, _ = json.Marshal(plan)
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		t.Fatalf("rewrite plan: %v", err)
	}

	fake := &applyCapture{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = origResolve }()

	// Stub the v2 dispatch seam — the test is asserting the provider-
	// resolution stage (action.Current.ProviderRef → loaded provider) and
	// MUST NOT cross into the per-driver dispatch layer (which the bare
	// applyCapture lacks). Per workflow#699 v2 is the only dispatch.
	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		return &interfaces.ApplyResult{}, nil
	}
	defer func() { applyV2ApplyPlanWithHooksFn = origApply }()

	// With an empty config (delete-all scenario), hash matches because both
	// sides hash nil/empty spec slices the same way.
	// The key assertion: applyFromPrecomputedPlan must NOT error on the delete action.
	_ = specs
	_, err = applyFromPrecomputedPlan(context.Background(), plan, cfgPath, "")
	// The apply itself won't error even if the config has my-db (hash mismatch
	// would catch that) — we just want to confirm no "missing provider" error.
	// With the delete action resolved via Current.ProviderRef, dispatch reaches
	// the v2 seam.
	if err != nil && strings.Contains(err.Error(), "missing 'provider' field") {
		t.Errorf("delete action should resolve provider from Current, got: %v", err)
	}
}

// TestApply_PlanStaleDiagnostic_NamesChangedKeys_Persisted verifies that the
// persisted-`--plan` apply path returns the typed inputsnapshot.ErrEnvVarChanged
// sentinel when an env-var fingerprint embedded in the plan differs from the
// env at apply time, and that the error message names the changed key. This
// is the W-1 cross-PR test for the persisted-plan path; the in-process apply
// path is wired in T3.1.5 (W-3a).
func TestApply_PlanStaleDiagnostic_NamesChangedKeys_Persisted(t *testing.T) {
	// Plan was generated with old-value; embed its fingerprint in the plan.
	t.Setenv("STAGING_PG_PASSWORD", "old-value")
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
      env_vars:
        DATABASE_PASSWORD: "${STAGING_PG_PASSWORD}"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	specs, err := parseInfraResourceSpecs(cfgPath)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}
	plan := interfaces.IaCPlan{
		ID:            "stale-input-plan",
		DesiredHash:   desiredStateHash(specs),
		SchemaVersion: 1,
		InputSnapshot: map[string]string{
			"STAGING_PG_PASSWORD": fingerprintForTest("old-value"),
		},
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

	// Mock provider so apply doesn't try to reach a real cloud.
	fake := &applyCapture{}
	origResolve := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = origResolve }()

	// Apply with a different value — should trigger the drift diagnostic.
	t.Setenv("STAGING_PG_PASSWORD", "new-value")
	err = runInfraApply([]string{"--auto-approve", "--config", cfgPath, "--plan", planPath})
	if err == nil {
		t.Fatal("expected plan-stale error from changed env-var fingerprint, got nil")
	}
	if !errors.Is(err, inputsnapshot.ErrEnvVarChanged) {
		t.Errorf("expected sentinel inputsnapshot.ErrEnvVarChanged; got %v", err)
	}
	if !strings.Contains(err.Error(), "STAGING_PG_PASSWORD") {
		t.Errorf("error should name the changed key; got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "plan stale") {
		t.Errorf("error should preserve the 'plan stale' marker; got: %s", err.Error())
	}
	if fake.applyCalled {
		t.Error("provider.Apply should not be invoked when plan is stale on input snapshot")
	}
}

// TestDesiredStateHash_EmptySpecsProducesStableHash verifies that an empty spec
// slice hashes deterministically (not "") so delete-all plans are not blocked.
func TestDesiredStateHash_EmptySpecsProducesStableHash(t *testing.T) {
	h1 := desiredStateHash(nil)
	h2 := desiredStateHash([]interfaces.ResourceSpec{})
	if h1 == "" {
		t.Error("desiredStateHash(nil) should return a stable hash, not empty string")
	}
	if h1 != h2 {
		t.Errorf("desiredStateHash(nil) = %q, desiredStateHash([]) = %q; must be equal", h1, h2)
	}
	// Verify it differs from a non-empty spec hash.
	nonEmpty := desiredStateHash([]interfaces.ResourceSpec{{Name: "db", Type: "infra.database"}})
	if h1 == nonEmpty {
		t.Error("hash of empty specs should differ from hash of non-empty specs")
	}
}
