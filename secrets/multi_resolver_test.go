package secrets

import (
	"context"
	"errors"
	"testing"
)

func TestMultiResolver_ExpandEnvDefault(t *testing.T) {
	t.Setenv("MY_DB_HOST", "localhost")
	t.Setenv("MY_DB_PORT", "5432")

	m := NewMultiResolver()
	result, err := m.Expand(context.Background(), "host=${MY_DB_HOST}:${MY_DB_PORT}")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if result != "host=localhost:5432" {
		t.Errorf("expected 'host=localhost:5432', got %q", result)
	}
}

func TestMultiResolver_ExpandEnvScheme(t *testing.T) {
	t.Setenv("APP_KEY", "secret123")

	m := NewMultiResolver()
	result, err := m.Expand(context.Background(), "${env:APP_KEY}")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if result != "secret123" {
		t.Errorf("expected 'secret123', got %q", result)
	}
}

func TestMultiResolver_ExpandNoReferences(t *testing.T) {
	m := NewMultiResolver()
	result, err := m.Expand(context.Background(), "plain-string-value")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if result != "plain-string-value" {
		t.Errorf("expected 'plain-string-value', got %q", result)
	}
}

func TestMultiResolver_ExpandUnknownScheme(t *testing.T) {
	m := NewMultiResolver()
	_, err := m.Expand(context.Background(), "${unknown:key}")
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}

func TestMultiResolver_ExpandMissingEnvVar(t *testing.T) {
	m := NewMultiResolver()
	_, err := m.Expand(context.Background(), "${NONEXISTENT_VAR_XYZ_123}")
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestMultiResolver_RegisterAndUseCustomProvider(t *testing.T) {
	m := NewMultiResolver()
	m.Register("mock", &mockProvider{
		secrets: map[string]string{"db/password": "s3cret"},
	})

	result, err := m.Expand(context.Background(), "${mock:db/password}")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if result != "s3cret" {
		t.Errorf("expected 's3cret', got %q", result)
	}
}

func TestMultiResolver_ExpandMultipleMixed(t *testing.T) {
	t.Setenv("HOST", "myhost")

	m := NewMultiResolver()
	m.Register("mock", &mockProvider{
		secrets: map[string]string{"api-key": "key123"},
	})

	result, err := m.Expand(context.Background(), "url=https://${HOST}/api?key=${mock:api-key}")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	expected := "url=https://myhost/api?key=key123"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestMultiResolver_Unregister(t *testing.T) {
	m := NewMultiResolver()
	m.Register("temp", &mockProvider{
		secrets: map[string]string{"key": "val"},
	})

	// Should work
	_, err := m.Expand(context.Background(), "${temp:key}")
	if err != nil {
		t.Fatalf("Expand before unregister: %v", err)
	}

	m.Unregister("temp")

	// Should fail
	_, err = m.Expand(context.Background(), "${temp:key}")
	if err == nil {
		t.Fatal("expected error after unregister")
	}
}

func TestMultiResolver_Schemes(t *testing.T) {
	m := NewMultiResolver()
	schemes := m.Schemes()
	found := false
	for _, s := range schemes {
		if s == "env" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'env' in schemes")
	}
}

func TestMultiResolver_Provider(t *testing.T) {
	m := NewMultiResolver()
	if m.Provider("env") == nil {
		t.Error("expected non-nil env provider")
	}
	if m.Provider("nonexistent") != nil {
		t.Error("expected nil for nonexistent provider")
	}
}

func TestParseReference(t *testing.T) {
	tests := []struct {
		input  string
		scheme string
		key    string
	}{
		{"vault:secret/path#field", "vault", "secret/path#field"},
		{"aws-sm:my-secret", "aws-sm", "my-secret"},
		{"env:DB_HOST", "env", "DB_HOST"},
		{"DB_HOST", "env", "DB_HOST"},
		{"file:path/to/secret", "file", "path/to/secret"},
	}

	for _, tt := range tests {
		scheme, key := parseReference(tt.input)
		if scheme != tt.scheme || key != tt.key {
			t.Errorf("parseReference(%q) = (%q, %q), want (%q, %q)",
				tt.input, scheme, key, tt.scheme, tt.key)
		}
	}
}

// mockProvider is a simple in-memory provider for testing.
type mockProvider struct {
	secrets map[string]string
}

func (p *mockProvider) Name() string { return "mock" }

func (p *mockProvider) Get(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}
	v, ok := p.secrets[key]
	if !ok {
		return "", errors.New("not found: " + key)
	}
	return v, nil
}

func (p *mockProvider) Set(_ context.Context, key, value string) error {
	if key == "" {
		return ErrInvalidKey
	}
	p.secrets[key] = value
	return nil
}

func (p *mockProvider) Delete(_ context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	delete(p.secrets, key)
	return nil
}

func (p *mockProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.secrets))
	for k := range p.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}
