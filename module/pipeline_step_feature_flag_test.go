package module

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/featureflag"
)

// mockProvider is a simple in-memory provider for testing.
type mockFFProvider struct {
	flags map[string]featureflag.FlagValue
}

func newMockFFProvider(flags map[string]featureflag.FlagValue) *mockFFProvider {
	return &mockFFProvider{flags: flags}
}

func (p *mockFFProvider) Name() string { return "mock" }

func (p *mockFFProvider) Evaluate(_ context.Context, key string, _ featureflag.EvaluationContext) (featureflag.FlagValue, error) {
	v, ok := p.flags[key]
	if !ok {
		return featureflag.FlagValue{}, context.DeadlineExceeded
	}
	return v, nil
}

func (p *mockFFProvider) AllFlags(_ context.Context, _ featureflag.EvaluationContext) ([]featureflag.FlagValue, error) {
	vals := make([]featureflag.FlagValue, 0, len(p.flags))
	for _, v := range p.flags {
		vals = append(vals, v)
	}
	return vals, nil
}

func (p *mockFFProvider) Subscribe(_ func(featureflag.FlagChangeEvent)) func() {
	return func() {}
}

func newTestFFService(flags map[string]featureflag.FlagValue) *featureflag.Service {
	provider := newMockFFProvider(flags)
	cache := featureflag.NewFlagCache(0) // no caching for tests
	return featureflag.NewService(provider, cache, slog.Default())
}

func TestFeatureFlagStep_Enabled(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"new-checkout": {
			Key:    "new-checkout",
			Value:  true,
			Type:   featureflag.FlagTypeBoolean,
			Source: "mock",
		},
	})

	factory := NewFeatureFlagStepFactory(service)
	step, err := factory("eval-flag", map[string]any{
		"flag":       "new-checkout",
		"output_key": "checkout_flag",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if step.Name() != "eval-flag" {
		t.Errorf("expected name %q, got %q", "eval-flag", step.Name())
	}

	pc := NewPipelineContext(map[string]any{"user_id": "u123"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	flagResult, ok := result.Output["checkout_flag"].(map[string]any)
	if !ok {
		t.Fatalf("expected checkout_flag in output, got: %v", result.Output)
	}
	if flagResult["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", flagResult["enabled"])
	}
	if flagResult["variant"] != "true" {
		t.Errorf("expected variant='true', got %v", flagResult["variant"])
	}
}

func TestFeatureFlagStep_Disabled(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"dark-mode": {
			Key:    "dark-mode",
			Value:  false,
			Type:   featureflag.FlagTypeBoolean,
			Source: "mock",
		},
	})

	factory := NewFeatureFlagStepFactory(service)
	step, err := factory("check-dark-mode", map[string]any{
		"flag": "dark-mode",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Default output_key should be the flag name
	flagResult, ok := result.Output["dark-mode"].(map[string]any)
	if !ok {
		t.Fatalf("expected dark-mode in output, got: %v", result.Output)
	}
	if flagResult["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", flagResult["enabled"])
	}
}

func TestFeatureFlagStep_StringVariant(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"banner-color": {
			Key:    "banner-color",
			Value:  "blue",
			Type:   featureflag.FlagTypeString,
			Source: "mock",
		},
	})

	factory := NewFeatureFlagStepFactory(service)
	step, err := factory("get-banner", map[string]any{
		"flag":       "banner-color",
		"output_key": "banner",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	flagResult, ok := result.Output["banner"].(map[string]any)
	if !ok {
		t.Fatalf("expected banner in output, got: %v", result.Output)
	}
	if flagResult["variant"] != "blue" {
		t.Errorf("expected variant='blue', got %v", flagResult["variant"])
	}
	if flagResult["enabled"] != true {
		t.Errorf("expected enabled=true for non-empty string, got %v", flagResult["enabled"])
	}
}

func TestFeatureFlagStep_WithUserFrom(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"premium-feature": {
			Key:    "premium-feature",
			Value:  true,
			Type:   featureflag.FlagTypeBoolean,
			Source: "mock",
		},
	})

	factory := NewFeatureFlagStepFactory(service)
	step, err := factory("check-premium", map[string]any{
		"flag":       "premium-feature",
		"user_from":  "{{.user_id}}",
		"output_key": "premium",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"user_id": "user-42"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	flagResult, ok := result.Output["premium"].(map[string]any)
	if !ok {
		t.Fatalf("expected premium in output, got: %v", result.Output)
	}
	if flagResult["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", flagResult["enabled"])
	}
}

func TestFeatureFlagStep_MissingFlag(t *testing.T) {
	factory := NewFeatureFlagStepFactory(nil)
	_, err := factory("bad-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing flag config")
	}
}

func TestFeatureFlagStep_EvalError(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{})

	factory := NewFeatureFlagStepFactory(service)
	step, err := factory("eval-missing", map[string]any{
		"flag": "nonexistent",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = step.Execute(ctx, pc)
	if err == nil {
		t.Fatal("expected error for nonexistent flag")
	}
}

// --- FFGate step tests ---

func TestFFGateStep_Enabled(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"new-ui": {
			Key:    "new-ui",
			Value:  true,
			Type:   featureflag.FlagTypeBoolean,
			Source: "mock",
		},
	})

	factory := NewFFGateStepFactory(service)
	step, err := factory("gate-new-ui", map[string]any{
		"flag":        "new-ui",
		"on_enabled":  "render-new-ui",
		"on_disabled": "render-old-ui",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.NextStep != "render-new-ui" {
		t.Errorf("expected NextStep='render-new-ui', got %q", result.NextStep)
	}
	if result.Output["enabled"] != true {
		t.Errorf("expected enabled=true in output, got %v", result.Output["enabled"])
	}
}

func TestFFGateStep_Disabled(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"experimental": {
			Key:    "experimental",
			Value:  false,
			Type:   featureflag.FlagTypeBoolean,
			Source: "mock",
		},
	})

	factory := NewFFGateStepFactory(service)
	step, err := factory("gate-exp", map[string]any{
		"flag":        "experimental",
		"on_enabled":  "use-experiment",
		"on_disabled": "use-stable",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.NextStep != "use-stable" {
		t.Errorf("expected NextStep='use-stable', got %q", result.NextStep)
	}
	if result.Output["enabled"] != false {
		t.Errorf("expected enabled=false in output, got %v", result.Output["enabled"])
	}
}

func TestFFGateStep_MissingConfig(t *testing.T) {
	factory := NewFFGateStepFactory(nil)

	// Missing flag
	_, err := factory("bad", map[string]any{
		"on_enabled":  "a",
		"on_disabled": "b",
	}, nil)
	if err == nil {
		t.Error("expected error for missing flag")
	}

	// Missing on_enabled
	_, err = factory("bad", map[string]any{
		"flag":        "f",
		"on_disabled": "b",
	}, nil)
	if err == nil {
		t.Error("expected error for missing on_enabled")
	}

	// Missing on_disabled
	_, err = factory("bad", map[string]any{
		"flag":       "f",
		"on_enabled": "a",
	}, nil)
	if err == nil {
		t.Error("expected error for missing on_disabled")
	}
}

func TestFFGateStep_StringFlag(t *testing.T) {
	service := newTestFFService(map[string]featureflag.FlagValue{
		"variant-flag": {
			Key:    "variant-flag",
			Value:  "enabled-variant",
			Type:   featureflag.FlagTypeString,
			Source: "mock",
		},
	})

	factory := NewFFGateStepFactory(service)
	step, err := factory("gate-variant", map[string]any{
		"flag":        "variant-flag",
		"on_enabled":  "path-a",
		"on_disabled": "path-b",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Non-empty, non-"false" string should be considered enabled
	if result.NextStep != "path-a" {
		t.Errorf("expected NextStep='path-a' for non-empty string, got %q", result.NextStep)
	}
}
