package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Helper ───────────────────────────────────────────────────────────────────

func setupRollbackApp(t *testing.T) (*module.MockApplication, *mockDeployDriver, *module.MemoryDeployHistoryStore) {
	t.Helper()
	app := module.NewMockApplication()
	driver := &mockDeployDriver{currentImage: "myapp:v2", replicaCount: 3}
	store := module.NewMemoryDeployHistoryStore()

	// Seed history: v2 is current, v1 is previous.
	if err := store.RecordDeploy("rb-svc", "myapp:v1", "v1"); err != nil {
		t.Fatalf("record v1: %v", err)
	}
	if err := store.RecordDeploy("rb-svc", "myapp:v2", "v2"); err != nil {
		t.Fatalf("record v2: %v", err)
	}

	if err := app.RegisterService("rb-svc", driver); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	if err := app.RegisterService("deploy-history", store); err != nil {
		t.Fatalf("register store: %v", err)
	}
	return app, driver, store
}

func baseRollbackCfg() map[string]any {
	return map[string]any{
		"service":        "rb-svc",
		"history_store":  "deploy-history",
		"target_version": "previous",
		"health_check": map[string]any{
			"path":    "/health",
			"timeout": "1s",
		},
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestDeployRollback_ToPrevious(t *testing.T) {
	app, driver, _ := setupRollbackApp(t)
	factory := module.NewDeployRollbackStepFactory()
	step, err := factory("rb", baseRollbackCfg(), app)
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
	if result.Output["rolled_back_to"] != "v1" {
		t.Errorf("expected rolled_back_to=v1, got %v", result.Output["rolled_back_to"])
	}
	if result.Output["image"] != "myapp:v1" {
		t.Errorf("expected image=myapp:v1, got %v", result.Output["image"])
	}
	// Driver should have been updated to v1.
	if len(driver.updateCalls) == 0 || driver.updateCalls[0] != "myapp:v1" {
		t.Errorf("expected driver update to myapp:v1, got %v", driver.updateCalls)
	}
}

func TestDeployRollback_ToSpecificVersion(t *testing.T) {
	app, driver, _ := setupRollbackApp(t)

	cfg := baseRollbackCfg()
	cfg["target_version"] = "v1"

	factory := module.NewDeployRollbackStepFactory()
	step, _ := factory("rb", cfg, app)
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["rolled_back_to"] != "v1" {
		t.Errorf("expected rolled_back_to=v1, got %v", result.Output["rolled_back_to"])
	}
	_ = driver
}

func TestDeployRollback_VersionNotFound(t *testing.T) {
	app, _, _ := setupRollbackApp(t)

	cfg := baseRollbackCfg()
	cfg["target_version"] = "v99"

	factory := module.NewDeployRollbackStepFactory()
	step, _ := factory("rb", cfg, app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for non-existent version, got nil")
	}
}

func TestDeployRollback_HealthCheckFails(t *testing.T) {
	app, driver, _ := setupRollbackApp(t)
	driver.healthErr = errors.New("service unavailable after rollback")

	factory := module.NewDeployRollbackStepFactory()
	step, _ := factory("rb", baseRollbackCfg(), app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error when health check fails after rollback, got nil")
	}
}

func TestDeployRollback_MissingService(t *testing.T) {
	factory := module.NewDeployRollbackStepFactory()
	_, err := factory("rb", map[string]any{"history_store": "hs"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestDeployRollback_MissingHistoryStore(t *testing.T) {
	factory := module.NewDeployRollbackStepFactory()
	_, err := factory("rb", map[string]any{"service": "svc"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing history_store, got nil")
	}
}

func TestDeployRollback_ServiceNotFound(t *testing.T) {
	app := module.NewMockApplication()
	store := module.NewMemoryDeployHistoryStore()
	_ = app.RegisterService("deploy-history", store)

	factory := module.NewDeployRollbackStepFactory()
	step, _ := factory("rb", map[string]any{
		"service":       "ghost",
		"history_store": "deploy-history",
	}, app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing service in registry, got nil")
	}
}

func TestDeployRollback_HistoryStoreNotFound(t *testing.T) {
	app := module.NewMockApplication()
	driver := &mockDeployDriver{currentImage: "myapp:v2", replicaCount: 2}
	_ = app.RegisterService("rb-svc", driver)

	factory := module.NewDeployRollbackStepFactory()
	step, _ := factory("rb", map[string]any{
		"service":       "rb-svc",
		"history_store": "ghost-store",
	}, app)
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing history store in registry, got nil")
	}
}
