package module

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testAuthProvider implements AuthProvider for auth_validate step tests.
type testAuthProvider struct {
	validTokens map[string]map[string]any
	returnErr   error
}

func (m *testAuthProvider) Authenticate(token string) (bool, map[string]any, error) {
	if m.returnErr != nil {
		return false, nil, m.returnErr
	}
	if claims, ok := m.validTokens[token]; ok {
		return true, claims, nil
	}
	return false, nil, nil
}

func newTestAuthApp(authModule string, provider AuthProvider) *MockApplication {
	app := NewMockApplication()
	app.Services[authModule] = provider
	return app
}

func TestAuthValidateStep_SuccessfulAuth(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	provider := &testAuthProvider{
		validTokens: map[string]map[string]any{
			"valid-token-123": {
				"sub":   "user-42",
				"scope": "read write",
				"iss":   "test-issuer",
			},
		},
	}
	app := newTestAuthApp("m2m-auth", provider)

	step, err := factory("auth", map[string]any{
		"auth_module": "m2m-auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"headers": map[string]any{
			"Authorization": "Bearer valid-token-123",
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stop {
		t.Error("expected Stop=false on successful auth")
	}
	if result.Output["sub"] != "user-42" {
		t.Errorf("expected sub=user-42, got %v", result.Output["sub"])
	}
	if result.Output["scope"] != "read write" {
		t.Errorf("expected scope='read write', got %v", result.Output["scope"])
	}
	if result.Output["iss"] != "test-issuer" {
		t.Errorf("expected iss=test-issuer, got %v", result.Output["iss"])
	}
	if result.Output["auth_user_id"] != "user-42" {
		t.Errorf("expected auth_user_id=user-42, got %v", result.Output["auth_user_id"])
	}
}

func TestAuthValidateStep_CustomSubjectField(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	provider := &testAuthProvider{
		validTokens: map[string]map[string]any{
			"tok": {"sub": "svc-1"},
		},
	}
	app := newTestAuthApp("auth", provider)

	step, err := factory("auth", map[string]any{
		"auth_module":   "auth",
		"subject_field": "service_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"headers": map[string]any{"Authorization": "Bearer tok"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["service_id"] != "svc-1" {
		t.Errorf("expected service_id=svc-1, got %v", result.Output["service_id"])
	}
}

func TestAuthValidateStep_CustomTokenSource(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	provider := &testAuthProvider{
		validTokens: map[string]map[string]any{
			"my-token": {"sub": "u1"},
		},
	}
	app := newTestAuthApp("auth", provider)

	step, err := factory("auth", map[string]any{
		"auth_module":  "auth",
		"token_source": "steps.headers.auth_header",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("headers", map[string]any{
		"auth_header": "Bearer my-token",
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stop {
		t.Error("expected Stop=false on success")
	}
	if result.Output["sub"] != "u1" {
		t.Errorf("expected sub=u1, got %v", result.Output["sub"])
	}
}

func TestAuthValidateStep_MissingToken(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	app := newTestAuthApp("auth", &testAuthProvider{})

	step, err := factory("auth", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	// No step output for "parse" — token is missing.

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true for missing token")
	}
	if result.Output["status"] != http.StatusUnauthorized {
		t.Errorf("expected status=401, got %v", result.Output["status"])
	}
}

func TestAuthValidateStep_MalformedHeader(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	app := newTestAuthApp("auth", &testAuthProvider{})

	step, err := factory("auth", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"headers": map[string]any{"Authorization": "Basic dXNlcjpwYXNz"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true for malformed header")
	}
	if result.Output["error"] != "unauthorized" {
		t.Errorf("expected error=unauthorized, got %v", result.Output["error"])
	}
}

func TestAuthValidateStep_InvalidToken(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	provider := &testAuthProvider{
		validTokens: map[string]map[string]any{}, // no valid tokens
	}
	app := newTestAuthApp("auth", provider)

	step, err := factory("auth", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"headers": map[string]any{"Authorization": "Bearer bad-token"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true for invalid token")
	}
	if result.Output["status"] != http.StatusUnauthorized {
		t.Errorf("expected status=401, got %v", result.Output["status"])
	}
}

func TestAuthValidateStep_AuthError(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	provider := &testAuthProvider{
		returnErr: fmt.Errorf("provider error"),
	}
	app := newTestAuthApp("auth", provider)

	step, err := factory("auth", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"headers": map[string]any{"Authorization": "Bearer some-token"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true when provider returns error")
	}
}

func TestAuthValidateStep_WritesHTTPResponse(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	app := newTestAuthApp("auth", &testAuthProvider{})

	step, err := factory("auth", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	w := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": w,
	})
	// No auth header → 401

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "unauthorized") {
		t.Errorf("expected 'unauthorized' in response body, got %q", w.Body.String())
	}
	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true in metadata")
	}
}

func TestAuthValidateStep_FactoryRequiresAuthModule(t *testing.T) {
	factory := NewAuthValidateStepFactory()

	_, err := factory("auth", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing auth_module")
	}
	if !strings.Contains(err.Error(), "'auth_module' is required") {
		t.Errorf("expected auth_module error, got: %v", err)
	}
}

func TestAuthValidateStep_Name(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	app := newTestAuthApp("auth", &testAuthProvider{})

	step, err := factory("my-auth-step", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "my-auth-step" {
		t.Errorf("expected name 'my-auth-step', got %q", step.Name())
	}
}

func TestAuthValidateStep_EmptyBearerToken(t *testing.T) {
	factory := NewAuthValidateStepFactory()
	app := newTestAuthApp("auth", &testAuthProvider{})

	step, err := factory("auth", map[string]any{
		"auth_module": "auth",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"headers": map[string]any{"Authorization": "Bearer "},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true for empty bearer token")
	}
}
