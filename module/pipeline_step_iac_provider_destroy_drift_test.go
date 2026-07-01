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

func TestIaCProviderDestroyStep_ResourcesResolveInfraModuleRefs(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		destroyResult: &interfaces.DestroyResult{
			Destroyed: []string{"staging-ecs"},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}
	infra := module.NewInfraModule("staging-ecs", "infra.container_service", map[string]any{"provider": "my-provider"})
	if err := app.RegisterService("staging-ecs.driver", infra); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
		"provider":  "my-provider",
		"resources": []any{"staging-ecs"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if _, err := step.Execute(context.Background(), &module.PipelineContext{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(provider.destroyRefs) != 1 {
		t.Fatalf("expected one destroy ref, got %d", len(provider.destroyRefs))
	}
	if provider.destroyRefs[0].Name != "staging-ecs" || provider.destroyRefs[0].Type != "infra.container_service" {
		t.Fatalf("unexpected destroy refs: %#v", provider.destroyRefs)
	}
}

func TestIaCProviderDestroyStep_RefsFromContext(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		destroyResult: &interfaces.DestroyResult{
			Destroyed: []string{"web-vpc"},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDestroyStepFactory()
	step, err := factory("destroy-step", map[string]any{
		"provider":  "my-provider",
		"refs_from": "steps.parse-request.body.refs",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{StepOutputs: parseRequestRefsOutputs([]any{
		map[string]any{"name": "web-vpc", "type": "infra.vpc"},
	})}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	destroyed, ok := result.Output["destroyed"].([]string)
	if !ok {
		t.Fatalf("expected []string destroyed, got %T", result.Output["destroyed"])
	}
	if len(destroyed) != 1 || destroyed[0] != "web-vpc" {
		t.Fatalf("unexpected destroyed resources: %#v", destroyed)
	}
	if len(provider.destroyRefs) != 1 {
		t.Fatalf("expected one destroy ref, got %d", len(provider.destroyRefs))
	}
	if provider.destroyRefs[0].Name != "web-vpc" || provider.destroyRefs[0].Type != "infra.vpc" {
		t.Fatalf("unexpected destroy refs: %#v", provider.destroyRefs)
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

func TestIaCProviderDestroyStep_Factory_RefsFromMutuallyExclusive(t *testing.T) {
	factory := module.NewIaCProviderDestroyStepFactory()
	for _, cfg := range []map[string]any{
		{"provider": "my-provider", "refs_from": "steps.parse-request.body.refs", "refs": []any{}},
		{"provider": "my-provider", "refs_from": "", "refs": []any{}},
		{"provider": "my-provider", "refs_from": "steps.parse-request.body.refs", "resources": []any{"db"}},
		{"provider": "my-provider", "refs_from": "", "resources": []any{"db"}},
	} {
		_, err := factory("destroy-step", cfg, nil)
		if err == nil {
			t.Fatal("expected mutually exclusive refs_from factory error, got nil")
		}
		if !containsString(err.Error(), "mutually exclusive") {
			t.Errorf("expected mutually exclusive error, got: %v", err)
		}
	}
}

func TestIaCProviderDestroyStep_Factory_RefsFromRequiresNonEmptyString(t *testing.T) {
	factory := module.NewIaCProviderDestroyStepFactory()
	for _, raw := range []any{"", nil, 123} {
		_, err := factory("destroy-step", map[string]any{
			"provider":  "my-provider",
			"refs_from": raw,
		}, nil)
		if err == nil {
			t.Fatalf("expected refs_from factory error for %#v, got nil", raw)
		}
		if !containsString(err.Error(), "refs_from' must be a non-empty string") {
			t.Errorf("expected non-empty string error, got: %v", err)
		}
	}
}

func TestIaCProviderDestroyStep_RefsFromFailures(t *testing.T) {
	cases := []struct {
		name        string
		stepOutputs map[string]map[string]any
		wantErrSub  string
	}{
		{
			name:        "path missing from context",
			stepOutputs: nil,
			wantErrSub:  "resolved to empty/zero refs",
		},
		{
			name: "body present but lacks refs key",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{}},
			},
			wantErrSub: "resolved to empty/zero refs",
		},
		{
			name: "refs resolves to a non-list scalar",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{"refs": "not-a-list"}},
			},
			wantErrSub: "resolve refs_from",
		},
		{
			name:        "refs resolves to an empty list",
			stepOutputs: parseRequestRefsOutputs([]any{}),
			wantErrSub:  "resolved to empty/zero refs",
		},
		{
			name:        "pipeline context is nil",
			stepOutputs: nil,
			wantErrSub:  "requires a non-nil pipeline context",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := module.NewMockApplication()
			provider := &stubIaCProvider{}
			if err := app.RegisterService("my-provider", provider); err != nil {
				t.Fatal(err)
			}
			factory := module.NewIaCProviderDestroyStepFactory()
			step, err := factory("destroy-step", map[string]any{
				"provider":  "my-provider",
				"refs_from": "steps.parse-request.body.refs",
			}, app)
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}

			pc := &module.PipelineContext{StepOutputs: tc.stepOutputs}
			if tc.name == "pipeline context is nil" {
				pc = nil
			}
			_, err = step.Execute(context.Background(), pc)
			if err == nil {
				t.Fatal("expected error, got nil (must not proceed with nil/zero refs)")
			}
			if !containsString(err.Error(), tc.wantErrSub) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErrSub, err)
			}
			if provider.destroyRefs != nil {
				t.Errorf("Destroy must not be called on refs_from failure, got refs %#v", provider.destroyRefs)
			}
		})
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

func TestIaCProviderDriftStep_Execute_Unsupported_DetectDriftUnimplemented(t *testing.T) {
	app := module.NewMockApplication()
	// Provider whose DetectDrift returns the ErrProviderMethodUnimplemented sentinel.
	provider := &stubIaCProvider{
		driftErr: interfaces.ErrProviderMethodUnimplemented,
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
		t.Fatalf("Execute should not error when DetectDrift returns unimplemented: %v", err)
	}
	// ErrProviderMethodUnimplemented sentinel → unsupported, not an error.
	if result.Output["supported"] != false {
		t.Errorf("expected supported=false when DetectDrift returns unimplemented, got %v", result.Output["supported"])
	}
}

func TestIaCProviderDriftStep_Execute_RealError_Propagated(t *testing.T) {
	app := module.NewMockApplication()
	// Provider whose DetectDrift returns a genuine (non-sentinel) error.
	provider := &stubIaCProvider{
		driftErr: errors.New("network timeout reaching provider API"),
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderDriftStepFactory()
	step, err := factory("drift-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), &module.PipelineContext{})
	if err == nil {
		t.Fatal("expected real DetectDrift error to be propagated, got nil")
	}
	if !containsString(err.Error(), "network timeout reaching provider API") {
		t.Errorf("expected original error text in propagated error, got: %v", err)
	}
}

func TestIaCProviderDriftStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderDriftStepFactory()
	_, err := factory("drift-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing")
	}
}
