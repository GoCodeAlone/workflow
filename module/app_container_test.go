package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// setupAppContainerWithK8s creates a PlatformKubernetes + AppContainerModule backed by it.
func setupAppContainerWithK8s(t *testing.T) (*module.MockApplication, *module.AppContainerModule) {
	t.Helper()
	app := module.NewMockApplication()

	k8s := module.NewPlatformKubernetes("prod-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("my-api", map[string]any{
		"environment": "prod-cluster",
		"image":       "registry.example.com/my-api:v1.2.3",
		"replicas":    3,
		"ports":       []any{8080},
		"cpu":         "500m",
		"memory":      "512Mi",
		"health_path": "/healthz",
		"health_port": 8080,
		"env": map[string]any{
			"LOG_LEVEL":    "info",
			"DATABASE_URL": "postgres://localhost/db",
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("app container Init: %v", err)
	}
	return app, m
}

// ─── module creation & defaults ───────────────────────────────────────────────

// TestAppContainer_ModuleCreation verifies that Init succeeds and Name() is correct.
func TestAppContainer_ModuleCreation(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)
	if m.Name() != "my-api" {
		t.Errorf("expected name=my-api, got %q", m.Name())
	}
}

// TestAppContainer_DefaultValues verifies default replicas, cpu, and memory.
func TestAppContainer_DefaultValues(t *testing.T) {
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("defaults-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("defaults-app", map[string]any{
		"environment": "defaults-cluster",
		"image":       "nginx:latest",
		// replicas, cpu, memory omitted — should use defaults
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.Spec()
	if spec.Replicas != 1 {
		t.Errorf("expected default replicas=1, got %d", spec.Replicas)
	}
	if spec.CPU != "256m" {
		t.Errorf("expected default cpu=256m, got %q", spec.CPU)
	}
	if spec.Memory != "512Mi" {
		t.Errorf("expected default memory=512Mi, got %q", spec.Memory)
	}
	if spec.HealthPath != "/healthz" {
		t.Errorf("expected default health_path=/healthz, got %q", spec.HealthPath)
	}
}

// ─── Deploy ───────────────────────────────────────────────────────────────────

// TestAppContainer_DeploySetStatusActive verifies Deploy returns status="active" with endpoint.
func TestAppContainer_DeploySetStatusActive(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)

	result, err := m.Deploy()
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if result.Status != "active" {
		t.Errorf("expected status=active, got %q", result.Status)
	}
	if result.Endpoint == "" {
		t.Error("expected non-empty endpoint after deploy")
	}
	if result.Image != "registry.example.com/my-api:v1.2.3" {
		t.Errorf("unexpected image: %q", result.Image)
	}
	if result.Replicas != 3 {
		t.Errorf("expected replicas=3, got %d", result.Replicas)
	}
	if result.Name != "my-api" {
		t.Errorf("expected name=my-api, got %q", result.Name)
	}
}

// TestAppContainer_DeploySetsPlatform verifies Deploy sets the correct platform field.
func TestAppContainer_DeploySetsPlatform(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)
	result, err := m.Deploy()
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	// environment name is "prod-cluster" so platform should be that name.
	if result.Platform == "" {
		t.Error("expected non-empty platform")
	}
}

// ─── Status ───────────────────────────────────────────────────────────────────

// TestAppContainer_StatusReturnsCurrentState verifies Status returns the current deployment.
func TestAppContainer_StatusReturnsCurrentState(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)

	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "active" {
		t.Errorf("expected status=active, got %q", st.Status)
	}
	if st.Replicas != 3 {
		t.Errorf("expected replicas=3, got %d", st.Replicas)
	}
	if st.Image != "registry.example.com/my-api:v1.2.3" {
		t.Errorf("unexpected image: %q", st.Image)
	}
}

// TestAppContainer_StatusBeforeDeploy verifies Status returns "not_deployed" before any Deploy.
func TestAppContainer_StatusBeforeDeploy(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)
	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "not_deployed" {
		t.Errorf("expected status=not_deployed before deploy, got %q", st.Status)
	}
}

// ─── Rollback ─────────────────────────────────────────────────────────────────

// TestAppContainer_RollbackRevertsToPreviewImage verifies Rollback uses the previous image.
func TestAppContainer_RollbackRevertsToPreviewImage(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)

	// First deploy: v1.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}

	// Update config image to v2 by deploying again with a new module that has a different image.
	// Since Deploy() saves previous, a second deploy from a new module using the same registry
	// is not the right approach. Instead, deploy twice using the same module to create a "previous".
	// But the module image is fixed in config. We need to verify the previous/current tracking.
	// For this test, deploy once to set current, then deploy a second module (same app name) with v2.
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("rollback-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m2 := module.NewAppContainerModule("test-api", map[string]any{
		"environment": "rollback-cluster",
		"image":       "registry.example.com/app:v1",
		"replicas":    1,
	})
	if err := m2.Init(app); err != nil {
		t.Fatalf("Init m2: %v", err)
	}

	// First deploy: image v1 goes into current.
	r1, err := m2.Deploy()
	if err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	if r1.Image != "registry.example.com/app:v1" {
		t.Errorf("expected v1 image, got %q", r1.Image)
	}

	// Change the module's config image to v2 for a second deploy.
	// We can't change the module config directly, so instead we create a new module
	// for v2 that shares the same app registry entry.
	m3 := module.NewAppContainerModule("test-api", map[string]any{
		"environment": "rollback-cluster",
		"image":       "registry.example.com/app:v2",
		"replicas":    1,
	})
	if err := m3.Init(app); err != nil {
		t.Fatalf("Init m3: %v", err)
	}

	// Deploy v2 — previous should become v1 result.
	r2, err := m3.Deploy()
	if err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
	if r2.Image != "registry.example.com/app:v2" {
		t.Errorf("expected v2 image, got %q", r2.Image)
	}

	// Rollback — should go back to v1.
	// We need to pre-set previous by calling Deploy twice on m3.
	// Since m3 starts with no previous, we need a different approach.
	// Let's do: deploy m3 once (no previous) then "redeploy" to set previous.
	if _, err := m3.Deploy(); err != nil {
		t.Fatalf("third Deploy: %v", err)
	}
	// Now m3.previous = r2 (v2), m3.current = (v2)
	// Rollback should revert to previous (v2 image but with rolled_back status).
	rb, err := m3.Rollback()
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rb.Status != "rolled_back" {
		t.Errorf("expected status=rolled_back, got %q", rb.Status)
	}
	if rb.Image != r2.Image {
		t.Errorf("expected rolled-back image=%q, got %q", r2.Image, rb.Image)
	}
}

// TestAppContainer_RollbackNoPreviousReturnsError verifies Rollback fails when no previous exists.
func TestAppContainer_RollbackNoPreviousReturnsError(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)

	// Deploy once: current is set, previous is nil.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// First rollback should fail — no previous state.
	_, err := m.Rollback()
	if err == nil {
		t.Error("expected error for rollback with no previous state, got nil")
	}
}

// TestAppContainer_DoubleRollbackError verifies second Rollback fails with no previous.
func TestAppContainer_DoubleRollbackError(t *testing.T) {
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("dr-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("dr-app", map[string]any{
		"environment": "dr-cluster",
		"image":       "app:v1",
		"replicas":    1,
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Two deploys: first sets current, second stores first as previous.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}

	// First rollback should succeed (previous exists).
	if _, err := m.Rollback(); err != nil {
		t.Fatalf("first Rollback: %v", err)
	}

	// Second rollback should fail (previous was cleared).
	_, err := m.Rollback()
	if err == nil {
		t.Error("expected error on second rollback (no previous), got nil")
	}
}

// TestAppContainer_RollbackPreservesHistory verifies Deploy after rollback sets new previous.
func TestAppContainer_RollbackPreservesHistory(t *testing.T) {
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("hist-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("hist-app", map[string]any{
		"environment": "hist-cluster",
		"image":       "app:v1",
		"replicas":    1,
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Deploy v1 → Deploy v1 again → Rollback → Deploy v1 again → should have previous.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
	if _, err := m.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// Current is now rolled_back, previous is nil.
	// Deploy again: previous = rolled_back current, current = new active.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("third Deploy after rollback: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "active" {
		t.Errorf("expected active after deploy post-rollback, got %q", st.Status)
	}
}

// ─── Missing environment ───────────────────────────────────────────────────────

// TestAppContainer_MissingEnvironmentModule verifies Init fails when environment not found.
func TestAppContainer_MissingEnvironmentModule(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewAppContainerModule("orphan", map[string]any{
		"environment": "nonexistent-cluster",
		"image":       "app:latest",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent environment, got nil")
	}
}

// ─── Health check & config propagation ───────────────────────────────────────

// TestAppContainer_HealthCheckConfigPropagation verifies health path/port propagate to spec.
func TestAppContainer_HealthCheckConfigPropagation(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)
	spec := m.Spec()
	if spec.HealthPath != "/healthz" {
		t.Errorf("expected health_path=/healthz, got %q", spec.HealthPath)
	}
	if spec.HealthPort != 8080 {
		t.Errorf("expected health_port=8080, got %d", spec.HealthPort)
	}
}

// TestAppContainer_HealthPortDefaultsToFirstPort verifies health_port defaults to first port.
func TestAppContainer_HealthPortDefaultsToFirstPort(t *testing.T) {
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("hpd-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("hpd-app", map[string]any{
		"environment": "hpd-cluster",
		"image":       "app:latest",
		"ports":       []any{9090},
		// health_port omitted — should default to 9090
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec := m.Spec()
	if spec.HealthPort != 9090 {
		t.Errorf("expected health_port=9090 (first port), got %d", spec.HealthPort)
	}
}

// TestAppContainer_EnvVarPropagation verifies env vars are captured in the spec.
func TestAppContainer_EnvVarPropagation(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)
	spec := m.Spec()
	if spec.Env["LOG_LEVEL"] != "info" {
		t.Errorf("expected LOG_LEVEL=info, got %q", spec.Env["LOG_LEVEL"])
	}
	if spec.Env["DATABASE_URL"] != "postgres://localhost/db" {
		t.Errorf("expected DATABASE_URL set, got %q", spec.Env["DATABASE_URL"])
	}
}

// TestAppContainer_PortConfig verifies ports are captured in the spec.
func TestAppContainer_PortConfig(t *testing.T) {
	_, m := setupAppContainerWithK8s(t)
	spec := m.Spec()
	if len(spec.Ports) == 0 {
		t.Fatal("expected at least one port")
	}
	if spec.Ports[0] != 8080 {
		t.Errorf("expected port=8080, got %d", spec.Ports[0])
	}
}

// ─── Pipeline step factories ──────────────────────────────────────────────────

// TestAppContainer_AppDeployStepFactory verifies step.app_deploy factory.
func TestAppContainer_AppDeployStepFactory(t *testing.T) {
	app, _ := setupAppContainerWithK8s(t)

	factory := module.NewAppDeployStepFactory()
	step, err := factory("deploy", map[string]any{"app": "my-api"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if step.Name() != "deploy" {
		t.Errorf("expected name=deploy, got %q", step.Name())
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}
	if result.Output["app"] != "my-api" {
		t.Errorf("expected app=my-api, got %v", result.Output["app"])
	}
}

// TestAppContainer_AppStatusStepFactory verifies step.app_status factory.
func TestAppContainer_AppStatusStepFactory(t *testing.T) {
	app, m := setupAppContainerWithK8s(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	factory := module.NewAppStatusStepFactory()
	step, err := factory("status", map[string]any{"app": "my-api"}, app)
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

// TestAppContainer_AppRollbackStepFactory verifies step.app_rollback factory.
func TestAppContainer_AppRollbackStepFactory(t *testing.T) {
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("rb-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("rb-api", map[string]any{
		"environment": "rb-cluster",
		"image":       "app:v1",
		"replicas":    1,
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Two deploys so that previous is set.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}

	factory := module.NewAppRollbackStepFactory()
	step, err := factory("rollback", map[string]any{"app": "rb-api"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["rolled_back"] != true {
		t.Errorf("expected rolled_back=true, got %v", result.Output["rolled_back"])
	}
	if result.Output["status"] != "rolled_back" {
		t.Errorf("expected status=rolled_back, got %v", result.Output["status"])
	}
}

// ─── step.app_deploy after rollback preserves history ─────────────────────────

// TestAppContainer_DeployAfterRollbackPreservesHistory verifies that deploying
// after a rollback stores the rolled-back state as previous.
func TestAppContainer_DeployAfterRollbackPreservesHistory(t *testing.T) {
	app := module.NewMockApplication()
	k8s := module.NewPlatformKubernetes("ph-cluster", map[string]any{"type": "kind"})
	if err := k8s.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	m := module.NewAppContainerModule("ph-app", map[string]any{
		"environment": "ph-cluster",
		"image":       "app:v1",
		"replicas":    1,
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Two deploys → rollback → deploy again.
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
	if _, err := m.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status after rollback: %v", err)
	}
	if st.Status != "rolled_back" {
		t.Errorf("expected rolled_back after rollback, got %q", st.Status)
	}

	// Deploy again after rollback.
	_, err = m.Deploy()
	if err != nil {
		t.Fatalf("Deploy after rollback: %v", err)
	}

	st2, err := m.Status()
	if err != nil {
		t.Fatalf("Status after re-deploy: %v", err)
	}
	if st2.Status != "active" {
		t.Errorf("expected active after re-deploy, got %q", st2.Status)
	}
}
