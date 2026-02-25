package module

import (
	"context"
	"testing"
)

// ─── mock backend unit tests ─────────────────────────────────────────────────

func TestMockPolicyEngine_DefaultAllow(t *testing.T) {
	eng := newMockPolicyEngine()
	decision, err := eng.Evaluate(context.Background(), map[string]any{"user": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected default allow, got deny: %v", decision.Reasons)
	}
}

func TestMockPolicyEngine_DenyAction(t *testing.T) {
	eng := newMockPolicyEngine()
	decision, err := eng.Evaluate(context.Background(), map[string]any{"action": "deny"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allowed {
		t.Error("expected deny when action=deny")
	}
	if len(decision.Reasons) == 0 {
		t.Error("expected non-empty reasons")
	}
}

func TestMockPolicyEngine_LoadDenyPolicy(t *testing.T) {
	eng := newMockPolicyEngine()
	if err := eng.LoadPolicy("block-all", "deny all requests"); err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}

	decision, err := eng.Evaluate(context.Background(), map[string]any{"user": "bob"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Allowed {
		t.Error("expected deny after loading deny policy")
	}
}

func TestMockPolicyEngine_LoadAllowPolicy(t *testing.T) {
	eng := newMockPolicyEngine()
	if err := eng.LoadPolicy("allow-all", "permit all requests"); err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}

	decision, err := eng.Evaluate(context.Background(), map[string]any{"user": "carol"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected allow with allow-only policy, got: %v", decision.Reasons)
	}
}

func TestMockPolicyEngine_ListPolicies(t *testing.T) {
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("p1", "permit everything")
	_ = eng.LoadPolicy("p2", "another rule")

	policies := eng.ListPolicies()
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}
	for _, p := range policies {
		if p.Backend != "mock" {
			t.Errorf("expected backend=mock, got %q", p.Backend)
		}
	}
}

// ─── PolicyEngineModule with config pre-load ─────────────────────────────────

func TestPolicyEngineModule_UnsupportedBackend(t *testing.T) {
	m := NewPolicyEngineModule("bad", map[string]any{"backend": "unknown"})
	err := m.Init(NewMockApplication())
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
}

func TestPolicyEngineModule_MockInit(t *testing.T) {
	app := NewMockApplication()
	m := NewPolicyEngineModule("policy-engine", map[string]any{
		"backend": "mock",
		"policies": []any{
			map[string]any{"name": "authz", "content": "permit admin"},
		},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.Backend() != "mock" {
		t.Errorf("expected backend=mock, got %q", m.Backend())
	}
	// Verify service was registered.
	if _, ok := app.Services["policy-engine"]; !ok {
		t.Error("expected policy-engine registered in service registry")
	}
	// Verify pre-loaded policy.
	policies := m.Engine().ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("expected 1 pre-loaded policy, got %d", len(policies))
	}
}

// ─── step.policy_evaluate tests ──────────────────────────────────────────────

func TestPolicyEvaluateStep_Allow(t *testing.T) {
	app := newMockAppWithPolicyEngine("my-engine", newMockPolicyEngine())

	factory := NewPolicyEvaluateStepFactory()
	step, err := factory("eval", map[string]any{
		"engine": "my-engine",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"user": "alice"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["allowed"] != true {
		t.Error("expected allowed=true")
	}
	if result.Stop {
		t.Error("expected Stop=false for allow decision")
	}
}

func TestPolicyEvaluateStep_Deny(t *testing.T) {
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("block", "deny all")
	app := newMockAppWithPolicyEngine("my-engine", eng)

	factory := NewPolicyEvaluateStepFactory()
	step, err := factory("eval", map[string]any{
		"engine": "my-engine",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"user": "bob"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["allowed"] != false {
		t.Error("expected allowed=false")
	}
	if !result.Stop {
		t.Error("expected Stop=true for deny decision")
	}
}

func TestPolicyEvaluateStep_InputFrom(t *testing.T) {
	app := newMockAppWithPolicyEngine("my-engine", newMockPolicyEngine())

	factory := NewPolicyEvaluateStepFactory()
	step, err := factory("eval", map[string]any{
		"engine":     "my-engine",
		"input_from": "request",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"request": map[string]any{"action": "read"},
	}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["allowed"] != true {
		t.Error("expected allow for read action")
	}
}

func TestPolicyEvaluateStep_MissingEngine(t *testing.T) {
	factory := NewPolicyEvaluateStepFactory()
	_, err := factory("eval", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when engine not specified")
	}
}

// ─── step.policy_load tests ──────────────────────────────────────────────────

func TestPolicyLoadStep_LoadsPolicy(t *testing.T) {
	eng := newMockPolicyEngine()
	app := newMockAppWithPolicyEngine("my-engine", eng)

	factory := NewPolicyLoadStepFactory()
	step, err := factory("load", map[string]any{
		"engine":      "my-engine",
		"policy_name": "authz",
		"content":     "permit requests from admin",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["loaded"] != true {
		t.Error("expected loaded=true")
	}
	if result.Output["policy_name"] != "authz" {
		t.Errorf("expected policy_name=authz, got %v", result.Output["policy_name"])
	}

	// Verify policy is queryable.
	policies := eng.ListPolicies()
	if len(policies) != 1 || policies[0].Name != "authz" {
		t.Errorf("expected policy 'authz' in engine, got %v", policies)
	}
}

func TestPolicyLoadStep_ContentFromContext(t *testing.T) {
	eng := newMockPolicyEngine()
	app := newMockAppWithPolicyEngine("my-engine", eng)

	factory := NewPolicyLoadStepFactory()
	step, err := factory("load", map[string]any{
		"engine":      "my-engine",
		"policy_name": "dynamic",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"policy_content": "permit all reads",
	}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	policies := eng.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
}

func TestPolicyLoadStep_MissingEngine(t *testing.T) {
	factory := NewPolicyLoadStepFactory()
	_, err := factory("load", map[string]any{"policy_name": "p"}, nil)
	if err == nil {
		t.Fatal("expected error when engine not specified")
	}
}

func TestPolicyLoadStep_MissingPolicyName(t *testing.T) {
	factory := NewPolicyLoadStepFactory()
	_, err := factory("load", map[string]any{"engine": "e"}, nil)
	if err == nil {
		t.Fatal("expected error when policy_name not specified")
	}
}

// ─── step.policy_list tests ──────────────────────────────────────────────────

func TestPolicyListStep_ListsPolicies(t *testing.T) {
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("p1", "rule1")
	_ = eng.LoadPolicy("p2", "rule2")
	app := newMockAppWithPolicyEngine("my-engine", eng)

	factory := NewPolicyListStepFactory()
	step, err := factory("list", map[string]any{"engine": "my-engine"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

func TestPolicyListStep_MissingEngine(t *testing.T) {
	factory := NewPolicyListStepFactory()
	_, err := factory("list", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when engine not specified")
	}
}

// ─── step.policy_test tests ──────────────────────────────────────────────────

func TestPolicyTestStep_PassWhenAllow(t *testing.T) {
	app := newMockAppWithPolicyEngine("my-engine", newMockPolicyEngine())

	factory := NewPolicyTestStepFactory()
	step, err := factory("policy-test", map[string]any{
		"engine":       "my-engine",
		"sample_input": map[string]any{"user": "admin"},
		"expect_allow": true,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["test_passed"] != true {
		t.Error("expected test_passed=true")
	}
	if result.Stop {
		t.Error("expected Stop=false when test passes")
	}
}

func TestPolicyTestStep_FailWhenUnexpectedAllow(t *testing.T) {
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("deny-all", "deny everything")
	app := newMockAppWithPolicyEngine("my-engine", eng)

	factory := NewPolicyTestStepFactory()
	step, err := factory("policy-test", map[string]any{
		"engine":       "my-engine",
		"sample_input": map[string]any{"user": "test"},
		"expect_allow": true, // expect allow but policy denies
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["test_passed"] != false {
		t.Error("expected test_passed=false when expectation not met")
	}
	if !result.Stop {
		t.Error("expected Stop=true when test fails")
	}
}

func TestPolicyTestStep_ExpectDeny(t *testing.T) {
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("deny-all", "deny everything")
	app := newMockAppWithPolicyEngine("my-engine", eng)

	factory := NewPolicyTestStepFactory()
	step, err := factory("policy-test", map[string]any{
		"engine":       "my-engine",
		"sample_input": map[string]any{"user": "test"},
		"expect_allow": false, // expect deny — policy does deny
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["test_passed"] != true {
		t.Error("expected test_passed=true when expect_allow=false and policy denies")
	}
}

// ─── integration: policy_evaluate after auth step ────────────────────────────

func TestPolicyEvaluateStep_IntegrationWithAuthPipeline(t *testing.T) {
	eng := newMockPolicyEngine()
	// Policy only has allow keyword — no "deny" keyword — so engine allows.
	_ = eng.LoadPolicy("admin-access", "allow principal.role == admin")
	app := newMockAppWithPolicyEngine("authz", eng)

	factory := NewPolicyEvaluateStepFactory()
	step, err := factory("authz-check", map[string]any{
		"engine":     "authz",
		"input_from": "auth_context",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	// Simulate prior auth step populating context.
	pc := NewPipelineContext(map[string]any{
		"auth_context": map[string]any{
			"principal": map[string]any{"role": "admin"},
			"action":    "read",
			"resource":  "data.sensitive",
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["allowed"] != true {
		t.Errorf("expected admin to be allowed, reasons: %v", result.Output["reasons"])
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// newMockAppWithPolicyEngine creates a MockApplication with a PolicyEngineModule registered.
func newMockAppWithPolicyEngine(name string, eng *mockPolicyEngine) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = &PolicyEngineModule{
		name:    name,
		backend: "mock",
		engine:  eng,
	}
	return app
}
