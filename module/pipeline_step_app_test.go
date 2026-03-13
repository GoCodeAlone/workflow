package module

import (
	"strings"
	"testing"
)

// ── app_deploy ─────────────────────────────────────────────────────────────

func TestAppDeployStep_MissingApp(t *testing.T) {
	_, err := NewAppDeployStepFactory()("deploy", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing app")
	}
	if !strings.Contains(err.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAppDeployStep_ValidConfig(t *testing.T) {
	step, err := NewAppDeployStepFactory()("deploy", map[string]any{
		"app": "my-container",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "deploy" {
		t.Errorf("expected name 'deploy', got %q", step.Name())
	}
}

// ── app_status ─────────────────────────────────────────────────────────────

func TestAppStatusStep_MissingApp(t *testing.T) {
	_, err := NewAppStatusStepFactory()("status", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing app")
	}
	if !strings.Contains(err.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAppStatusStep_ValidConfig(t *testing.T) {
	step, err := NewAppStatusStepFactory()("status", map[string]any{
		"app": "my-container",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "status" {
		t.Errorf("expected name 'status', got %q", step.Name())
	}
}

// ── app_rollback ───────────────────────────────────────────────────────────

func TestAppRollbackStep_MissingApp(t *testing.T) {
	_, err := NewAppRollbackStepFactory()("rollback", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing app")
	}
	if !strings.Contains(err.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAppRollbackStep_ValidConfig(t *testing.T) {
	step, err := NewAppRollbackStepFactory()("rollback", map[string]any{
		"app": "my-container",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "rollback" {
		t.Errorf("expected name 'rollback', got %q", step.Name())
	}
}
