package module

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

// mockSecretSetProvider is an in-memory secrets.Provider for testing secret_set.
// It implements a broader interface than SecretSetProvider requires (including
// Get/Delete/List) so that tests can verify written values via provider.Get.
type mockSecretSetProvider struct {
	data   map[string]string
	setErr error
}

func newMockSecretSetProvider() *mockSecretSetProvider {
	return &mockSecretSetProvider{data: make(map[string]string)}
}

func (m *mockSecretSetProvider) Name() string { return "mock-set" }

func (m *mockSecretSetProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", errors.New("secret not found: " + key)
	}
	return v, nil
}

func (m *mockSecretSetProvider) Set(_ context.Context, key, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.data[key] = value
	return nil
}

func (m *mockSecretSetProvider) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockSecretSetProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

// mockAppWithSetProvider registers a secrets.Provider that supports Set into a MockApplication.
func mockAppWithSetProvider(name string, p SecretSetProvider) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = p
	return app
}

// --- factory validation tests ---

func TestSecretSetStep_MissingModule(t *testing.T) {
	factory := NewSecretSetStepFactory()
	_, err := factory("bad", map[string]any{
		"secrets": map[string]any{"client_id": "my-id"},
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'module' is missing")
	}
}

func TestSecretSetStep_MissingSecrets(t *testing.T) {
	factory := NewSecretSetStepFactory()
	_, err := factory("bad", map[string]any{
		"module": "zoom-secrets",
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'secrets' is missing")
	}
}

func TestSecretSetStep_EmptySecrets(t *testing.T) {
	factory := NewSecretSetStepFactory()
	_, err := factory("bad", map[string]any{
		"module":  "zoom-secrets",
		"secrets": map[string]any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error when 'secrets' map is empty")
	}
}

func TestSecretSetStep_NonStringValue(t *testing.T) {
	factory := NewSecretSetStepFactory()
	_, err := factory("bad", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": 42, // not a string
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error when secret value is not a string")
	}
}

func TestSecretSetStep_EmptyKey(t *testing.T) {
	factory := NewSecretSetStepFactory()
	_, err := factory("bad", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"": "some-value", // empty key name
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error when secrets key is empty")
	}
}

func TestSecretSetStep_WhitespaceOnlyKey(t *testing.T) {
	factory := NewSecretSetStepFactory()
	_, err := factory("bad", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"   ": "some-value", // whitespace-only key
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error when secrets key is whitespace-only")
	}
}

// --- execute tests ---

func TestSecretSetStep_SetSingle(t *testing.T) {
	provider := newMockSecretSetProvider()
	app := mockAppWithSetProvider("zoom-secrets", provider)

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": "my-id-value",
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

	// Verify the value was written to the provider.
	got, getErr := provider.Get(context.Background(), "client_id")
	if getErr != nil {
		t.Fatalf("provider.Get: %v", getErr)
	}
	if got != "my-id-value" {
		t.Errorf("expected client_id=my-id-value in provider, got %q", got)
	}

	// Verify output shape.
	setKeys, ok := result.Output["set_keys"]
	if !ok {
		t.Fatal("expected 'set_keys' in step output")
	}
	keys, ok := setKeys.([]string)
	if !ok {
		t.Fatalf("expected set_keys to be []string, got %T", setKeys)
	}
	if len(keys) != 1 || keys[0] != "client_id" {
		t.Errorf("unexpected set_keys: %v", keys)
	}
}

func TestSecretSetStep_SetMultiple(t *testing.T) {
	provider := newMockSecretSetProvider()
	app := mockAppWithSetProvider("zoom-secrets", provider)

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id":     "my-id-value",
			"client_secret": "my-secret-value",
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

	// Verify both values were written.
	if got, err := provider.Get(context.Background(), "client_id"); err != nil || got != "my-id-value" {
		t.Errorf("client_id mismatch: got=%q err=%v", got, err)
	}
	if got, err := provider.Get(context.Background(), "client_secret"); err != nil || got != "my-secret-value" {
		t.Errorf("client_secret mismatch: got=%q err=%v", got, err)
	}

	setKeys, _ := result.Output["set_keys"].([]string)
	if len(setKeys) != 2 {
		t.Errorf("expected 2 set_keys, got %d: %v", len(setKeys), setKeys)
	}
}

// TestSecretSetStep_TemplateResolution verifies that value templates are resolved
// against the pipeline context before being written to the provider.
func TestSecretSetStep_TemplateResolution(t *testing.T) {
	provider := newMockSecretSetProvider()
	app := mockAppWithSetProvider("zoom-secrets", provider)

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": "{{.steps.form.client_id}}",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Simulate a prior step that returned client_id from a form submission.
	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["form"] = map[string]any{
		"client_id": "resolved-id-from-form",
	}

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	got, getErr := provider.Get(context.Background(), "client_id")
	if getErr != nil {
		t.Fatalf("provider.Get: %v", getErr)
	}
	if got != "resolved-id-from-form" {
		t.Errorf("expected client_id=resolved-id-from-form, got %q", got)
	}
}

func TestSecretSetStep_ProviderError(t *testing.T) {
	provider := newMockSecretSetProvider()
	provider.setErr = errors.New("write denied")
	app := mockAppWithSetProvider("zoom-secrets", provider)

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": "some-value",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from provider.Set")
	}
}

func TestSecretSetStep_ModuleNotFound(t *testing.T) {
	app := NewMockApplication()

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "nonexistent-secrets",
		"secrets": map[string]any{
			"client_id": "value",
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

func TestSecretSetStep_WrongServiceType(t *testing.T) {
	app := NewMockApplication()
	app.Services["zoom-secrets"] = "not-a-provider"

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": "value",
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

func TestSecretSetStep_NoAppContext(t *testing.T) {
	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": "value",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Cast to concrete type to force nil app at execute time.
	concreteStep := step.(*SecretSetStep)
	concreteStep.app = nil

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when app is nil")
	}
}

// TestSecretSetStep_PartialFailure verifies that when writing multiple secrets
// and the provider fails mid-way, earlier writes remain committed (no rollback).
// This matches the documented behavior: secrets backends have no transaction primitive.
func TestSecretSetStep_PartialFailure(t *testing.T) {
	provider := &failAfterNProvider{
		data:     make(map[string]string),
		failAt:   1, // fail on the 2nd Set call
		writeNum: 0,
	}
	app := mockAppWithSetProvider("zoom-secrets", provider)

	factory := NewSecretSetStepFactory()
	// Use sorted key names so iteration order is predictable for the test.
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"aaa_first":  "value-1",
			"bbb_second": "value-2",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from partial failure")
	}

	// At least one write should have succeeded before the failure.
	if len(provider.data) == 0 {
		t.Error("expected at least one write to have succeeded before failure")
	}
}

// failAfterNProvider fails on the Nth Set call.
type failAfterNProvider struct {
	data     map[string]string
	failAt   int
	writeNum int
}

func (p *failAfterNProvider) Name() string { return "fail-after-n" }
func (p *failAfterNProvider) Set(_ context.Context, key, value string) error {
	if p.writeNum >= p.failAt {
		return errors.New("simulated write failure")
	}
	p.data[key] = value
	p.writeNum++
	return nil
}

// mockModuleWithProviderAccessor simulates a built-in secrets module wrapper
// (like SecretsAWSModule/SecretsVaultModule) that exposes Provider() returning
// the underlying secrets.Provider but doesn't implement Set directly on itself.
type mockModuleWithProviderAccessor struct {
	provider secrets.Provider
}

func (m *mockModuleWithProviderAccessor) Provider() secrets.Provider {
	return m.provider
}

// TestSecretSetStep_ProviderAccessorFallback verifies that resolveProvider
// finds Set via the Provider() accessor when the service doesn't implement
// SecretSetProvider directly — matching how SecretsAWSModule etc. work.
func TestSecretSetStep_ProviderAccessorFallback(t *testing.T) {
	// mockSecretSetProvider implements Set directly, but we wrap it in a
	// module-like struct that only exposes it via Provider().
	inner := newMockSecretSetProvider()
	wrapper := &mockModuleWithProviderAccessor{provider: inner}
	app := NewMockApplication()
	app.Services["zoom-secrets"] = wrapper

	factory := NewSecretSetStepFactory()
	step, err := factory("save-creds", map[string]any{
		"module": "zoom-secrets",
		"secrets": map[string]any{
			"client_id": "accessor-value",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	got, getErr := inner.Get(context.Background(), "client_id")
	if getErr != nil {
		t.Fatalf("provider.Get: %v", getErr)
	}
	if got != "accessor-value" {
		t.Errorf("expected client_id=accessor-value, got %q", got)
	}
}

// TestSecretSetStep_RejectsNoValueSentinel verifies that secret_set refuses to
// write the Go template "<no value>" sentinel to the secrets backend. In
// non-strict template mode, a missing map key resolves to "<no value>" rather
// than returning an error. Silently persisting that string would corrupt the
// secrets store.
func TestSecretSetStep_RejectsNoValueSentinel(t *testing.T) {
	provider := newMockSecretSetProvider()
	app := mockAppWithSetProvider("test-secrets", provider)

	step := &SecretSetStep{
		name:       "reject-no-value",
		moduleName: "test-secrets",
		secrets:    map[string]string{"api_key": "{{.steps.missing_step.value}}"},
		app:        app,
		tmpl:       NewTemplateEngine(),
	}

	// PipelineContext with no "missing_step" in step outputs — template
	// resolves to "<no value>" in non-strict mode.
	pc := NewPipelineContext(nil, nil)

	_, err := step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for <no value> sentinel, got nil")
	}
	if !strings.Contains(err.Error(), "<no value>") {
		t.Errorf("expected error mentioning '<no value>', got: %v", err)
	}
}
