package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Mock DeployDriver ────────────────────────────────────────────────────────

type mockDeployDriver struct {
	currentImage    string
	replicaCount    int
	updateCalls     []string
	updateErr       error
	healthErr       error
	currentImageErr error
	replicaCountErr error
}

func (m *mockDeployDriver) Update(_ context.Context, image string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updateCalls = append(m.updateCalls, image)
	m.currentImage = image
	return nil
}

func (m *mockDeployDriver) HealthCheck(_ context.Context, _ string) error {
	return m.healthErr
}

func (m *mockDeployDriver) CurrentImage(_ context.Context) (string, error) {
	return m.currentImage, m.currentImageErr
}

func (m *mockDeployDriver) ReplicaCount(_ context.Context) (int, error) {
	return m.replicaCount, m.replicaCountErr
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func setupRollingApp(t *testing.T) (*module.MockApplication, *mockDeployDriver) {
	t.Helper()
	app := module.NewMockApplication()
	driver := &mockDeployDriver{currentImage: "myapp:v1", replicaCount: 3}
	if err := app.RegisterService("my-svc", driver); err != nil {
		t.Fatalf("register service: %v", err)
	}
	return app, driver
}

func baseRollingCfg() map[string]any {
	return map[string]any{
		"service":         "my-svc",
		"image":           "myapp:v2",
		"max_surge":       1,
		"max_unavailable": 1,
		"health_check": map[string]any{
			"path":    "/health",
			"timeout": "1s",
		},
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestDeployRolling_HappyPath(t *testing.T) {
	app, driver := setupRollingApp(t)
	factory := module.NewDeployRollingStepFactory()
	step, err := factory("roll", baseRollingCfg(), app)
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
	if result.Output["image"] != "myapp:v2" {
		t.Errorf("expected image=myapp:v2, got %v", result.Output["image"])
	}
	if result.Output["previous_image"] != "myapp:v1" {
		t.Errorf("expected previous_image=myapp:v1, got %v", result.Output["previous_image"])
	}
	if len(driver.updateCalls) == 0 {
		t.Error("expected at least one Update call")
	}
}

func TestDeployRolling_HealthCheckFailure_WithRollback(t *testing.T) {
	app, driver := setupRollingApp(t)
	driver.healthErr = errors.New("service unhealthy")

	cfg := baseRollingCfg()
	cfg["rollback_on_failure"] = true

	factory := module.NewDeployRollingStepFactory()
	step, err := factory("roll", cfg, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Fatal("expected error on health check failure, got nil")
	}
	// Should have attempted a rollback — last update should be to previous image.
	if len(driver.updateCalls) < 2 {
		t.Errorf("expected at least 2 update calls (deploy + rollback), got %d", len(driver.updateCalls))
	}
	last := driver.updateCalls[len(driver.updateCalls)-1]
	if last != "myapp:v1" {
		t.Errorf("expected last update to be rollback image myapp:v1, got %q", last)
	}
}

func TestDeployRolling_HealthCheckFailure_NoRollback(t *testing.T) {
	app, driver := setupRollingApp(t)
	driver.healthErr = errors.New("service unhealthy")

	factory := module.NewDeployRollingStepFactory()
	step, _ := factory("roll", baseRollingCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Fatal("expected error on health check failure, got nil")
	}
	// No rollback: last update should be to new image.
	if len(driver.updateCalls) == 0 {
		t.Error("expected at least one update call")
	}
	last := driver.updateCalls[len(driver.updateCalls)-1]
	if last != "myapp:v2" {
		t.Errorf("expected last update to be new image myapp:v2, got %q", last)
	}
}

func TestDeployRolling_MissingService(t *testing.T) {
	factory := module.NewDeployRollingStepFactory()
	_, err := factory("roll", map[string]any{"image": "myapp:v2"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestDeployRolling_MissingImage(t *testing.T) {
	factory := module.NewDeployRollingStepFactory()
	_, err := factory("roll", map[string]any{"service": "svc"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing image, got nil")
	}
}

func TestDeployRolling_ServiceNotFound(t *testing.T) {
	factory := module.NewDeployRollingStepFactory()
	step, err := factory("roll", map[string]any{"service": "ghost", "image": "myapp:v2"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestDeployRolling_UpdateError(t *testing.T) {
	app, driver := setupRollingApp(t)
	driver.updateErr = errors.New("cannot update: out of capacity")

	factory := module.NewDeployRollingStepFactory()
	step, _ := factory("roll", baseRollingCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error on update failure, got nil")
	}
}
