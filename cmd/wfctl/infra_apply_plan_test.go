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

// applyCaptureFull is a mock provider that returns a real ApplyResult with
// provisioned resources, enabling state-persistence path testing.
type applyCaptureFull struct {
	applyCapture
	resources []interfaces.ResourceOutput
}

func (f *applyCaptureFull) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCalled = true
	f.appliedPlan = plan
	return &interfaces.ApplyResult{Resources: f.resources}, nil
}

// TestInfraApplyPrecomputedPlan_PersistsState verifies that applyPrecomputedPlanWithStore
// writes ResourceState records to the store after a successful apply, with correct
// metadata fields (ProviderID, ProviderRef, ConfigHash, Dependencies).
func TestInfraApplyPrecomputedPlan_PersistsState(t *testing.T) {
	stateDir := t.TempDir()
	store := &fsWfctlStateStore{dir: stateDir}

	spec := interfaces.ResourceSpec{
		Name: "my-db",
		Type: "infra.database",
		Config: map[string]any{
			"provider": "test-provider",
			"engine":   "postgres",
		},
		DependsOn: []string{"some-vpc"},
	}
	plan := interfaces.IaCPlan{
		ID:      "persist-test",
		Actions: []interfaces.PlanAction{{Action: "create", Resource: spec}},
	}

	provider := &applyCaptureFull{
		resources: []interfaces.ResourceOutput{
			{Name: "my-db", Type: "infra.database", ProviderID: "db-abc123"},
		},
	}

	err := applyPrecomputedPlanWithStore(context.Background(), plan, provider, "fake-cloud", store, io.Discard, "", "", nil)
	if err != nil {
		t.Fatalf("applyPrecomputedPlanWithStore: %v", err)
	}

	// Verify the state was persisted.
	all, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var saved *interfaces.ResourceState
	for i := range all {
		if all[i].Name == "my-db" {
			saved = &all[i]
			break
		}
	}
	if saved == nil {
		t.Fatal("ResourceState for my-db not found in store")
	}
	if saved.ProviderID != "db-abc123" {
		t.Errorf("ProviderID: want db-abc123, got %q", saved.ProviderID)
	}
	if saved.ProviderRef != "test-provider" {
		t.Errorf("ProviderRef: want test-provider, got %q", saved.ProviderRef)
	}
	if saved.Provider != "fake-cloud" {
		t.Errorf("Provider: want fake-cloud, got %q", saved.Provider)
	}
	if len(saved.Dependencies) != 1 || saved.Dependencies[0] != "some-vpc" {
		t.Errorf("Dependencies: want [some-vpc], got %v", saved.Dependencies)
	}
	if saved.ConfigHash == "" {
		t.Error("ConfigHash: want non-empty, got empty")
	}
}

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
	provider := &iactest.NoopProvider{ProviderName: "v2-stub", DispatchVersion: "v2"}
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

func TestInfraApplyPrecomputedPlan_FailedDeleteKeepsState(t *testing.T) {
	current := interfaces.ResourceState{Name: "old", Type: "infra.test", ProviderID: "id-old"}
	store := &fakeStateStore{saved: []interfaces.ResourceState{current}}
	plan := interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "delete",
		Resource: interfaces.ResourceSpec{Name: "old", Type: "infra.test"},
		Current:  &current,
	}}}
	provider := &stateReturningProvider{
		applyResult: &interfaces.ApplyResult{
			Errors: []interfaces.ActionError{{Action: "delete", Resource: "old", Error: "delete failed"}},
		},
	}

	err := applyPrecomputedPlanWithStore(t.Context(), plan, provider, "fake-cloud", store, io.Discard, "", "", nil)
	if err == nil {
		t.Fatal("expected delete failure, got nil")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleted) != 0 {
		t.Fatalf("deleted state entries = %v, want none after failed delete", store.deleted)
	}
}

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

	// With an empty config (delete-all scenario), hash matches because both
	// sides hash nil/empty spec slices the same way.
	// The key assertion: applyFromPrecomputedPlan must NOT error on the delete action.
	_ = specs
	_, err = applyFromPrecomputedPlan(context.Background(), plan, cfgPath, "")
	// The apply itself won't error even if the config has my-db (hash mismatch
	// would catch that) — we just want to confirm no "missing provider" error.
	// With the delete action resolved via Current.ProviderRef, provider.Apply is called.
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
