package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// TestPlatformKubernetes_KindLifecycle tests the full plan→apply→status→destroy
// lifecycle using the in-memory kind backend.
func TestPlatformKubernetes_KindLifecycle(t *testing.T) {
	k := module.NewPlatformKubernetes("test-cluster", map[string]any{
		"type":    "kind",
		"version": "1.29",
		"nodeGroups": []any{
			map[string]any{
				"name":         "default",
				"instanceType": "t3.medium",
				"min":          2,
				"max":          10,
			},
		},
	})

	app := module.NewMockApplication()
	if err := k.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Plan — should propose a create action on a fresh cluster.
	plan, err := k.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("expected at least one plan action")
	}
	if plan.Actions[0].Type != "create" {
		t.Errorf("expected action=create, got %q", plan.Actions[0].Type)
	}

	// Apply — should create the cluster in-memory.
	result, err := k.Apply()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Apply success=true")
	}

	// Status — cluster should now be running.
	st, err := k.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	state, ok := st.(*module.KubernetesClusterState)
	if !ok {
		t.Fatalf("Status returned unexpected type %T", st)
	}
	if state.Status != "running" {
		t.Errorf("expected status=running, got %q", state.Status)
	}
	if state.Endpoint == "" {
		t.Error("expected non-empty endpoint after apply")
	}

	// Plan after apply — should produce a noop.
	plan2, err := k.Plan()
	if err != nil {
		t.Fatalf("second Plan failed: %v", err)
	}
	if len(plan2.Actions) == 0 || plan2.Actions[0].Type != "noop" {
		t.Errorf("expected noop after apply, got %+v", plan2.Actions)
	}

	// Destroy — cluster should be deleted.
	if err := k.Destroy(); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}
	st2, err := k.Status()
	if err != nil {
		t.Fatalf("Status after destroy failed: %v", err)
	}
	state2 := st2.(*module.KubernetesClusterState)
	if state2.Status != "deleted" {
		t.Errorf("expected status=deleted after destroy, got %q", state2.Status)
	}
}

// TestPlatformKubernetes_PlatformProviderInterface verifies PlatformKubernetes
// satisfies the PlatformProvider interface.
func TestPlatformKubernetes_PlatformProviderInterface(t *testing.T) {
	k := module.NewPlatformKubernetes("iface-cluster", map[string]any{"type": "kind"})
	app := module.NewMockApplication()
	if err := k.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var _ module.PlatformProvider = k
}

// TestPlatformKubernetes_CloudAccountResolution verifies the module resolves a
// cloud.account from the service registry during Init.
func TestPlatformKubernetes_CloudAccountResolution(t *testing.T) {
	acc := module.NewCloudAccount("mock-account", map[string]any{
		"provider": "mock",
		"region":   "us-east-1",
	})
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account Init: %v", err)
	}

	k := module.NewPlatformKubernetes("kind-cluster", map[string]any{
		"type":    "kind",
		"account": "mock-account",
	})
	if err := k.Init(app); err != nil {
		t.Fatalf("kubernetes Init: %v", err)
	}

	// Confirm the cluster module itself is also in the registry.
	svc, ok := app.Services["kind-cluster"]
	if !ok {
		t.Fatal("expected kind-cluster in service registry")
	}
	if _, ok := svc.(*module.PlatformKubernetes); !ok {
		t.Fatalf("registry entry is %T, want *PlatformKubernetes", svc)
	}
}

// TestPlatformKubernetes_InvalidAccount verifies Init fails gracefully when the
// referenced cloud.account does not exist.
func TestPlatformKubernetes_InvalidAccount(t *testing.T) {
	k := module.NewPlatformKubernetes("fail-cluster", map[string]any{
		"type":    "kind",
		"account": "nonexistent-account",
	})
	app := module.NewMockApplication()
	if err := k.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

// TestPlatformKubernetes_UnsupportedType verifies Init rejects unknown backends.
func TestPlatformKubernetes_UnsupportedType(t *testing.T) {
	k := module.NewPlatformKubernetes("bad-cluster", map[string]any{"type": "openshift"})
	app := module.NewMockApplication()
	if err := k.Init(app); err == nil {
		t.Error("expected error for unsupported type, got nil")
	}
}

// TestPlatformKubernetes_EKSStubPlan verifies that EKS Plan returns a stub action.
func TestPlatformKubernetes_EKSStubPlan(t *testing.T) {
	k := module.NewPlatformKubernetes("eks-cluster", map[string]any{"type": "eks"})
	app := module.NewMockApplication()
	if err := k.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plan, err := k.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("expected at least one action")
	}
	if plan.Provider != "eks" {
		t.Errorf("expected provider=eks, got %q", plan.Provider)
	}
}

// TestPlatformKubernetes_EKSApplyNotImplemented verifies EKS Apply returns an error.
func TestPlatformKubernetes_EKSApplyNotImplemented(t *testing.T) {
	k := module.NewPlatformKubernetes("eks-cluster", map[string]any{"type": "eks"})
	app := module.NewMockApplication()
	if err := k.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := k.Apply()
	if err == nil {
		t.Error("expected error from EKS Apply stub, got nil")
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func setupK8sApp(t *testing.T) (*module.MockApplication, *module.PlatformKubernetes) {
	t.Helper()
	app := module.NewMockApplication()
	k := module.NewPlatformKubernetes("my-cluster", map[string]any{
		"type":    "kind",
		"version": "1.29",
	})
	if err := k.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}
	return app, k
}

func TestK8sPlanStep(t *testing.T) {
	app, _ := setupK8sApp(t)
	factory := module.NewK8sPlanStepFactory()
	step, err := factory("plan", map[string]any{"cluster": "my-cluster"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["cluster"] != "my-cluster" {
		t.Errorf("expected cluster=my-cluster, got %v", result.Output["cluster"])
	}
	if result.Output["actions"] == nil {
		t.Error("expected actions in output")
	}
}

func TestK8sApplyStep(t *testing.T) {
	app, _ := setupK8sApp(t)
	factory := module.NewK8sApplyStepFactory()
	step, err := factory("apply", map[string]any{"cluster": "my-cluster"}, app)
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

func TestK8sStatusStep(t *testing.T) {
	app, k := setupK8sApp(t)

	// Apply first so there is meaningful state.
	if _, err := k.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	factory := module.NewK8sStatusStepFactory()
	step, err := factory("status", map[string]any{"cluster": "my-cluster"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["cluster"] != "my-cluster" {
		t.Errorf("expected cluster=my-cluster, got %v", result.Output["cluster"])
	}
	st := result.Output["status"].(*module.KubernetesClusterState)
	if st.Status != "running" {
		t.Errorf("expected status=running, got %q", st.Status)
	}
}

func TestK8sDestroyStep(t *testing.T) {
	app, k := setupK8sApp(t)

	// Apply so there is something to destroy.
	if _, err := k.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	factory := module.NewK8sDestroyStepFactory()
	step, err := factory("destroy", map[string]any{"cluster": "my-cluster"}, app)
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

func TestK8sPlanStep_MissingCluster(t *testing.T) {
	factory := module.NewK8sPlanStepFactory()
	_, err := factory("plan", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing cluster, got nil")
	}
}

func TestK8sPlanStep_ClusterNotFound(t *testing.T) {
	factory := module.NewK8sPlanStepFactory()
	step, err := factory("plan", map[string]any{"cluster": "ghost"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing cluster service, got nil")
	}
}
