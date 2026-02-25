package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── module lifecycle tests ───────────────────────────────────────────────────

// TestPlatformECS_MockLifecycle tests the full plan→apply→status→destroy
// lifecycle using the in-memory mock backend.
func TestPlatformECS_MockLifecycle(t *testing.T) {
	e := module.NewPlatformECS("staging-svc", map[string]any{
		"cluster":       "staging-cluster",
		"region":        "us-east-1",
		"launch_type":   "FARGATE",
		"desired_count": 2,
	})

	app := module.NewMockApplication()
	if err := e.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Plan — should propose create actions on a fresh service.
	plan, err := e.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("expected at least one plan action")
	}
	if plan.Actions[0].Type != "create" {
		t.Errorf("expected action=create, got %q", plan.Actions[0].Type)
	}
	if plan.Provider != "ecs" {
		t.Errorf("expected provider=ecs, got %q", plan.Provider)
	}

	// Apply — should create the service in-memory.
	result, err := e.Apply()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Apply success=true")
	}
	if result.Message == "" {
		t.Error("expected non-empty message from Apply")
	}

	// Status — service should now be running.
	st, err := e.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	state, ok := st.(*module.ECSServiceState)
	if !ok {
		t.Fatalf("Status returned unexpected type %T", st)
	}
	if state.Status != "running" {
		t.Errorf("expected status=running, got %q", state.Status)
	}
	if state.RunningCount != 2 {
		t.Errorf("expected RunningCount=2, got %d", state.RunningCount)
	}
	if state.DesiredCount != 2 {
		t.Errorf("expected DesiredCount=2, got %d", state.DesiredCount)
	}

	// Plan after apply — should produce a noop.
	plan2, err := e.Plan()
	if err != nil {
		t.Fatalf("second Plan failed: %v", err)
	}
	if len(plan2.Actions) == 0 || plan2.Actions[0].Type != "noop" {
		t.Errorf("expected noop after apply, got %+v", plan2.Actions)
	}

	// Destroy — service should be deleted.
	if err := e.Destroy(); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}
	st2, err := e.Status()
	if err != nil {
		t.Fatalf("Status after destroy failed: %v", err)
	}
	state2 := st2.(*module.ECSServiceState)
	if state2.Status != "deleted" {
		t.Errorf("expected status=deleted after destroy, got %q", state2.Status)
	}
	if state2.RunningCount != 0 {
		t.Errorf("expected RunningCount=0 after destroy, got %d", state2.RunningCount)
	}
}

// TestPlatformECS_PlatformProviderInterface verifies PlatformECS satisfies
// the PlatformProvider interface.
func TestPlatformECS_PlatformProviderInterface(t *testing.T) {
	e := module.NewPlatformECS("iface-svc", map[string]any{"cluster": "test-cluster"})
	app := module.NewMockApplication()
	if err := e.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var _ module.PlatformProvider = e
}

// TestPlatformECS_CloudAccountResolution verifies the module resolves a
// cloud.account from the service registry during Init.
func TestPlatformECS_CloudAccountResolution(t *testing.T) {
	acc := module.NewCloudAccount("aws-staging", map[string]any{
		"provider": "mock",
		"region":   "us-east-1",
	})
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account Init: %v", err)
	}

	e := module.NewPlatformECS("my-svc", map[string]any{
		"cluster": "staging-cluster",
		"account": "aws-staging",
	})
	if err := e.Init(app); err != nil {
		t.Fatalf("ECS Init: %v", err)
	}

	svc, ok := app.Services["my-svc"]
	if !ok {
		t.Fatal("expected my-svc in service registry")
	}
	if _, ok := svc.(*module.PlatformECS); !ok {
		t.Fatalf("registry entry is %T, want *PlatformECS", svc)
	}
}

// TestPlatformECS_InvalidAccount verifies Init fails when the referenced
// cloud.account does not exist.
func TestPlatformECS_InvalidAccount(t *testing.T) {
	e := module.NewPlatformECS("fail-svc", map[string]any{
		"cluster": "staging-cluster",
		"account": "nonexistent-account",
	})
	app := module.NewMockApplication()
	if err := e.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

// TestPlatformECS_MissingCluster verifies Init fails when 'cluster' is not set.
func TestPlatformECS_MissingCluster(t *testing.T) {
	e := module.NewPlatformECS("no-cluster-svc", map[string]any{})
	app := module.NewMockApplication()
	if err := e.Init(app); err == nil {
		t.Error("expected error for missing cluster, got nil")
	}
}

// TestPlatformECS_TaskDefinitionPopulated verifies Apply populates the task definition.
func TestPlatformECS_TaskDefinitionPopulated(t *testing.T) {
	e := module.NewPlatformECS("td-svc", map[string]any{"cluster": "test-cluster"})
	app := module.NewMockApplication()
	if err := e.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	result, err := e.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	state, ok := result.State.(*module.ECSServiceState)
	if !ok {
		t.Fatalf("unexpected state type %T", result.State)
	}
	if state.TaskDefinition.Family == "" {
		t.Error("expected TaskDefinition.Family to be set")
	}
	if state.TaskDefinition.Revision != 1 {
		t.Errorf("expected Revision=1, got %d", state.TaskDefinition.Revision)
	}
	if len(state.TaskDefinition.Containers) == 0 {
		t.Error("expected at least one container in task definition")
	}
}

// TestPlatformECS_LoadBalancerConfigured verifies Apply sets up load balancer config.
func TestPlatformECS_LoadBalancerConfigured(t *testing.T) {
	e := module.NewPlatformECS("lb-svc", map[string]any{
		"cluster": "prod-cluster",
		"region":  "us-west-2",
	})
	app := module.NewMockApplication()
	if err := e.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	result, err := e.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	state := result.State.(*module.ECSServiceState)
	if state.LoadBalancer == nil {
		t.Fatal("expected LoadBalancer to be set after Apply")
	}
	if state.LoadBalancer.TargetGroupARN == "" {
		t.Error("expected non-empty TargetGroupARN")
	}
	if state.LoadBalancer.ContainerPort == 0 {
		t.Error("expected non-zero ContainerPort")
	}
}

// TestPlatformECS_DefaultDesiredCount verifies the default desired count is 1.
func TestPlatformECS_DefaultDesiredCount(t *testing.T) {
	e := module.NewPlatformECS("default-svc", map[string]any{"cluster": "cluster"})
	app := module.NewMockApplication()
	if err := e.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	result, err := e.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	state := result.State.(*module.ECSServiceState)
	if state.DesiredCount != 1 {
		t.Errorf("expected default DesiredCount=1, got %d", state.DesiredCount)
	}
}

// TestPlatformECS_DestroyIdempotent verifies calling Destroy twice does not error.
func TestPlatformECS_DestroyIdempotent(t *testing.T) {
	e := module.NewPlatformECS("idempotent-svc", map[string]any{"cluster": "cluster"})
	app := module.NewMockApplication()
	if err := e.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := e.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := e.Destroy(); err != nil {
		t.Fatalf("first Destroy: %v", err)
	}
	if err := e.Destroy(); err != nil {
		t.Errorf("second Destroy should be idempotent, got: %v", err)
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func setupECSApp(t *testing.T) (*module.MockApplication, *module.PlatformECS) {
	t.Helper()
	app := module.NewMockApplication()
	e := module.NewPlatformECS("my-ecs-svc", map[string]any{
		"cluster":     "test-cluster",
		"region":      "us-east-1",
		"launch_type": "FARGATE",
	})
	if err := e.Init(app); err != nil {
		t.Fatalf("ECS Init: %v", err)
	}
	return app, e
}

func TestECSPlanStep(t *testing.T) {
	app, _ := setupECSApp(t)
	factory := module.NewECSPlanStepFactory()
	step, err := factory("plan", map[string]any{"service": "my-ecs-svc"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["service"] != "my-ecs-svc" {
		t.Errorf("expected service=my-ecs-svc, got %v", result.Output["service"])
	}
	if result.Output["actions"] == nil {
		t.Error("expected actions in output")
	}
}

func TestECSApplyStep(t *testing.T) {
	app, _ := setupECSApp(t)
	factory := module.NewECSApplyStepFactory()
	step, err := factory("apply", map[string]any{"service": "my-ecs-svc"}, app)
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
}

func TestECSStatusStep(t *testing.T) {
	app, e := setupECSApp(t)

	if _, err := e.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	factory := module.NewECSStatusStepFactory()
	step, err := factory("status", map[string]any{"service": "my-ecs-svc"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["service"] != "my-ecs-svc" {
		t.Errorf("expected service=my-ecs-svc, got %v", result.Output["service"])
	}
	st := result.Output["status"].(*module.ECSServiceState)
	if st.Status != "running" {
		t.Errorf("expected status=running, got %q", st.Status)
	}
}

func TestECSDestroyStep(t *testing.T) {
	app, e := setupECSApp(t)

	if _, err := e.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	factory := module.NewECSDestroyStepFactory()
	step, err := factory("destroy", map[string]any{"service": "my-ecs-svc"}, app)
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

func TestECSPlanStep_MissingService(t *testing.T) {
	factory := module.NewECSPlanStepFactory()
	_, err := factory("plan", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestECSPlanStep_ServiceNotFound(t *testing.T) {
	factory := module.NewECSPlanStepFactory()
	step, err := factory("plan", map[string]any{"service": "ghost-svc"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing service in registry, got nil")
	}
}
