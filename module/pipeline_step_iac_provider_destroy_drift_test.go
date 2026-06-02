package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// ─── step.iac_provider_destroy tests ─────────────────────────────────────────

func TestIaCProviderDestroyStep_Execute_ReturnsDestroyed(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		destroyResult: &interfaces.DestroyResult{
			Destroyed: []string{"db-1", "vpc-1"},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
		"provider": "my-provider",
		"refs": []any{
			map[string]any{"name": "db-1", "type": "infra.database"},
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	destroyed, ok := result.Output["destroyed"].([]string)
	if !ok {
		t.Fatalf("expected []string destroyed, got %T", result.Output["destroyed"])
	}
	if len(destroyed) != 2 {
		t.Errorf("expected 2 destroyed resources, got %d", len(destroyed))
	}
}

func TestIaCProviderDestroyStep_Execute_UnregisteredProvider(t *testing.T) {
	app := module.NewMockApplication()
	factory := module.NewIaCProviderDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{"provider": "none"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), &module.PipelineContext{})
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
	if !containsString(err.Error(), "not registered") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIaCProviderDestroyStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderDestroyStepFactory()
	_, err := factory("destroy-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing")
	}
}

// ─── step.iac_provider_drift tests ───────────────────────────────────────────

// stubDriftConfigDetector implements interfaces.DriftConfigDetector.
type stubDriftConfigDetector struct {
	drifts []interfaces.DriftResult
	err    error
}

func (d *stubDriftConfigDetector) DetectDriftWithSpecs(_ context.Context, _ []interfaces.ResourceRef, _ map[string]interfaces.ResourceSpec) ([]interfaces.DriftResult, error) {
	return d.drifts, d.err
}

var _ interfaces.DriftConfigDetector = (*stubDriftConfigDetector)(nil)

// stubProviderWithDriftDetector extends stubIaCProvider with DriftDetector capability.
type stubProviderWithDriftDetector struct {
	stubIaCProvider
	detector interfaces.DriftConfigDetector // nil → DriftDetector() returns nil
}

// DriftDetector satisfies providerclient.DriftDetectorProvider.
func (p *stubProviderWithDriftDetector) DriftDetector() interfaces.DriftConfigDetector {
	return p.detector
}

func TestIaCProviderDriftStep_Execute_WithDriftDetector(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubProviderWithDriftDetector{
		detector: &stubDriftConfigDetector{
			drifts: []interfaces.DriftResult{
				{Name: "db", Type: "infra.database", Drifted: true, Class: interfaces.DriftClassConfig, Fields: []string{"size"}},
				{Name: "vpc", Type: "infra.vpc", Drifted: false, Class: interfaces.DriftClassInSync},
			},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDriftStepFactory()
	step, err := factory("drift-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Output["supported"] != true {
		t.Errorf("expected supported=true, got %v", result.Output["supported"])
	}
	if result.Output["any_drifted"] != true {
		t.Errorf("expected any_drifted=true, got %v", result.Output["any_drifted"])
	}
	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

func TestIaCProviderDriftStep_Execute_NilDriftDetector_FallsBackToDetectDrift(t *testing.T) {
	app := module.NewMockApplication()
	// Provider implements DriftDetectorProvider but returns nil detector.
	provider := &stubProviderWithDriftDetector{detector: nil}
	// Since DetectDrift on stubIaCProvider returns (nil, nil) — treat as "supported but no drifts".
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDriftStepFactory()
	step, err := factory("drift-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// Falls through to DetectDrift; stubIaCProvider.driftResult is nil so drifts is empty.
	if result.Output["supported"] != true {
		t.Errorf("expected supported=true (existence-only path), got %v", result.Output["supported"])
	}
}

func TestIaCProviderDriftStep_Execute_Unsupported_NoInterface(t *testing.T) {
	app := module.NewMockApplication()
	// stubIaCProvider.DetectDrift returns (nil, nil) — empty drifts, supported.
	provider := &stubIaCProvider{}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDriftStepFactory()
	step, err := factory("drift-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Output["supported"] != true {
		t.Errorf("expected supported=true (DetectDrift returned nil error), got %v", result.Output["supported"])
	}
}

func TestIaCProviderDriftStep_Execute_Unsupported_DetectDriftError(t *testing.T) {
	app := module.NewMockApplication()
	// Provider whose DetectDrift returns ErrProviderMethodUnimplemented.
	provider := &stubIaCProvider{
		driftErr: errors.New("workflow: provider method not implemented"),
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDriftStepFactory()
	step, err := factory("drift-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute should not error: %v", err)
	}
	// Error from required DetectDrift surface → unsupported.
	if result.Output["supported"] != false {
		t.Errorf("expected supported=false when DetectDrift errors, got %v", result.Output["supported"])
	}
}

func TestIaCProviderDriftStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderDriftStepFactory()
	_, err := factory("drift-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing")
	}
}
