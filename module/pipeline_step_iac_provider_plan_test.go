package module_test

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// ─── step.iac_provider_plan tests ────────────────────────────────────────────

func makePlanProvider(t *testing.T) *stubIaCProvider {
	t.Helper()
	return &stubIaCProvider{
		statusResult: []interfaces.ResourceStatus{
			{Name: "existing-db", Type: "infra.database", ProviderID: "pid-99", Status: "running"},
		},
		planResult: &interfaces.IaCPlan{
			ID:        "plan-001",
			CreatedAt: time.Now(),
			Actions: []interfaces.PlanAction{
				{
					Action: "create",
					Resource: interfaces.ResourceSpec{
						Name: "my-db",
						Type: "infra.database",
					},
				},
			},
		},
	}
}

func TestIaCProviderPlanStep_Execute_ReturnsPlanAndHash(t *testing.T) {
	app := module.NewMockApplication()
	provider := makePlanProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-provider",
		"specs": []any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		},
	}
	factory := module.NewIaCProviderPlanStepFactory()
	step, err := factory("plan-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	hash, ok := result.Output["desired_hash"].(string)
	if !ok || hash == "" {
		t.Errorf("expected non-empty desired_hash, got %v", result.Output["desired_hash"])
	}

	// Plan should be present and JSON-able.
	if result.Output["plan"] == nil {
		t.Error("expected plan in output, got nil")
	}
}

func TestIaCProviderPlanStep_HashStableWhenEnvVarChanges(t *testing.T) {
	// The NO-OP env resolver in DesiredStateHash must preserve ${ENV_VAR}
	// placeholders verbatim so the hash is identical regardless of env value.
	app := module.NewMockApplication()
	provider := makePlanProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"provider": "my-provider",
		"specs": []any{
			map[string]any{
				"name": "env-db",
				"type": "infra.database",
				"config": map[string]any{
					"password": "${DB_PASSWORD}",
				},
			},
		},
	}

	factory := module.NewIaCProviderPlanStepFactory()
	step, err := factory("plan-step", cfg, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// First hash: env var set to "secret1".
	t.Setenv("DB_PASSWORD", "secret1")
	result1, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute (run 1) error: %v", err)
	}
	hash1 := result1.Output["desired_hash"].(string)

	// Second hash: env var changed to "secret2".
	t.Setenv("DB_PASSWORD", "secret2")
	result2, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute (run 2) error: %v", err)
	}
	hash2 := result2.Output["desired_hash"].(string)

	if hash1 != hash2 {
		t.Errorf("expected hash to be stable when env var value changes:\n  hash1=%s\n  hash2=%s", hash1, hash2)
	}
}

func TestIaCProviderPlanStep_UnregisteredProvider(t *testing.T) {
	app := module.NewMockApplication()
	factory := module.NewIaCProviderPlanStepFactory()
	step, err := factory("plan-step", map[string]any{"provider": "none"}, app)
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

func TestIaCProviderPlanStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderPlanStepFactory()
	_, err := factory("plan-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing")
	}
}

// ─── specs_from tests ─────────────────────────────────────────────────────────

// TestIaCProviderPlan_SpecsFromContext verifies that when specs_from is set, the
// step resolves specs from the pipeline context (StepOutputs) at Execute time and
// returns a valid desired_hash in its output.
func TestIaCProviderPlan_SpecsFromContext(t *testing.T) {
	app := module.NewMockApplication()
	provider := makePlanProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderPlanStepFactory()
	// No static specs — specs_from points into StepOutputs.
	step, err := factory("plan-step", map[string]any{
		"provider":   "my-provider",
		"specs_from": "steps.parse-request.body.specs",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Build a PipelineContext whose StepOutputs["parse-request"]["body"]["specs"]
	// contains one resource spec with a secret:// ref in its config.
	pc := &module.PipelineContext{
		StepOutputs: map[string]map[string]any{
			"parse-request": {
				"body": map[string]any{
					"specs": []any{
						map[string]any{
							"name": "vault-db",
							"type": "infra.database",
							"config": map[string]any{
								"password": "secret://vault/x",
							},
						},
					},
				},
			},
		},
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	hash, ok := result.Output["desired_hash"].(string)
	if !ok || hash == "" {
		t.Errorf("expected non-empty desired_hash, got %v", result.Output["desired_hash"])
	}
	if result.Output["plan"] == nil {
		t.Error("expected plan in output, got nil")
	}
}

// TestIaCProviderPlan_SpecsFromAndStatic_FactoryError verifies that setting both
// specs and specs_from is rejected at factory time (mutually exclusive).
func TestIaCProviderPlan_SpecsFromAndStatic_FactoryError(t *testing.T) {
	factory := module.NewIaCProviderPlanStepFactory()
	_, err := factory("plan-step", map[string]any{
		"provider":   "my-provider",
		"specs_from": "steps.parse-request.body.specs",
		"specs": []any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected factory error when both specs and specs_from are set")
	}
	if !containsString(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error, got: %v", err)
	}
}
