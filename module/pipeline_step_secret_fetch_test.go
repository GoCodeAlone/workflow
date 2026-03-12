package module

import (
	"context"
	"errors"
	"testing"
)

// mockSecretProvider is an in-memory SecretFetchProvider for testing.
type mockSecretProvider struct {
	data   map[string]string
	getErr error
}

func newMockSecretProvider(data map[string]string) *mockSecretProvider {
	return &mockSecretProvider{data: data}
}

func (m *mockSecretProvider) Get(_ context.Context, key string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.data[key]
	if !ok {
		return "", errors.New("secret not found: " + key)
	}
	return v, nil
}

// mockAppWithSecrets creates a MockApplication with a SecretFetchProvider registered.
func mockAppWithSecrets(name string, p SecretFetchProvider) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = p
	return app
}

// --- factory validation tests ---

func TestSecretFetchStep_MissingModule(t *testing.T) {
	factory := NewSecretFetchStepFactory()
	_, err := factory("bad", map[string]any{
		"secrets": map[string]any{"key": "arn:x"},
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'module' is missing")
	}
}

func TestSecretFetchStep_MissingSecrets(t *testing.T) {
	factory := NewSecretFetchStepFactory()
	_, err := factory("bad", map[string]any{
		"module": "aws-secrets",
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'secrets' is missing")
	}
}

func TestSecretFetchStep_EmptySecrets(t *testing.T) {
	factory := NewSecretFetchStepFactory()
	_, err := factory("bad", map[string]any{
		"module":  "aws-secrets",
		"secrets": map[string]any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'secrets' map is empty")
	}
}

func TestSecretFetchStep_NonStringSecretID(t *testing.T) {
	factory := NewSecretFetchStepFactory()
	_, err := factory("bad", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"mykey": 42, // not a string
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error when secret ID is not a string")
	}
}

// --- execute tests ---

func TestSecretFetchStep_FetchSingle(t *testing.T) {
	provider := newMockSecretProvider(map[string]string{
		"arn:aws:secretsmanager:us-east-1:123:secret:my-token": "tok-xyz",
	})
	app := mockAppWithSecrets("aws-secrets", provider)

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"token": "arn:aws:secretsmanager:us-east-1:123:secret:my-token",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["token"] != "tok-xyz" {
		t.Errorf("expected token=tok-xyz, got %v", result.Output["token"])
	}
	if result.Output["fetched"] != true {
		t.Errorf("expected fetched=true, got %v", result.Output["fetched"])
	}
}

func TestSecretFetchStep_FetchMultiple(t *testing.T) {
	provider := newMockSecretProvider(map[string]string{
		"arn:secret:token-url":     "https://login.example.com/oauth/token",
		"arn:secret:client-id":     "client-abc",
		"arn:secret:client-secret": "super-secret",
	})
	app := mockAppWithSecrets("aws-secrets", provider)

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"token_url":     "arn:secret:token-url",
			"client_id":     "arn:secret:client-id",
			"client_secret": "arn:secret:client-secret",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["token_url"] != "https://login.example.com/oauth/token" {
		t.Errorf("unexpected token_url: %v", result.Output["token_url"])
	}
	if result.Output["client_id"] != "client-abc" {
		t.Errorf("unexpected client_id: %v", result.Output["client_id"])
	}
	if result.Output["client_secret"] != "super-secret" {
		t.Errorf("unexpected client_secret: %v", result.Output["client_secret"])
	}
	if result.Output["fetched"] != true {
		t.Errorf("expected fetched=true")
	}
}

// TestSecretFetchStep_TenantAwareDynamic verifies that secret IDs support
// Go template expressions so ARNs can be constructed per-tenant at runtime.
func TestSecretFetchStep_TenantAwareDynamic(t *testing.T) {
	provider := newMockSecretProvider(map[string]string{
		"arn:aws:secretsmanager:us-east-1:123:secret:tenant-acme-creds": "acme-token",
	})
	app := mockAppWithSecrets("aws-secrets", provider)

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			// Template expression resolved against pipeline context (tenant_id from trigger data).
			"token": "arn:aws:secretsmanager:us-east-1:123:secret:tenant-{{.tenant_id}}-creds",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Simulate trigger data containing the tenant ID.
	pc := NewPipelineContext(map[string]any{"tenant_id": "acme"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["token"] != "acme-token" {
		t.Errorf("expected token=acme-token, got %v", result.Output["token"])
	}
}

// TestSecretFetchStep_TenantAwareFromStepOutput verifies resolution from a
// previous step's output (the most common tenant-aware use case in pipelines).
func TestSecretFetchStep_TenantAwareFromStepOutput(t *testing.T) {
	provider := newMockSecretProvider(map[string]string{
		"arn:aws:secretsmanager:us-east-1:123:secret:salesforce-client-secret": "sf-secret-xyz",
	})
	app := mockAppWithSecrets("aws-secrets", provider)

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"client_secret": "{{.steps.lookup_integration.row.client_secret_arn}}",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Simulate a previous step that returned a client_secret_arn.
	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["lookup_integration"] = map[string]any{
		"row": map[string]any{
			"client_secret_arn": "arn:aws:secretsmanager:us-east-1:123:secret:salesforce-client-secret",
		},
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["client_secret"] != "sf-secret-xyz" {
		t.Errorf("expected client_secret=sf-secret-xyz, got %v", result.Output["client_secret"])
	}
}

func TestSecretFetchStep_ProviderError(t *testing.T) {
	provider := newMockSecretProvider(nil)
	provider.getErr = errors.New("access denied")
	app := mockAppWithSecrets("aws-secrets", provider)

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"token": "arn:secret:token",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from provider.Get")
	}
}

func TestSecretFetchStep_ModuleNotFound(t *testing.T) {
	app := NewMockApplication()

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "nonexistent-secrets",
		"secrets": map[string]any{
			"token": "arn:secret:token",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when module not found")
	}
}

func TestSecretFetchStep_WrongServiceType(t *testing.T) {
	app := NewMockApplication()
	app.Services["aws-secrets"] = "not-a-secret-provider" // wrong type

	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"token": "arn:secret:token",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for wrong service type")
	}
}

func TestSecretFetchStep_NoAppContext(t *testing.T) {
	factory := NewSecretFetchStepFactory()
	step, err := factory("fetch-creds", map[string]any{
		"module": "aws-secrets",
		"secrets": map[string]any{
			"token": "arn:secret:token",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Cast to concrete type to access internal state.
	concreteStep := step.(*SecretFetchStep)
	concreteStep.app = nil // force nil

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when app is nil")
	}
}
