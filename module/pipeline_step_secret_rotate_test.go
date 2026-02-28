package module

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

// mockRotationProvider is a mock secrets.RotationProvider for testing.
type mockRotationProvider struct {
	rotateVal string
	rotateErr error
	prevVal   string
	prevErr   error
}

func (m *mockRotationProvider) Name() string { return "mock" }
func (m *mockRotationProvider) Get(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockRotationProvider) Set(_ context.Context, _, _ string) error { return nil }
func (m *mockRotationProvider) Delete(_ context.Context, _ string) error { return nil }
func (m *mockRotationProvider) List(_ context.Context) ([]string, error) { return nil, nil }
func (m *mockRotationProvider) Rotate(_ context.Context, _ string) (string, error) {
	return m.rotateVal, m.rotateErr
}
func (m *mockRotationProvider) GetPrevious(_ context.Context, _ string) (string, error) {
	return m.prevVal, m.prevErr
}

// Compile-time check that mockRotationProvider satisfies secrets.RotationProvider.
var _ secrets.RotationProvider = (*mockRotationProvider)(nil)

// ---- factory validation tests ----

func TestSecretRotateStep_MissingProvider(t *testing.T) {
	factory := NewSecretRotateStepFactory()
	_, err := factory("rotate-step", map[string]any{
		"key": "myapp/db-pass",
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' is missing")
	}
}

func TestSecretRotateStep_MissingKey(t *testing.T) {
	factory := NewSecretRotateStepFactory()
	_, err := factory("rotate-step", map[string]any{
		"provider": "vault",
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'key' is missing")
	}
}

func TestSecretRotateStep_ValidConfig(t *testing.T) {
	factory := NewSecretRotateStepFactory()
	step, err := factory("rotate-step", map[string]any{
		"provider":      "vault",
		"key":           "myapp/db-pass",
		"notify_module": "slack-notifier",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "rotate-step" {
		t.Errorf("expected name 'rotate-step', got %q", step.Name())
	}
}

// ---- Execute tests ----

func TestSecretRotateStep_Execute_Success(t *testing.T) {
	mock := &mockRotationProvider{rotateVal: "new-secret-abc123"}
	app := NewMockApplication()
	app.Services["vault"] = mock

	factory := NewSecretRotateStepFactory()
	step, err := factory("rotate-step", map[string]any{
		"provider": "vault",
		"key":      "myapp/db-pass",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Output["rotated"] != true {
		t.Errorf("expected rotated=true, got %v", result.Output["rotated"])
	}
	if result.Output["key"] != "myapp/db-pass" {
		t.Errorf("expected key='myapp/db-pass', got %v", result.Output["key"])
	}
	if result.Output["provider"] != "vault" {
		t.Errorf("expected provider='vault', got %v", result.Output["provider"])
	}
}

func TestSecretRotateStep_Execute_RotateError(t *testing.T) {
	mock := &mockRotationProvider{rotateErr: errors.New("vault unavailable")}
	app := NewMockApplication()
	app.Services["vault"] = mock

	factory := NewSecretRotateStepFactory()
	step, err := factory("rotate-step", map[string]any{
		"provider": "vault",
		"key":      "myapp/db-pass",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from Rotate failure")
	}
}

func TestSecretRotateStep_Execute_ProviderNotFound(t *testing.T) {
	app := NewMockApplication()
	// No services registered.

	factory := NewSecretRotateStepFactory()
	step, err := factory("rotate-step", map[string]any{
		"provider": "vault",
		"key":      "myapp/db-pass",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing provider service")
	}
}

func TestSecretRotateStep_Execute_ProviderWrongType(t *testing.T) {
	app := NewMockApplication()
	// Register something that doesn't implement RotationProvider.
	app.Services["vault"] = "not-a-rotation-provider"

	factory := NewSecretRotateStepFactory()
	step, err := factory("rotate-step", map[string]any{
		"provider": "vault",
		"key":      "myapp/db-pass",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when service does not implement RotationProvider")
	}
}

func TestSecretRotateStep_Execute_NoApp(t *testing.T) {
	factory := NewSecretRotateStepFactory()
	// Pass nil app at factory time is allowed; error comes at Execute time.
	step, err := factory("rotate-step", map[string]any{
		"provider": "vault",
		"key":      "myapp/db-pass",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when app is nil")
	}
}
