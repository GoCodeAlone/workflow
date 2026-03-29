package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestInjectSecrets_MultiStoreRouting(t *testing.T) {
	// Set secrets in the environment
	t.Setenv("DB_PASS", "pg-password")
	t.Setenv("JWT_KEY", "jwt-signing-key")

	wfCfg := &config.WorkflowConfig{
		SecretStores: map[string]*config.SecretStoreConfig{
			"local-env": {Provider: "env"},
		},
		Secrets: &config.SecretsConfig{
			DefaultStore: "local-env",
			Entries: []config.SecretEntry{
				{Name: "DB_PASS", Store: "local-env"},
				{Name: "JWT_KEY"}, // uses defaultStore
			},
		},
	}

	secrets, err := injectSecrets(context.Background(), wfCfg, "local")
	if err != nil {
		t.Fatalf("injectSecrets: %v", err)
	}

	if secrets["DB_PASS"] != "pg-password" {
		t.Errorf("DB_PASS: got %q, want pg-password", secrets["DB_PASS"])
	}
	if secrets["JWT_KEY"] != "jwt-signing-key" {
		t.Errorf("JWT_KEY: got %q, want jwt-signing-key", secrets["JWT_KEY"])
	}
}

func TestInjectSecrets_EnvOverrideRouting(t *testing.T) {
	t.Setenv("MY_API_KEY", "api-key-value")

	wfCfg := &config.WorkflowConfig{
		SecretStores: map[string]*config.SecretStoreConfig{
			"local": {Provider: "env"},
		},
		Secrets: &config.SecretsConfig{
			DefaultStore: "local",
			Entries: []config.SecretEntry{
				{Name: "MY_API_KEY"},
			},
		},
		Environments: map[string]*config.EnvironmentConfig{
			"staging": {SecretsStoreOverride: "local"}, // routes to env provider
		},
	}

	secrets, err := injectSecrets(context.Background(), wfCfg, "staging")
	if err != nil {
		t.Fatalf("injectSecrets: %v", err)
	}
	if secrets["MY_API_KEY"] != "api-key-value" {
		t.Errorf("MY_API_KEY: got %q, want api-key-value", secrets["MY_API_KEY"])
	}
}

func TestInjectSecrets_UnknownStore_Error(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Entries: []config.SecretEntry{
				{Name: "MY_SECRET", Store: "nonexistent-provider"},
			},
		},
	}

	_, err := injectSecrets(context.Background(), wfCfg, "")
	if err == nil {
		t.Error("expected error for unknown store provider")
	}
}

func TestInjectSecrets_LegacyProvider(t *testing.T) {
	t.Setenv("LEGACY_SECRET", "legacy-value")

	wfCfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Provider: "env", // legacy field
			Entries: []config.SecretEntry{
				{Name: "LEGACY_SECRET"},
			},
		},
	}

	secrets, err := injectSecrets(context.Background(), wfCfg, "")
	if err != nil {
		t.Fatalf("injectSecrets (legacy): %v", err)
	}
	if secrets["LEGACY_SECRET"] != "legacy-value" {
		t.Errorf("LEGACY_SECRET: got %q, want legacy-value", secrets["LEGACY_SECRET"])
	}
}

func TestInjectSecrets_EmptyEntries(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Provider: "env",
			Entries:  nil,
		},
	}
	secrets, err := injectSecrets(context.Background(), wfCfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets != nil {
		t.Errorf("expected nil for empty entries, got %v", secrets)
	}
}
