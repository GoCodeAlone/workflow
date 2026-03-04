package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestPolicyApp creates a MockApplication with a PolicyEngineModule registered.
func newTestPolicyApp(engineName string, eng PolicyEngine) *MockApplication {
	app := NewMockApplication()
	mod := &PolicyEngineModule{
		name:   engineName,
		engine: eng,
	}
	app.Services[engineName] = mod
	return app
}

func TestAuthzCheckStep_Allowed(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	app := newTestPolicyApp("policy", newMockPolicyEngine())

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"subject": "user-1"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stop {
		t.Error("expected Stop=false when policy allows")
	}
	if result.Output["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", result.Output["allowed"])
	}
}

func TestAuthzCheckStep_Denied(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	eng := newMockPolicyEngine()
	// Load a deny policy so the mock engine denies the request.
	_ = eng.LoadPolicy("deny-all", "deny")
	app := newTestPolicyApp("policy", eng)

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"subject": "user-1"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when policy denies")
	}
	if result.Output["response_status"] != http.StatusForbidden {
		t.Errorf("expected response_status=403, got %v", result.Output["response_status"])
	}
	errMsg, _ := result.Output["error"].(string)
	if !strings.Contains(errMsg, "forbidden") {
		t.Errorf("expected error to contain 'forbidden', got %q", errMsg)
	}
}

func TestAuthzCheckStep_WritesHTTPResponse(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("deny-all", "deny")
	app := newTestPolicyApp("policy", eng)

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	w := httptest.NewRecorder()
	pc := NewPipelineContext(map[string]any{"subject": "user-1"}, map[string]any{
		"_http_response_writer": w,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected HTTP 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("expected 'forbidden' in response body, got %q", w.Body.String())
	}
	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true in metadata")
	}
}

func TestAuthzCheckStep_WritesHTTPResponse_NoResponseWriter(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("deny-all", "deny")
	app := newTestPolicyApp("policy", eng)

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No response writer in metadata — should still stop pipeline with output.
	pc := NewPipelineContext(map[string]any{"subject": "user-1"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true even without response writer")
	}
	if result.Output["response_status"] != http.StatusForbidden {
		t.Errorf("expected response_status=403, got %v", result.Output["response_status"])
	}
	headers, _ := result.Output["response_headers"].(map[string]string)
	if headers["Content-Type"] != "application/json" {
		t.Errorf("expected response_headers Content-Type=application/json, got %v", headers)
	}
}

func TestAuthzCheckStep_InputFrom(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	eng := newMockPolicyEngine()
	_ = eng.LoadPolicy("deny-all", "deny")
	app := newTestPolicyApp("policy", eng)

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
		"input_from":    "authz_input",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// The input_from field contains a sub-map; the deny policy still triggers.
	pc := NewPipelineContext(map[string]any{
		"authz_input": map[string]any{"subject": "user-1", "action": "read"},
	}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
}

func TestAuthzCheckStep_FactoryRequiresPolicyEngine(t *testing.T) {
	factory := NewAuthzCheckStepFactory()

	_, err := factory("authz", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing policy_engine")
	}
	if !strings.Contains(err.Error(), "'policy_engine' is required") {
		t.Errorf("expected policy_engine error, got: %v", err)
	}
}

func TestAuthzCheckStep_NilApp(t *testing.T) {
	factory := NewAuthzCheckStepFactory()

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when app is nil")
	}
	if !strings.Contains(err.Error(), "no application context") {
		t.Errorf("expected 'no application context' error, got: %v", err)
	}
}

func TestAuthzCheckStep_Name(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	app := newTestPolicyApp("policy", newMockPolicyEngine())

	step, err := factory("my-authz-step", map[string]any{
		"policy_engine": "policy",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "my-authz-step" {
		t.Errorf("expected name 'my-authz-step', got %q", step.Name())
	}
}

func TestAuthzCheckStep_DefaultSubjectField(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	app := newTestPolicyApp("policy", newMockPolicyEngine())

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	s := step.(*AuthzCheckStep)
	if s.subjectField != "subject" {
		t.Errorf("expected default subject_field='subject', got %q", s.subjectField)
	}
}

func TestAuthzCheckStep_CustomSubjectField(t *testing.T) {
	factory := NewAuthzCheckStepFactory()
	app := newTestPolicyApp("policy", newMockPolicyEngine())

	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
		"subject_field": "user_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	s := step.(*AuthzCheckStep)
	if s.subjectField != "user_id" {
		t.Errorf("expected subject_field='user_id', got %q", s.subjectField)
	}
}

// capturingPolicyEngine records the last input passed to Evaluate.
type capturingPolicyEngine struct {
	lastInput map[string]any
}

func (e *capturingPolicyEngine) Evaluate(_ context.Context, input map[string]any) (*PolicyDecision, error) {
	e.lastInput = input
	return &PolicyDecision{Allowed: true, Reasons: []string{"allow"}, Metadata: nil}, nil
}
func (e *capturingPolicyEngine) LoadPolicy(_, _ string) error { return nil }
func (e *capturingPolicyEngine) ListPolicies() []PolicyInfo   { return nil }

func TestAuthzCheckStep_SubjectFieldMappedToInput(t *testing.T) {
	eng := &capturingPolicyEngine{}
	app := newTestPolicyApp("policy", eng)

	factory := NewAuthzCheckStepFactory()
	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
		"subject_field": "auth_user_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"auth_user_id": "user-99",
		"other_field":  "value",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stop {
		t.Error("expected Stop=false when policy allows")
	}
	// The input passed to the engine should have "subject" mapped from auth_user_id.
	if eng.lastInput["subject"] != "user-99" {
		t.Errorf("expected input[subject]=user-99, got %v", eng.lastInput["subject"])
	}
	// Original field should still be present.
	if eng.lastInput["auth_user_id"] != "user-99" {
		t.Errorf("expected input[auth_user_id]=user-99, got %v", eng.lastInput["auth_user_id"])
	}
}

func TestAuthzCheckStep_SubjectFieldMappingDoesNotMutatePipelineContext(t *testing.T) {
	eng := &capturingPolicyEngine{}
	app := newTestPolicyApp("policy", eng)

	factory := NewAuthzCheckStepFactory()
	step, err := factory("authz", map[string]any{
		"policy_engine": "policy",
		"subject_field": "auth_user_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"auth_user_id": "user-99",
	}, nil)

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// pc.Current should NOT have had "subject" injected.
	if _, ok := pc.Current["subject"]; ok {
		t.Error("expected pc.Current to not be mutated with 'subject' key")
	}
}
