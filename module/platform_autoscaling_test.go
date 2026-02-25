package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func newAutoscalingConfig() map[string]any {
	return map[string]any{
		"provider": "mock",
		"policies": []any{
			map[string]any{
				"name":            "cpu-target",
				"type":            "target_tracking",
				"target_resource": "staging-ecs",
				"min_capacity":    2,
				"max_capacity":    20,
				"metric_name":     "CPUUtilization",
				"target_value":    70.0,
			},
			map[string]any{
				"name":             "night-scale-down",
				"type":             "scheduled",
				"target_resource":  "staging-ecs",
				"schedule":         "cron(0 22 * * ? *)",
				"desired_capacity": 1,
			},
		},
	}
}

// TestPlatformAutoscaling_MockLifecycle tests the full plan→apply→status→destroy lifecycle.
func TestPlatformAutoscaling_MockLifecycle(t *testing.T) {
	as := module.NewPlatformAutoscaling("app-scaling", newAutoscalingConfig())
	app := module.NewMockApplication()
	if err := as.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Plan — fresh state should propose creation.
	plan, err := as.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change in plan")
	}
	if len(plan.Policies) != 2 {
		t.Errorf("expected 2 policies in plan, got %d", len(plan.Policies))
	}

	// Apply — should create the scaling policies in-memory.
	state, err := as.Apply()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
	if state.ID == "" {
		t.Error("expected non-empty ID after apply")
	}
	if state.CurrentCapacity == 0 {
		t.Error("expected non-zero CurrentCapacity after apply")
	}

	// Status — should show active.
	st, err := as.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	scalingState, ok := st.(*module.ScalingState)
	if !ok {
		t.Fatalf("Status returned unexpected type %T", st)
	}
	if scalingState.Status != "active" {
		t.Errorf("expected status=active, got %q", scalingState.Status)
	}

	// Plan after apply — should show idempotent state.
	plan2, err := as.Plan()
	if err != nil {
		t.Fatalf("second Plan failed: %v", err)
	}
	if len(plan2.Changes) == 0 {
		t.Fatal("expected at least one change entry")
	}

	// Destroy.
	if err := as.Destroy(); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}
	st2, err := as.Status()
	if err != nil {
		t.Fatalf("Status after destroy failed: %v", err)
	}
	scalingState2 := st2.(*module.ScalingState)
	if scalingState2.Status != "deleted" {
		t.Errorf("expected status=deleted after destroy, got %q", scalingState2.Status)
	}
	if scalingState2.ID != "" {
		t.Errorf("expected empty ID after destroy, got %q", scalingState2.ID)
	}
}

// TestPlatformAutoscaling_PolicyTypes verifies all policy types are parsed.
func TestPlatformAutoscaling_PolicyTypes(t *testing.T) {
	as := module.NewPlatformAutoscaling("typed-scaling", newAutoscalingConfig())
	app := module.NewMockApplication()
	if err := as.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plan, err := as.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	types := map[string]bool{}
	for _, p := range plan.Policies {
		types[p.Type] = true
	}
	if !types["target_tracking"] {
		t.Error("expected target_tracking policy type")
	}
	if !types["scheduled"] {
		t.Error("expected scheduled policy type")
	}
}

// TestPlatformAutoscaling_ApplyIdempotent verifies double-apply is safe.
func TestPlatformAutoscaling_ApplyIdempotent(t *testing.T) {
	as := module.NewPlatformAutoscaling("idem-scaling", newAutoscalingConfig())
	app := module.NewMockApplication()
	if err := as.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := as.Apply(); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	state, err := as.Apply()
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected active after second apply, got %q", state.Status)
	}
}

// TestPlatformAutoscaling_InvalidProvider verifies Init rejects unknown providers.
func TestPlatformAutoscaling_InvalidProvider(t *testing.T) {
	as := module.NewPlatformAutoscaling("bad-scaling", map[string]any{"provider": "azure"})
	app := module.NewMockApplication()
	if err := as.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

// TestPlatformAutoscaling_InvalidAccount verifies Init fails when account is missing.
func TestPlatformAutoscaling_InvalidAccount(t *testing.T) {
	as := module.NewPlatformAutoscaling("no-acc-scaling", map[string]any{
		"provider": "mock",
		"account":  "nonexistent-account",
	})
	app := module.NewMockApplication()
	if err := as.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

// TestPlatformAutoscaling_AWSStubPlan verifies the AWS stub returns a plan.
func TestPlatformAutoscaling_AWSStubPlan(t *testing.T) {
	as := module.NewPlatformAutoscaling("aws-scaling", map[string]any{
		"provider": "aws",
		"policies": []any{
			map[string]any{
				"name":            "cpu",
				"type":            "target_tracking",
				"target_resource": "my-ecs",
				"min_capacity":    1,
				"max_capacity":    10,
			},
		},
	})
	app := module.NewMockApplication()
	if err := as.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plan, err := as.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change from AWS stub")
	}
}

// TestPlatformAutoscaling_AWSApplyNotImplemented verifies AWS Apply returns an error.
func TestPlatformAutoscaling_AWSApplyNotImplemented(t *testing.T) {
	as := module.NewPlatformAutoscaling("aws-scaling2", map[string]any{"provider": "aws"})
	app := module.NewMockApplication()
	if err := as.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := as.Apply()
	if err == nil {
		t.Error("expected error from AWS Apply stub, got nil")
	}
}

// TestPlatformAutoscaling_ServiceRegistration verifies the module registers itself.
func TestPlatformAutoscaling_ServiceRegistration(t *testing.T) {
	as := module.NewPlatformAutoscaling("reg-scaling", map[string]any{"provider": "mock"})
	app := module.NewMockApplication()
	if err := as.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	svc, ok := app.Services["reg-scaling"]
	if !ok {
		t.Fatal("expected reg-scaling in service registry")
	}
	if _, ok := svc.(*module.PlatformAutoscaling); !ok {
		t.Fatalf("expected *PlatformAutoscaling in registry, got %T", svc)
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func setupAutoscalingApp(t *testing.T) (*module.MockApplication, *module.PlatformAutoscaling) {
	t.Helper()
	app := module.NewMockApplication()
	as := module.NewPlatformAutoscaling("my-scaling", newAutoscalingConfig())
	if err := as.Init(app); err != nil {
		t.Fatalf("autoscaling Init: %v", err)
	}
	return app, as
}

func TestScalingPlanStep(t *testing.T) {
	app, _ := setupAutoscalingApp(t)
	factory := module.NewScalingPlanStepFactory()
	step, err := factory("plan", map[string]any{"scaling": "my-scaling"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["scaling"] != "my-scaling" {
		t.Errorf("expected scaling=my-scaling, got %v", result.Output["scaling"])
	}
	if result.Output["changes"] == nil {
		t.Error("expected changes in output")
	}
}

func TestScalingApplyStep(t *testing.T) {
	app, _ := setupAutoscalingApp(t)
	factory := module.NewScalingApplyStepFactory()
	step, err := factory("apply", map[string]any{"scaling": "my-scaling"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}
}

func TestScalingStatusStep(t *testing.T) {
	app, as := setupAutoscalingApp(t)
	if _, err := as.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	factory := module.NewScalingStatusStepFactory()
	step, err := factory("status", map[string]any{"scaling": "my-scaling"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["scaling"] != "my-scaling" {
		t.Errorf("expected scaling=my-scaling, got %v", result.Output["scaling"])
	}
	st := result.Output["status"].(*module.ScalingState)
	if st.Status != "active" {
		t.Errorf("expected status=active, got %q", st.Status)
	}
}

func TestScalingDestroyStep(t *testing.T) {
	app, as := setupAutoscalingApp(t)
	if _, err := as.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	factory := module.NewScalingDestroyStepFactory()
	step, err := factory("destroy", map[string]any{"scaling": "my-scaling"}, app)
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
}

func TestScalingPlanStep_MissingScaling(t *testing.T) {
	factory := module.NewScalingPlanStepFactory()
	_, err := factory("plan", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing scaling, got nil")
	}
}

func TestScalingPlanStep_ScalingNotFound(t *testing.T) {
	factory := module.NewScalingPlanStepFactory()
	step, err := factory("plan", map[string]any{"scaling": "ghost"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing scaling service, got nil")
	}
}
