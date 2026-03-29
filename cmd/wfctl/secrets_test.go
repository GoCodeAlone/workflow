package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestSecretsDetect_EnvRef(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "db",
				Type: "database.postgres",
				Config: map[string]any{
					"dsn": "${DATABASE_URL}",
				},
			},
		},
	}
	found := detectSecrets(cfg)
	if len(found) == 0 {
		t.Error("expected at least one detected secret for env var reference")
	}
	if found[0].field != "dsn" {
		t.Errorf("expected field 'dsn', got %q", found[0].field)
	}
}

func TestSecretsDetect_FieldName(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "auth",
				Type: "auth.jwt",
				Config: map[string]any{
					"signingKey": "supersecretkey",
				},
			},
		},
	}
	found := detectSecrets(cfg)
	if len(found) == 0 {
		t.Error("expected detection of secret-named field")
	}
	if found[0].field != "signingKey" {
		t.Errorf("expected field 'signingKey', got %q", found[0].field)
	}
}

func TestSecretsDetect_NoSecrets(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "server",
				Type: "http.server",
				Config: map[string]any{
					"port": "8080",
					"host": "localhost",
				},
			},
		},
	}
	found := detectSecrets(cfg)
	if len(found) != 0 {
		t.Errorf("expected no secrets detected, got %d: %+v", len(found), found)
	}
}

func TestIsSecretFieldName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"dsn", true},
		{"apiKey", true},
		{"api_key", true},
		{"token", true},
		{"signingKey", true},
		{"clientSecret", true},
		{"password", true},
		{"port", false},
		{"host", false},
		{"timeout", false},
		{"region", false},
	}
	for _, tt := range tests {
		got := isSecretFieldName(tt.name)
		if got != tt.want {
			t.Errorf("isSecretFieldName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMaskValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc", "****"},
		{"abcd", "****"},
		{"abcde", "ab*de"},
		{"supersecret", "su*******et"},
	}
	for _, tt := range tests {
		got := maskValue(tt.input)
		if got != tt.want {
			t.Errorf("maskValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEnvProvider_SetGet(t *testing.T) {
	p := &envProvider{}
	ctx := context.Background()

	const testKey = "WFCTL_TEST_SECRET_12345"
	const testVal = "test-value"

	if err := p.Set(ctx, testKey, testVal); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, err := p.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != testVal {
		t.Errorf("Get returned %q, want %q", got, testVal)
	}

	// Cleanup
	_ = p.Delete(ctx, testKey)
}

func TestEnvProvider_GetUnset(t *testing.T) {
	p := &envProvider{}
	ctx := context.Background()

	val, err := p.Get(ctx, "WFCTL_DEFINITELY_NOT_SET_XYZ123")
	if err != nil {
		t.Fatalf("Get should not error for unset var: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for unset var, got %q", val)
	}
}

func TestNewSecretsProvider_Env(t *testing.T) {
	p, err := newSecretsProvider("env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewSecretsProvider_Default(t *testing.T) {
	p, err := newSecretsProvider("")
	if err != nil {
		t.Fatalf("unexpected error for empty provider name: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewSecretsProvider_Unknown(t *testing.T) {
	_, err := newSecretsProvider("vault")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestRunSecretsDispatch_UnknownAction(t *testing.T) {
	err := runSecrets([]string{"unknown-action"})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestRunSecretsDispatch_NoArgs(t *testing.T) {
	err := runSecrets([]string{})
	if err == nil {
		t.Error("expected error for no args")
	}
}
