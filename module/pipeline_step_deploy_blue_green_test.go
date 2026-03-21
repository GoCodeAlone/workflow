package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Mock BlueGreenDriver ─────────────────────────────────────────────────────

type mockBlueGreenDriver struct {
	mockDeployDriver
	createGreenErr   error
	switchTrafficErr error
	destroyBlueErr   error
	greenEndpoint    string
	greenEndpointErr error
	createGreenCalled   bool
	switchTrafficCalled bool
	destroyBlueCalled   bool
}

func (m *mockBlueGreenDriver) CreateGreen(_ context.Context, _ string) error {
	m.createGreenCalled = true
	return m.createGreenErr
}

func (m *mockBlueGreenDriver) SwitchTraffic(_ context.Context) error {
	m.switchTrafficCalled = true
	return m.switchTrafficErr
}

func (m *mockBlueGreenDriver) DestroyBlue(_ context.Context) error {
	m.destroyBlueCalled = true
	return m.destroyBlueErr
}

func (m *mockBlueGreenDriver) GreenEndpoint(_ context.Context) (string, error) {
	return m.greenEndpoint, m.greenEndpointErr
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func setupBlueGreenApp(t *testing.T) (*module.MockApplication, *mockBlueGreenDriver) {
	t.Helper()
	app := module.NewMockApplication()
	driver := &mockBlueGreenDriver{
		mockDeployDriver: mockDeployDriver{currentImage: "myapp:v1", replicaCount: 2},
		greenEndpoint:    "http://green.example.com",
	}
	if err := app.RegisterService("bg-svc", driver); err != nil {
		t.Fatalf("register service: %v", err)
	}
	return app, driver
}

func baseBluGreenCfg() map[string]any {
	return map[string]any{
		"service": "bg-svc",
		"image":   "myapp:v2",
		"health_check": map[string]any{
			"path":    "/health",
			"timeout": "1s",
		},
		"traffic_switch": "lb",
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestDeployBlueGreen_HappyPath(t *testing.T) {
	app, driver := setupBlueGreenApp(t)
	factory := module.NewDeployBlueGreenStepFactory()
	step, err := factory("bg", baseBluGreenCfg(), app)
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
	if result.Output["green_endpoint"] != "http://green.example.com" {
		t.Errorf("unexpected green_endpoint: %v", result.Output["green_endpoint"])
	}
	if !driver.createGreenCalled {
		t.Error("expected CreateGreen to be called")
	}
	if !driver.switchTrafficCalled {
		t.Error("expected SwitchTraffic to be called")
	}
	if !driver.destroyBlueCalled {
		t.Error("expected DestroyBlue to be called")
	}
}

func TestDeployBlueGreen_CreateGreenError(t *testing.T) {
	app, driver := setupBlueGreenApp(t)
	driver.createGreenErr = errors.New("quota exceeded")

	factory := module.NewDeployBlueGreenStepFactory()
	step, _ := factory("bg", baseBluGreenCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error on CreateGreen failure, got nil")
	}
}

func TestDeployBlueGreen_HealthCheckError(t *testing.T) {
	app, driver := setupBlueGreenApp(t)
	driver.healthErr = errors.New("green is unhealthy")

	factory := module.NewDeployBlueGreenStepFactory()
	step, _ := factory("bg", baseBluGreenCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error on health check failure, got nil")
	}
	// Traffic should NOT have been switched.
	if driver.switchTrafficCalled {
		t.Error("SwitchTraffic should not be called when health check fails")
	}
}

func TestDeployBlueGreen_SwitchTrafficError(t *testing.T) {
	app, driver := setupBlueGreenApp(t)
	driver.switchTrafficErr = errors.New("lb update failed")

	factory := module.NewDeployBlueGreenStepFactory()
	step, _ := factory("bg", baseBluGreenCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error on SwitchTraffic failure, got nil")
	}
}

func TestDeployBlueGreen_MissingService(t *testing.T) {
	factory := module.NewDeployBlueGreenStepFactory()
	_, err := factory("bg", map[string]any{"image": "myapp:v2"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestDeployBlueGreen_MissingImage(t *testing.T) {
	factory := module.NewDeployBlueGreenStepFactory()
	_, err := factory("bg", map[string]any{"service": "svc"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing image, got nil")
	}
}

func TestDeployBlueGreen_ServiceNotFound(t *testing.T) {
	factory := module.NewDeployBlueGreenStepFactory()
	step, _ := factory("bg", map[string]any{"service": "ghost", "image": "myapp:v2"}, module.NewMockApplication())
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}
