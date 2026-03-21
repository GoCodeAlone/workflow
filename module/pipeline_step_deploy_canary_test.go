package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Mock CanaryDriver ────────────────────────────────────────────────────────

type mockCanaryDriver struct {
	mockDeployDriver
	createCanaryErr   error
	routePercentErr   error
	metricGateErr     error
	promoteCanaryErr  error
	destroyCanaryErr  error

	createCanaryCalled  bool
	promoteCanaryCalled bool
	destroyCanaryCalled bool
	routePercents       []int
}

func (m *mockCanaryDriver) CreateCanary(_ context.Context, _ string) error {
	m.createCanaryCalled = true
	return m.createCanaryErr
}

func (m *mockCanaryDriver) RoutePercent(_ context.Context, pct int) error {
	m.routePercents = append(m.routePercents, pct)
	return m.routePercentErr
}

func (m *mockCanaryDriver) CheckMetricGate(_ context.Context, _ string) error {
	return m.metricGateErr
}

func (m *mockCanaryDriver) PromoteCanary(_ context.Context) error {
	m.promoteCanaryCalled = true
	return m.promoteCanaryErr
}

func (m *mockCanaryDriver) DestroyCanary(_ context.Context) error {
	m.destroyCanaryCalled = true
	return m.destroyCanaryErr
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func setupCanaryApp(t *testing.T) (*module.MockApplication, *mockCanaryDriver) {
	t.Helper()
	app := module.NewMockApplication()
	driver := &mockCanaryDriver{
		mockDeployDriver: mockDeployDriver{currentImage: "myapp:v1", replicaCount: 4},
	}
	if err := app.RegisterService("canary-svc", driver); err != nil {
		t.Fatalf("register service: %v", err)
	}
	return app, driver
}

func baseCanaryCfg() map[string]any {
	return map[string]any{
		"service": "canary-svc",
		"image":   "myapp:v2",
		"stages": []any{
			map[string]any{"percent": 10, "metric_gate": "error_rate"},
			map[string]any{"percent": 50, "metric_gate": "error_rate"},
			map[string]any{"percent": 100},
		},
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestDeployCanary_HappyPath(t *testing.T) {
	app, driver := setupCanaryApp(t)
	factory := module.NewDeployCanaryStepFactory()
	step, err := factory("canary", baseCanaryCfg(), app)
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
	if result.Output["promoted"] != true {
		t.Errorf("expected promoted=true, got %v", result.Output["promoted"])
	}
	if result.Output["stage_reached"] != 3 {
		t.Errorf("expected stage_reached=3, got %v", result.Output["stage_reached"])
	}
	if !driver.createCanaryCalled {
		t.Error("expected CreateCanary to be called")
	}
	if !driver.promoteCanaryCalled {
		t.Error("expected PromoteCanary to be called")
	}
	if len(driver.routePercents) != 3 {
		t.Errorf("expected 3 RoutePercent calls, got %d", len(driver.routePercents))
	}
}

func TestDeployCanary_MetricGateFailure_WithRollback(t *testing.T) {
	app, driver := setupCanaryApp(t)
	driver.metricGateErr = errors.New("error rate too high")

	cfg := baseCanaryCfg()
	cfg["rollback_on_failure"] = true

	factory := module.NewDeployCanaryStepFactory()
	step, _ := factory("canary", cfg, app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Fatal("expected error on metric gate failure, got nil")
	}
	if !driver.destroyCanaryCalled {
		t.Error("expected DestroyCanary to be called on gate failure with rollback")
	}
	if driver.promoteCanaryCalled {
		t.Error("PromoteCanary should not be called after gate failure")
	}
}

func TestDeployCanary_MetricGateFailure_NoRollback(t *testing.T) {
	app, driver := setupCanaryApp(t)
	driver.metricGateErr = errors.New("latency exceeded")

	factory := module.NewDeployCanaryStepFactory()
	step, _ := factory("canary", baseCanaryCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Fatal("expected error on metric gate failure, got nil")
	}
	if driver.destroyCanaryCalled {
		t.Error("DestroyCanary should not be called without rollback_on_failure")
	}
}

func TestDeployCanary_CreateCanaryError(t *testing.T) {
	app, driver := setupCanaryApp(t)
	driver.createCanaryErr = errors.New("cannot create canary instance")

	factory := module.NewDeployCanaryStepFactory()
	step, _ := factory("canary", baseCanaryCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error on CreateCanary failure, got nil")
	}
}

func TestDeployCanary_MissingService(t *testing.T) {
	factory := module.NewDeployCanaryStepFactory()
	_, err := factory("canary", map[string]any{"image": "myapp:v2"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestDeployCanary_MissingImage(t *testing.T) {
	factory := module.NewDeployCanaryStepFactory()
	_, err := factory("canary", map[string]any{"service": "svc"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing image, got nil")
	}
}

func TestDeployCanary_DefaultSingleStage(t *testing.T) {
	app, driver := setupCanaryApp(t)
	factory := module.NewDeployCanaryStepFactory()
	// No stages configured → defaults to single 100% stage.
	step, err := factory("canary", map[string]any{"service": "canary-svc", "image": "myapp:v2"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["total_stages"] != 1 {
		t.Errorf("expected total_stages=1, got %v", result.Output["total_stages"])
	}
	if !driver.promoteCanaryCalled {
		t.Error("expected PromoteCanary to be called on successful single-stage rollout")
	}
}
