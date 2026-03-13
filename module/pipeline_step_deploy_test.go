package module

import (
	"strings"
	"testing"
)

func TestDeployStep_MissingEnvironment(t *testing.T) {
	_, err := NewDeployStepFactory()("deploy", map[string]any{
		"strategy": "rolling",
		"image":    "myapp:v1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing environment")
	}
	if !strings.Contains(err.Error(), "'environment' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeployStep_MissingStrategy(t *testing.T) {
	_, err := NewDeployStepFactory()("deploy", map[string]any{
		"environment": "production",
		"image":       "myapp:v1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing strategy")
	}
	if !strings.Contains(err.Error(), "'strategy' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeployStep_InvalidStrategy(t *testing.T) {
	_, err := NewDeployStepFactory()("deploy", map[string]any{
		"environment": "production",
		"strategy":    "teleport",
		"image":       "myapp:v1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid strategy")
	}
	if !strings.Contains(err.Error(), "invalid strategy") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeployStep_MissingImage(t *testing.T) {
	_, err := NewDeployStepFactory()("deploy", map[string]any{
		"environment": "staging",
		"strategy":    "rolling",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "'image' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeployStep_ValidConfig(t *testing.T) {
	step, err := NewDeployStepFactory()("deploy", map[string]any{
		"environment": "staging",
		"strategy":    "rolling",
		"image":       "myapp:v2",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "deploy" {
		t.Errorf("expected name 'deploy', got %q", step.Name())
	}
}

func TestDeployStep_AllStrategies(t *testing.T) {
	for _, strategy := range []string{"rolling", "blue_green", "canary"} {
		_, err := NewDeployStepFactory()("deploy", map[string]any{
			"environment": "prod",
			"strategy":    strategy,
			"image":       "app:v1",
		}, nil)
		if err != nil {
			t.Errorf("strategy %q: unexpected error: %v", strategy, err)
		}
	}
}
