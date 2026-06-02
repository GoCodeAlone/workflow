package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// ─── apply stub helpers ───────────────────────────────────────────────────────

// noopApplyFn is an apply function stub that returns an empty ApplyResult
// (simulates a successful zero-action apply).
func noopApplyFn(_ context.Context, _ interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	result := &interfaces.ApplyResult{PlanID: plan.ID}
	// Emit one ActionOutcome per action so the engine invariant (len Actions == len plan.Actions) holds.
	for range plan.Actions {
		result.Actions = append(result.Actions, interfaces.ActionOutcome{Status: interfaces.ActionStatusSuccess})
	}
	return result, nil
}

// errApplyFn always returns a provider error.
func errApplyFn(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, errors.New("provider internal error: disk full")
}

// buildApplyProvider returns a stub provider with a known status and a plan result
// that matches the given specs so the hash-recompute path exercises the equality branch.
func buildApplyProvider(t *testing.T) (*stubIaCProvider, string) {
	t.Helper()
	specs := []interfaces.ResourceSpec{
		{Name: "my-db", Type: "infra.database"},
	}
	// No current state — hash is just over the desired specs.
	hash := computeDesiredStateHashTestHelper(specs, nil)
	provider := &stubIaCProvider{
		statusResult: nil, // no existing resources
		planResult: &interfaces.IaCPlan{
			ID: "plan-999",
			Actions: []interfaces.PlanAction{
				{Action: "create", Resource: interfaces.ResourceSpec{Name: "my-db", Type: "infra.database"}},
			},
		},
	}
	return provider, hash
}

// computeDesiredStateHashTestHelper calls the step's Execute to get the hash
// indirectly, or we replicate the logic using the plan step.
// Since we can't import the private function, we use a plan step to get the hash.
func computeDesiredStateHashTestHelper(specs []interfaces.ResourceSpec, current []interfaces.ResourceState) string {
	_ = current // only specs matter for the test setup
	// We reproduce the hash inline using the same algorithm as the step.
	// The test just needs a matching hash string.
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		statusResult: nil,
		planResult:   &interfaces.IaCPlan{ID: "x"},
	}
	if err := app.RegisterService("hp", provider); err != nil {
		panic(err)
	}
	specsAny := make([]any, len(specs))
	for i, s := range specs {
		specsAny[i] = map[string]any{"name": s.Name, "type": s.Type}
	}
	planFactory := module.NewIaCProviderPlanStepFactory()
	step, err := planFactory("h", map[string]any{"provider": "hp", "specs": specsAny}, app)
	if err != nil {
		panic(err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		panic(err)
	}
	return result.Output["desired_hash"].(string)
}

// ─── step.iac_provider_apply tests ───────────────────────────────────────────

func TestIaCProviderApplyStep_Execute_Matches_Applies(t *testing.T) {
	app := module.NewMockApplication()
	provider, correctHash := buildApplyProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderApplyStepFactory(noopApplyFn)
	step, err := factory("apply-step", map[string]any{
		"provider":     "my-provider",
		"desired_hash": correctHash,
		"specs": []any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Output["apply_result"] == nil {
		t.Error("expected apply_result in output")
	}
	if result.Output["desired_hash"] != correctHash {
		t.Errorf("desired_hash mismatch: got %v", result.Output["desired_hash"])
	}
}

func TestIaCProviderApplyStep_Execute_Mismatch_Rejected(t *testing.T) {
	app := module.NewMockApplication()
	provider, _ := buildApplyProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	applied := false
	trackingApplyFn := func(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		applied = true
		return noopApplyFn(ctx, p, plan)
	}

	factory := module.NewIaCProviderApplyStepFactory(trackingApplyFn)
	step, err := factory("apply-step", map[string]any{
		"provider":     "my-provider",
		"desired_hash": "deadbeef0000000000000000000000000000000000000000000000000000dead", // wrong hash
		"specs": []any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), &module.PipelineContext{})
	if err == nil {
		t.Fatal("expected error for hash mismatch, got nil")
	}
	if !containsString(err.Error(), "plan hash mismatch") {
		t.Errorf("expected 'plan hash mismatch' error, got: %v", err)
	}
	if applied {
		t.Error("applyFn must not be called when hash mismatches")
	}
}

func TestIaCProviderApplyStep_Execute_ProviderError_Surfaced(t *testing.T) {
	app := module.NewMockApplication()
	provider, correctHash := buildApplyProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderApplyStepFactory(errApplyFn)
	step, err := factory("apply-step", map[string]any{
		"provider":     "my-provider",
		"desired_hash": correctHash,
		"specs": []any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), &module.PipelineContext{})
	if err == nil {
		t.Fatal("expected provider error to be surfaced")
	}
	// Must surface the underlying provider error, not mask it as "denied".
	if !containsString(err.Error(), "provider internal error") {
		t.Errorf("expected provider error text, got: %v", err)
	}
}

func TestIaCProviderApplyStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderApplyStepFactory(noopApplyFn)
	_, err := factory("apply-step", map[string]any{"desired_hash": "abc"}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing")
	}
}

func TestIaCProviderApplyStep_Factory_RequiresHash(t *testing.T) {
	factory := module.NewIaCProviderApplyStepFactory(noopApplyFn)
	_, err := factory("apply-step", map[string]any{"provider": "x"}, nil)
	if err == nil {
		t.Fatal("expected error when 'desired_hash' missing")
	}
}

// ─── specs_from / desired_hash_from (dynamic input) tests ────────────────────

// TestIaCProviderApply_DynamicInput verifies that specs_from + desired_hash_from
// pull specs and hash from the pipeline context at Execute time, and that the
// recompute-hash guard still fires (matching hash → apply proceeds).
func TestIaCProviderApply_DynamicInput(t *testing.T) {
	app := module.NewMockApplication()
	provider, correctHash := buildApplyProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderApplyStepFactory(noopApplyFn)
	// No static specs or hash — both come from context.
	step, err := factory("apply-step", map[string]any{
		"provider":          "my-provider",
		"specs_from":        "steps.plan-step.specs",
		"desired_hash_from": "steps.plan-step.desired_hash",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: map[string]map[string]any{
			"plan-step": {
				"desired_hash": correctHash,
				"specs": []any{
					map[string]any{"name": "my-db", "type": "infra.database"},
				},
			},
		},
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Output["apply_result"] == nil {
		t.Error("expected apply_result in output")
	}
	if result.Output["desired_hash"] != correctHash {
		t.Errorf("desired_hash mismatch: got %v", result.Output["desired_hash"])
	}
}

// TestIaCProviderApply_HashMismatchRejected verifies that supplying a wrong
// desired_hash via context still triggers the TOCTOU/drift guard.
func TestIaCProviderApply_HashMismatchRejected(t *testing.T) {
	app := module.NewMockApplication()
	provider, _ := buildApplyProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	applied := false
	trackingApplyFn := func(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		applied = true
		return noopApplyFn(ctx, p, plan)
	}

	factory := module.NewIaCProviderApplyStepFactory(trackingApplyFn)
	step, err := factory("apply-step", map[string]any{
		"provider":          "my-provider",
		"specs_from":        "steps.plan-step.specs",
		"desired_hash_from": "steps.plan-step.desired_hash",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: map[string]map[string]any{
			"plan-step": {
				// Wrong hash — should trigger mismatch error.
				"desired_hash": "deadbeef0000000000000000000000000000000000000000000000000000dead",
				"specs": []any{
					map[string]any{"name": "my-db", "type": "infra.database"},
				},
			},
		},
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for hash mismatch, got nil")
	}
	if !containsString(err.Error(), "plan hash mismatch") {
		t.Errorf("expected 'plan hash mismatch' error, got: %v", err)
	}
	if applied {
		t.Error("applyFn must not be called when hash mismatches")
	}
}

// TestIaCProviderApply_SecretRefSurvives verifies that specs containing
// secret:// refs survive unchanged through the dynamic-input path — no resolver
// is invoked, and the literal ref is what reaches the applyFn.
func TestIaCProviderApply_SecretRefSurvives(t *testing.T) {
	// specsWithSecret is the raw []any form that would come from the pipeline context
	// (e.g. via step.request_parse output). It includes a secret:// ref that must
	// not be expanded.
	specsWithSecret := []any{
		map[string]any{
			"name": "secret-db",
			"type": "infra.database",
			"config": map[string]any{
				"password": "secret://vault/x",
			},
		},
	}

	// Compute the correct hash by driving the plan step with the same []any data,
	// so both plan and apply hash over identical parsed ResourceSpec values.
	hashApp := module.NewMockApplication()
	hashProvider := &stubIaCProvider{
		statusResult: nil,
		planResult:   &interfaces.IaCPlan{ID: "x"},
	}
	if err := hashApp.RegisterService("hp", hashProvider); err != nil {
		t.Fatal(err)
	}
	planFactory := module.NewIaCProviderPlanStepFactory()
	hashStep, err := planFactory("h", map[string]any{"provider": "hp", "specs": specsWithSecret}, hashApp)
	if err != nil {
		t.Fatalf("hash step factory error: %v", err)
	}
	hashResult, err := hashStep.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("hash step Execute error: %v", err)
	}
	correctHash := hashResult.Output["desired_hash"].(string)

	// Build the actual provider under test (no existing resources).
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		statusResult: nil,
		planResult: &interfaces.IaCPlan{
			ID: "plan-secret",
			Actions: []interfaces.PlanAction{
				{Action: "create", Resource: interfaces.ResourceSpec{Name: "secret-db", Type: "infra.database"}},
			},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	// Capture what plan reaches the applyFn.
	var capturedPlan *interfaces.IaCPlan
	capturingApplyFn := func(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		capturedPlan = plan
		return noopApplyFn(ctx, p, plan)
	}

	factory := module.NewIaCProviderApplyStepFactory(capturingApplyFn)
	step, err := factory("apply-step", map[string]any{
		"provider":          "my-provider",
		"specs_from":        "steps.plan-step.specs",
		"desired_hash_from": "steps.plan-step.desired_hash",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: map[string]map[string]any{
			"plan-step": {
				"desired_hash": correctHash,
				"specs":        specsWithSecret,
			},
		},
	}

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// The captured plan must not be nil — the applyFn was called.
	if capturedPlan == nil {
		t.Fatal("expected applyFn to be called with a plan")
	}
	// The plan's DesiredHash must match the hash we computed from the literal secret ref.
	if capturedPlan.DesiredHash != correctHash {
		t.Errorf("plan DesiredHash = %q, want %q", capturedPlan.DesiredHash, correctHash)
	}
}
