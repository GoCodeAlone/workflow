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

func TestIaCProviderApplyStep_ResourcesResolveInfraModules(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		planResult: &interfaces.IaCPlan{
			ID: "plan-resource",
			Actions: []interfaces.PlanAction{
				{Action: "create", Resource: interfaces.ResourceSpec{Name: "staging-ecs", Type: "infra.container_service"}},
			},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}
	infra := module.NewInfraModule("staging-ecs", "infra.container_service", map[string]any{
		"provider": "my-provider",
		"image":    "public.ecr.aws/nginx/nginx:latest",
		"replicas": 2,
	})
	if err := app.RegisterService("staging-ecs.driver", infra); err != nil {
		t.Fatal(err)
	}

	planStep, err := module.NewIaCProviderPlanStepFactory()("plan-step", map[string]any{
		"provider":  "my-provider",
		"resources": []any{"staging-ecs"},
	}, app)
	if err != nil {
		t.Fatalf("plan factory error: %v", err)
	}
	planResult, err := planStep.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("plan Execute error: %v", err)
	}
	correctHash := planResult.Output["desired_hash"].(string)

	applyStep, err := module.NewIaCProviderApplyStepFactory(noopApplyFn)("apply-step", map[string]any{
		"provider":     "my-provider",
		"resources":    []any{"staging-ecs"},
		"desired_hash": correctHash,
	}, app)
	if err != nil {
		t.Fatalf("apply factory error: %v", err)
	}
	result, err := applyStep.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("apply Execute error: %v", err)
	}
	if result.Output["action_count"] != 1 {
		t.Fatalf("expected action_count=1, got %v", result.Output["action_count"])
	}
	if len(provider.planDesired) != 1 {
		t.Fatalf("expected one desired spec, got %d", len(provider.planDesired))
	}
	if got := provider.planDesired[0].Config["image"]; got != "public.ecr.aws/nginx/nginx:latest" {
		t.Fatalf("unexpected desired image: %v", got)
	}
}

// ─── specs_from / desired_hash_from (dynamic input) tests ────────────────────

// parseRequestBodyOutputs builds StepOutputs that mirror the canonical
// production wiring: step.request_parse writes the parsed POST body to its
// Output["body"] (a map[string]any), so specs resolve as
// steps.parse-request.body.specs and the hash as steps.parse-request.body.desired_hash.
func parseRequestBodyOutputs(specs []any, hash any) map[string]map[string]any {
	body := map[string]any{"specs": specs}
	if hash != nil {
		body["desired_hash"] = hash
	}
	return map[string]map[string]any{
		"parse-request": {"body": body},
	}
}

// hashForSpecs computes the canonical desired_hash for the given []any specs by
// driving the plan step with the same data, so both plan and apply hash over
// identical parsed ResourceSpec values.
func hashForSpecs(t *testing.T, specs []any) string {
	t.Helper()
	app := module.NewMockApplication()
	provider := &stubIaCProvider{statusResult: nil, planResult: &interfaces.IaCPlan{ID: "x"}}
	if err := app.RegisterService("hp", provider); err != nil {
		t.Fatal(err)
	}
	step, err := module.NewIaCProviderPlanStepFactory()("h", map[string]any{"provider": "hp", "specs": specs}, app)
	if err != nil {
		t.Fatalf("hash step factory error: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("hash step Execute error: %v", err)
	}
	return result.Output["desired_hash"].(string)
}

// TestIaCProviderApply_DynamicInput verifies that specs_from + desired_hash_from
// pull specs and hash from the pipeline context at Execute time using the
// canonical request_parse wiring, and that the recompute-hash guard still fires
// (matching hash → apply proceeds).
func TestIaCProviderApply_DynamicInput(t *testing.T) {
	app := module.NewMockApplication()
	provider, correctHash := buildApplyProvider(t)
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderApplyStepFactory(noopApplyFn)
	// No static specs or hash — both come from context (request_parse body).
	step, err := factory("apply-step", map[string]any{
		"provider":          "my-provider",
		"specs_from":        "steps.parse-request.body.specs",
		"desired_hash_from": "steps.parse-request.body.desired_hash",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: parseRequestBodyOutputs([]any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		}, correctHash),
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
// desired_hash via the request_parse body still triggers the TOCTOU/drift guard.
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
		"specs_from":        "steps.parse-request.body.specs",
		"desired_hash_from": "steps.parse-request.body.desired_hash",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: parseRequestBodyOutputs([]any{
			map[string]any{"name": "my-db", "type": "infra.database"},
		}, "deadbeef0000000000000000000000000000000000000000000000000000dead"), // wrong hash
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
	// specsWithSecret is the raw []any form delivered by step.request_parse.
	// It includes a secret:// ref that must not be expanded.
	specsWithSecret := []any{
		map[string]any{
			"name": "secret-db",
			"type": "infra.database",
			"config": map[string]any{
				"password": "secret://vault/x",
			},
		},
	}
	correctHash := hashForSpecs(t, specsWithSecret)

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
		"specs_from":        "steps.parse-request.body.specs",
		"desired_hash_from": "steps.parse-request.body.desired_hash",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := &module.PipelineContext{
		StepOutputs: parseRequestBodyOutputs(specsWithSecret, correctHash),
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

// TestIaCProviderApply_DynamicInputFailures asserts the dynamic-input path fails
// cleanly (rather than proceeding with nil/zero specs or panicking on a bad hash
// cast) when the resolved context values are missing, the wrong type, or empty.
func TestIaCProviderApply_DynamicInputFailures(t *testing.T) {
	// validSpecs + its correct hash, reused where a test isolates one failure.
	validSpecs := []any{map[string]any{"name": "my-db", "type": "infra.database"}}
	validHash := hashForSpecs(t, validSpecs)

	cases := []struct {
		name        string
		stepOutputs map[string]map[string]any
		wantErrSub  string
	}{
		{
			name:        "specs path missing from context (request_parse didn't run)",
			stepOutputs: nil,
			wantErrSub:  "resolved to empty/zero specs",
		},
		{
			name: "body lacks specs key",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{"desired_hash": validHash}},
			},
			wantErrSub: "resolved to empty/zero specs",
		},
		{
			name: "specs resolves to a non-list scalar",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{"specs": "not-a-list", "desired_hash": validHash}},
			},
			wantErrSub: "resolve specs_from",
		},
		{
			name: "specs resolves to an empty list",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{"specs": []any{}, "desired_hash": validHash}},
			},
			wantErrSub: "resolved to empty/zero specs",
		},
		{
			name: "desired_hash resolves to a non-string number (guarded cast, no panic)",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{"specs": validSpecs, "desired_hash": 12345}},
			},
			wantErrSub: "did not resolve to a non-empty string",
		},
		{
			name: "desired_hash missing from body",
			stepOutputs: map[string]map[string]any{
				"parse-request": {"body": map[string]any{"specs": validSpecs}},
			},
			wantErrSub: "did not resolve to a non-empty string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
				"specs_from":        "steps.parse-request.body.specs",
				"desired_hash_from": "steps.parse-request.body.desired_hash",
			}, app)
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}

			_, err = step.Execute(context.Background(), &module.PipelineContext{StepOutputs: tc.stepOutputs})
			if err == nil {
				t.Fatal("expected error, got nil (must not proceed with nil/zero specs or bad hash)")
			}
			if !containsString(err.Error(), tc.wantErrSub) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErrSub, err)
			}
			if applied {
				t.Error("applyFn must not be called on a dynamic-input failure")
			}
		})
	}
}
