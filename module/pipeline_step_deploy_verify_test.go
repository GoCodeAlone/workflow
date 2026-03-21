package module_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Mock with MetricsProvider ────────────────────────────────────────────────

type mockDeployDriverWithMetrics struct {
	mockDeployDriver
	metricValue float64
	metricErr   error
}

func (m *mockDeployDriverWithMetrics) QueryMetric(_ context.Context, _ string, _ time.Duration) (float64, error) {
	return m.metricValue, m.metricErr
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func setupVerifyApp(t *testing.T) (*module.MockApplication, *mockDeployDriverWithMetrics) {
	t.Helper()
	app := module.NewMockApplication()
	driver := &mockDeployDriverWithMetrics{
		mockDeployDriver: mockDeployDriver{currentImage: "myapp:v2", replicaCount: 3},
	}
	if err := app.RegisterService("verify-svc", driver); err != nil {
		t.Fatalf("register service: %v", err)
	}
	return app, driver
}

func baseVerifyCfg() map[string]any {
	return map[string]any{
		"service": "verify-svc",
		"checks": []any{
			map[string]any{"type": "http", "path": "/health", "expected_status": 200},
		},
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestDeployVerify_HappyPath(t *testing.T) {
	app, _ := setupVerifyApp(t)
	factory := module.NewDeployVerifyStepFactory()
	step, err := factory("verify", baseVerifyCfg(), app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["all_passed"] != true {
		t.Errorf("expected all_passed=true, got %v", result.Output["all_passed"])
	}
	if result.Output["check_count"] != 1 {
		t.Errorf("expected check_count=1, got %v", result.Output["check_count"])
	}
}

func TestDeployVerify_HTTPCheckFails(t *testing.T) {
	app, driver := setupVerifyApp(t)
	driver.healthErr = errors.New("connection refused")

	factory := module.NewDeployVerifyStepFactory()
	step, _ := factory("verify", baseVerifyCfg(), app)
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Execute returns nil error — check results instead.
	if result.Output["all_passed"] != false {
		t.Errorf("expected all_passed=false, got %v", result.Output["all_passed"])
	}
}

func TestDeployVerify_MetricsCheckPasses(t *testing.T) {
	app, driver := setupVerifyApp(t)
	driver.metricValue = 0.01

	cfg := map[string]any{
		"service": "verify-svc",
		"checks": []any{
			map[string]any{
				"type":      "metrics",
				"query":     "error_rate",
				"threshold": float64(0.05),
				"window":    "5m",
			},
		},
	}

	factory := module.NewDeployVerifyStepFactory()
	step, _ := factory("verify", cfg, app)
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["all_passed"] != true {
		t.Errorf("expected all_passed=true, got %v", result.Output["all_passed"])
	}
}

func TestDeployVerify_MetricsCheckExceedsThreshold(t *testing.T) {
	app, driver := setupVerifyApp(t)
	driver.metricValue = 0.15 // above threshold of 0.05

	cfg := map[string]any{
		"service": "verify-svc",
		"checks": []any{
			map[string]any{
				"type":      "metrics",
				"query":     "error_rate",
				"threshold": float64(0.05),
				"window":    "5m",
			},
		},
	}

	factory := module.NewDeployVerifyStepFactory()
	step, _ := factory("verify", cfg, app)
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["all_passed"] != false {
		t.Errorf("expected all_passed=false when metric exceeds threshold, got %v", result.Output["all_passed"])
	}
}

func TestDeployVerify_MultipleChecks_AllPass(t *testing.T) {
	app, _ := setupVerifyApp(t)
	cfg := map[string]any{
		"service": "verify-svc",
		"checks": []any{
			map[string]any{"type": "http", "path": "/health"},
			map[string]any{"type": "http", "path": "/ready"},
		},
	}

	factory := module.NewDeployVerifyStepFactory()
	step, _ := factory("verify", cfg, app)
	result, _ := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if result.Output["check_count"] != 2 {
		t.Errorf("expected check_count=2, got %v", result.Output["check_count"])
	}
	if result.Output["all_passed"] != true {
		t.Errorf("expected all_passed=true, got %v", result.Output["all_passed"])
	}
}

func TestDeployVerify_MissingService(t *testing.T) {
	factory := module.NewDeployVerifyStepFactory()
	_, err := factory("verify", map[string]any{"checks": []any{map[string]any{"type": "http", "path": "/"}}}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestDeployVerify_EmptyChecks(t *testing.T) {
	factory := module.NewDeployVerifyStepFactory()
	_, err := factory("verify", map[string]any{"service": "svc", "checks": []any{}}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for empty checks, got nil")
	}
}

func TestDeployVerify_ServiceNotFound(t *testing.T) {
	factory := module.NewDeployVerifyStepFactory()
	step, _ := factory("verify", map[string]any{
		"service": "ghost",
		"checks":  []any{map[string]any{"type": "http", "path": "/"}},
	}, module.NewMockApplication())
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}
