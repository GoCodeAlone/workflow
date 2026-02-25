package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Mock PlatformProvider ────────────────────────────────────────────────────

type mockPlatformProvider struct {
	planResult    *module.PlatformPlan
	planErr       error
	applyResult   *module.PlatformResult
	applyErr      error
	statusResult  any
	statusErr     error
	destroyErr    error
	destroyCalled bool
}

func (m *mockPlatformProvider) Plan() (*module.PlatformPlan, error) {
	return m.planResult, m.planErr
}

func (m *mockPlatformProvider) Apply() (*module.PlatformResult, error) {
	return m.applyResult, m.applyErr
}

func (m *mockPlatformProvider) Status() (any, error) {
	return m.statusResult, m.statusErr
}

func (m *mockPlatformProvider) Destroy() error {
	m.destroyCalled = true
	return m.destroyErr
}

// ─── Setup helpers ────────────────────────────────────────────────────────────

func setupIaCApp(t *testing.T) (*module.MockApplication, module.IaCStateStore, *mockPlatformProvider) {
	t.Helper()
	app := module.NewMockApplication()

	store := module.NewMemoryIaCStateStore()
	if err := app.RegisterService("test-store", store); err != nil {
		t.Fatalf("register store: %v", err)
	}

	provider := &mockPlatformProvider{
		planResult: &module.PlatformPlan{
			Provider: "local",
			Resource: "kubernetes",
			Actions: []module.PlatformAction{
				{Type: "create", Resource: "cluster", Detail: "create cluster"},
			},
		},
		applyResult: &module.PlatformResult{
			Success: true,
			Message: "cluster created",
			State:   map[string]any{"endpoint": "https://127.0.0.1:6443"},
		},
		statusResult: map[string]any{"status": "running"},
	}
	if err := app.RegisterService("test-platform", provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	return app, store, provider
}

func baseIaCCfg() map[string]any {
	return map[string]any{
		"platform":    "test-platform",
		"resource_id": "my-cluster",
		"state_store": "test-store",
	}
}

// ─── step.iac_plan ────────────────────────────────────────────────────────────

func TestIaCPlanStep_BasicPlan(t *testing.T) {
	app, store, _ := setupIaCApp(t)
	factory := module.NewIaCPlanStepFactory()
	step, err := factory("plan", baseIaCCfg(), app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "planned" {
		t.Errorf("expected status=planned, got %v", result.Output["status"])
	}
	if result.Output["provider"] != "local" {
		t.Errorf("expected provider=local, got %v", result.Output["provider"])
	}
	actions, ok := result.Output["actions"].([]module.PlatformAction)
	if !ok || len(actions) == 0 {
		t.Errorf("expected non-empty actions slice, got %v", result.Output["actions"])
	}

	// State should be persisted.
	st, _ := store.GetState("my-cluster")
	if st == nil {
		t.Fatal("expected state to be persisted after plan")
	}
	if st.Status != "planned" {
		t.Errorf("expected stored status=planned, got %q", st.Status)
	}
}

func TestIaCPlanStep_MissingPlatform(t *testing.T) {
	factory := module.NewIaCPlanStepFactory()
	_, err := factory("plan", map[string]any{"state_store": "s"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing platform, got nil")
	}
}

func TestIaCPlanStep_MissingStateStore(t *testing.T) {
	factory := module.NewIaCPlanStepFactory()
	_, err := factory("plan", map[string]any{"platform": "p"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing state_store, got nil")
	}
}

func TestIaCPlanStep_PlatformNotFound(t *testing.T) {
	app := module.NewMockApplication()
	_ = app.RegisterService("test-store", module.NewMemoryIaCStateStore())
	factory := module.NewIaCPlanStepFactory()
	step, _ := factory("plan", map[string]any{"platform": "ghost", "state_store": "test-store"}, app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing platform service, got nil")
	}
}

func TestIaCPlanStep_PlanError(t *testing.T) {
	app, _, provider := setupIaCApp(t)
	provider.planErr = errors.New("plan failed: quota exceeded")
	factory := module.NewIaCPlanStepFactory()
	step, _ := factory("plan", baseIaCCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error from Plan, got nil")
	}
}

// ─── step.iac_apply ───────────────────────────────────────────────────────────

func TestIaCApplyStep_BasicApply(t *testing.T) {
	app, store, _ := setupIaCApp(t)

	// Plan first.
	planFactory := module.NewIaCPlanStepFactory()
	planStep, _ := planFactory("plan", baseIaCCfg(), app)
	_, _ = planStep.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})

	factory := module.NewIaCApplyStepFactory()
	step, err := factory("apply", baseIaCCfg(), app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}

	// State should be active.
	st, _ := store.GetState("my-cluster")
	if st == nil || st.Status != "active" {
		t.Errorf("expected stored status=active, got %v", st)
	}
}

func TestIaCApplyStep_ApplyError(t *testing.T) {
	app, store, provider := setupIaCApp(t)
	provider.applyErr = errors.New("apply failed: insufficient resources")

	// Seed a planned state.
	_ = store.SaveState(makeState("my-cluster", "kubernetes", "local", "planned"))

	factory := module.NewIaCApplyStepFactory()
	step, _ := factory("apply", baseIaCCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error from Apply, got nil")
	}

	// State should be error.
	st, _ := store.GetState("my-cluster")
	if st == nil || st.Status != "error" {
		t.Errorf("expected stored status=error after apply failure, got %v", st)
	}
}

func TestIaCApplyStep_MissingPlatform(t *testing.T) {
	factory := module.NewIaCApplyStepFactory()
	_, err := factory("apply", map[string]any{"state_store": "s"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing platform, got nil")
	}
}

// ─── step.iac_status ──────────────────────────────────────────────────────────

func TestIaCStatusStep_BasicStatus(t *testing.T) {
	app, store, _ := setupIaCApp(t)
	_ = store.SaveState(makeState("my-cluster", "kubernetes", "local", "active"))

	factory := module.NewIaCStatusStepFactory()
	step, err := factory("status", baseIaCCfg(), app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["resource_id"] != "my-cluster" {
		t.Errorf("expected resource_id=my-cluster, got %v", result.Output["resource_id"])
	}
	if result.Output["stored_status"] != "active" {
		t.Errorf("expected stored_status=active, got %v", result.Output["stored_status"])
	}
	if result.Output["live_status"] == nil {
		t.Error("expected live_status to be non-nil")
	}
}

func TestIaCStatusStep_MissingPlatform(t *testing.T) {
	factory := module.NewIaCStatusStepFactory()
	_, err := factory("status", map[string]any{"state_store": "s"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing platform, got nil")
	}
}

func TestIaCStatusStep_ProviderStatusError(t *testing.T) {
	app, _, provider := setupIaCApp(t)
	provider.statusErr = errors.New("connection refused")

	factory := module.NewIaCStatusStepFactory()
	step, _ := factory("status", baseIaCCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error from Status, got nil")
	}
}

// ─── step.iac_destroy ─────────────────────────────────────────────────────────

func TestIaCDestroyStep_BasicDestroy(t *testing.T) {
	app, store, provider := setupIaCApp(t)
	_ = store.SaveState(makeState("my-cluster", "kubernetes", "local", "active"))

	factory := module.NewIaCDestroyStepFactory()
	step, err := factory("destroy", baseIaCCfg(), app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["destroyed"] != true {
		t.Errorf("expected destroyed=true, got %v", result.Output["destroyed"])
	}
	if result.Output["status"] != "destroyed" {
		t.Errorf("expected status=destroyed, got %v", result.Output["status"])
	}
	if !provider.destroyCalled {
		t.Error("expected provider Destroy() to be called")
	}

	// State should be destroyed.
	st, _ := store.GetState("my-cluster")
	if st == nil || st.Status != "destroyed" {
		t.Errorf("expected stored status=destroyed, got %v", st)
	}
}

func TestIaCDestroyStep_DestroyError(t *testing.T) {
	app, store, provider := setupIaCApp(t)
	provider.destroyErr = errors.New("cannot delete: cluster in use")
	_ = store.SaveState(makeState("my-cluster", "kubernetes", "local", "active"))

	factory := module.NewIaCDestroyStepFactory()
	step, _ := factory("destroy", baseIaCCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error from Destroy, got nil")
	}

	st, _ := store.GetState("my-cluster")
	if st == nil || st.Status != "error" {
		t.Errorf("expected stored status=error after destroy failure, got %v", st)
	}
}

func TestIaCDestroyStep_MissingStateStore(t *testing.T) {
	factory := module.NewIaCDestroyStepFactory()
	_, err := factory("destroy", map[string]any{"platform": "p"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing state_store, got nil")
	}
}

// ─── step.iac_drift_detect ────────────────────────────────────────────────────

func TestIaCDriftDetect_NoDrift(t *testing.T) {
	app, store, _ := setupIaCApp(t)

	// Store state with a config snapshot.
	st := makeState("my-cluster", "kubernetes", "local", "active")
	st.Config = map[string]any{"version": "1.29", "nodeCount": 3}
	_ = store.SaveState(st)

	cfg := map[string]any{
		"platform":    "test-platform",
		"resource_id": "my-cluster",
		"state_store": "test-store",
		"config":      map[string]any{"version": "1.29", "nodeCount": 3},
	}

	factory := module.NewIaCDriftDetectStepFactory()
	step, err := factory("drift", cfg, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["drifted"] != false {
		t.Errorf("expected drifted=false, got %v", result.Output["drifted"])
	}
	diffs := result.Output["diffs"].([]module.IaCDriftDiff)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs, got %v", diffs)
	}
}

func TestIaCDriftDetect_WithDrift(t *testing.T) {
	app, store, _ := setupIaCApp(t)

	// Store state with original config.
	st := makeState("my-cluster", "kubernetes", "local", "active")
	st.Config = map[string]any{"version": "1.29", "nodeCount": 3}
	_ = store.SaveState(st)

	// Current config has modified nodeCount and a new key.
	cfg := map[string]any{
		"platform":    "test-platform",
		"resource_id": "my-cluster",
		"state_store": "test-store",
		"config":      map[string]any{"version": "1.29", "nodeCount": 5, "region": "us-east-1"},
	}

	factory := module.NewIaCDriftDetectStepFactory()
	step, _ := factory("drift", cfg, app)
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["drifted"] != true {
		t.Errorf("expected drifted=true, got %v", result.Output["drifted"])
	}
	diffs := result.Output["diffs"].([]module.IaCDriftDiff)
	if len(diffs) == 0 {
		t.Error("expected diffs, got none")
	}
}

func TestIaCDriftDetect_MissingState(t *testing.T) {
	app, _, _ := setupIaCApp(t)

	cfg := map[string]any{
		"platform":    "test-platform",
		"resource_id": "nonexistent-cluster",
		"state_store": "test-store",
		"config":      map[string]any{"version": "1.29"},
	}

	factory := module.NewIaCDriftDetectStepFactory()
	step, _ := factory("drift", cfg, app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing state, got nil")
	}
}

func TestIaCDriftDetect_RemovedKey(t *testing.T) {
	app, store, _ := setupIaCApp(t)

	st := makeState("my-cluster", "kubernetes", "local", "active")
	st.Config = map[string]any{"version": "1.29", "tags": "prod"}
	_ = store.SaveState(st)

	// Current config is missing the "tags" key.
	cfg := map[string]any{
		"platform":    "test-platform",
		"resource_id": "my-cluster",
		"state_store": "test-store",
		"config":      map[string]any{"version": "1.29"},
	}

	factory := module.NewIaCDriftDetectStepFactory()
	step, _ := factory("drift", cfg, app)
	result, _ := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if result.Output["drifted"] != true {
		t.Error("expected drifted=true for removed key")
	}
	diffs := result.Output["diffs"].([]module.IaCDriftDiff)
	found := false
	for _, d := range diffs {
		if d.Key == "tags" && d.DiffType == "removed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected diff with key=tags type=removed, got %v", diffs)
	}
}

func TestIaCDriftDetect_MissingPlatform(t *testing.T) {
	factory := module.NewIaCDriftDetectStepFactory()
	_, err := factory("drift", map[string]any{"state_store": "s"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing platform, got nil")
	}
}

func TestIaCDriftDetect_MissingStateStore(t *testing.T) {
	factory := module.NewIaCDriftDetectStepFactory()
	_, err := factory("drift", map[string]any{"platform": "p"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing state_store, got nil")
	}
}

// ─── Full IaC lifecycle ───────────────────────────────────────────────────────

func TestIaCLifecycle_PlanApplyStatusDestroy(t *testing.T) {
	app, store, _ := setupIaCApp(t)
	pc := &module.PipelineContext{Current: map[string]any{}}
	cfg := baseIaCCfg()

	// Plan.
	planStep, _ := module.NewIaCPlanStepFactory()("plan", cfg, app)
	_, err := planStep.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	st, _ := store.GetState("my-cluster")
	if st.Status != "planned" {
		t.Fatalf("expected planned, got %q", st.Status)
	}

	// Apply.
	applyStep, _ := module.NewIaCApplyStepFactory()("apply", cfg, app)
	_, err = applyStep.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	st, _ = store.GetState("my-cluster")
	if st.Status != "active" {
		t.Fatalf("expected active, got %q", st.Status)
	}

	// Status.
	statusStep, _ := module.NewIaCStatusStepFactory()("status", cfg, app)
	statusResult, err := statusStep.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if statusResult.Output["stored_status"] != "active" {
		t.Errorf("expected stored_status=active, got %v", statusResult.Output["stored_status"])
	}

	// Destroy.
	destroyStep, _ := module.NewIaCDestroyStepFactory()("destroy", cfg, app)
	_, err = destroyStep.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	st, _ = store.GetState("my-cluster")
	if st.Status != "destroyed" {
		t.Fatalf("expected destroyed, got %q", st.Status)
	}
}
