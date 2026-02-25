package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func newDOAppApp(t *testing.T) (*module.MockApplication, *module.PlatformDOApp) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewPlatformDOApp("my-app", map[string]any{
		"provider":  "mock",
		"name":      "my-web-app",
		"region":    "nyc",
		"image":     "registry.example.com/my-app:v1.0.0",
		"instances": 2,
		"http_port": 8080,
		"envs": map[string]any{
			"APP_ENV": "production",
			"PORT":    "8080",
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module lifecycle ─────────────────────────────────────────────────────────

func TestDO_App_Init(t *testing.T) {
	_, m := newDOAppApp(t)
	if m.Name() != "my-app" {
		t.Errorf("expected name=my-app, got %q", m.Name())
	}
}

func TestDO_App_InitRegistersService(t *testing.T) {
	app, _ := newDOAppApp(t)
	svc, ok := app.Services["my-app"]
	if !ok {
		t.Fatal("expected my-app in service registry")
	}
	if _, ok := svc.(*module.PlatformDOApp); !ok {
		t.Fatalf("registry entry is %T, want *PlatformDOApp", svc)
	}
}

func TestDO_App_Deploy(t *testing.T) {
	_, m := newDOAppApp(t)
	state, err := m.Deploy()
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if state.Status != "running" {
		t.Errorf("expected status=running, got %q", state.Status)
	}
	if state.ID == "" {
		t.Error("expected non-empty app ID after deploy")
	}
	if state.LiveURL == "" {
		t.Error("expected non-empty LiveURL after deploy")
	}
	if state.DeploymentID == "" {
		t.Error("expected non-empty DeploymentID after deploy")
	}
}

func TestDO_App_Status(t *testing.T) {
	_, m := newDOAppApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	state, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.Status != "running" {
		t.Errorf("expected status=running, got %q", state.Status)
	}
}

func TestDO_App_Logs(t *testing.T) {
	_, m := newDOAppApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	logs, err := m.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if logs == "" {
		t.Error("expected non-empty logs")
	}
}

func TestDO_App_Logs_NotDeployed(t *testing.T) {
	_, m := newDOAppApp(t)
	_, err := m.Logs()
	if err == nil {
		t.Error("expected error for logs on undeployed app, got nil")
	}
}

func TestDO_App_Scale(t *testing.T) {
	_, m := newDOAppApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	state, err := m.Scale(5)
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if state.Instances != 5 {
		t.Errorf("expected instances=5, got %d", state.Instances)
	}
}

func TestDO_App_Destroy(t *testing.T) {
	_, m := newDOAppApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	state, err := m.Status()
	if err != nil {
		t.Fatalf("Status after destroy: %v", err)
	}
	if state.Status != "deleted" {
		t.Errorf("expected status=deleted, got %q", state.Status)
	}
	if state.LiveURL != "" {
		t.Error("expected empty LiveURL after destroy")
	}
}

func TestDO_App_DestroyIdempotent(t *testing.T) {
	_, m := newDOAppApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("first Destroy: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Errorf("second Destroy should be idempotent, got: %v", err)
	}
}

func TestDO_App_UnsupportedProvider(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDOApp("bad-app", map[string]any{
		"provider": "gcp",
		"name":     "bad",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

func TestDO_App_InvalidAccountRef(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformDOApp("fail-app", map[string]any{
		"provider": "mock",
		"account":  "nonexistent",
		"name":     "fail",
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

// ─── pipeline steps ───────────────────────────────────────────────────────────

func setupDOAppStepApp(t *testing.T) (*module.MockApplication, *module.PlatformDOApp) {
	t.Helper()
	return newDOAppApp(t)
}

func TestDO_DeployStep(t *testing.T) {
	app, _ := setupDOAppStepApp(t)
	factory := module.NewDODeployStepFactory()
	step, err := factory("deploy", map[string]any{"app": "my-app"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["app"] != "my-app" {
		t.Errorf("expected app=my-app, got %v", result.Output["app"])
	}
	if result.Output["status"] != "running" {
		t.Errorf("expected status=running, got %v", result.Output["status"])
	}
}

func TestDO_StatusStep(t *testing.T) {
	app, m := setupDOAppStepApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	factory := module.NewDOStatusStepFactory()
	step, err := factory("status", map[string]any{"app": "my-app"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["app"] != "my-app" {
		t.Errorf("expected app=my-app, got %v", result.Output["app"])
	}
}

func TestDO_LogsStep(t *testing.T) {
	app, m := setupDOAppStepApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	factory := module.NewDOLogsStepFactory()
	step, err := factory("logs", map[string]any{"app": "my-app"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["logs"] == "" {
		t.Error("expected non-empty logs in output")
	}
}

func TestDO_ScaleStep(t *testing.T) {
	app, m := setupDOAppStepApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	factory := module.NewDOScaleStepFactory()
	step, err := factory("scale", map[string]any{"app": "my-app", "instances": 4}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["instances"] != 4 {
		t.Errorf("expected instances=4, got %v", result.Output["instances"])
	}
}

func TestDO_DestroyStep(t *testing.T) {
	app, m := setupDOAppStepApp(t)
	if _, err := m.Deploy(); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	factory := module.NewDODestroyStepFactory()
	step, err := factory("destroy", map[string]any{"app": "my-app"}, app)
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

func TestDO_DeployStep_MissingApp(t *testing.T) {
	factory := module.NewDODeployStepFactory()
	_, err := factory("deploy", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing app, got nil")
	}
}

func TestDO_DeployStep_AppNotFound(t *testing.T) {
	factory := module.NewDODeployStepFactory()
	step, err := factory("deploy", map[string]any{"app": "ghost"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing app in registry, got nil")
	}
}
