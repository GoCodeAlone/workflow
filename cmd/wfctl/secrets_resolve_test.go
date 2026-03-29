package main

import (
	"context"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestResolveSecretStore_NilConfig(t *testing.T) {
	got := ResolveSecretStore("MY_SECRET", "production", nil)
	if got != "env" {
		t.Errorf("expected env fallback, got %q", got)
	}
}

func TestResolveSecretStore_PerSecretStore(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			DefaultStore: "github",
			Entries: []config.SecretEntry{
				{Name: "DATABASE_URL", Store: "aws"},
				{Name: "JWT_SECRET"},
			},
		},
	}
	if got := ResolveSecretStore("DATABASE_URL", "production", cfg); got != "aws" {
		t.Errorf("DATABASE_URL: got %q, want aws", got)
	}
	if got := ResolveSecretStore("JWT_SECRET", "production", cfg); got != "github" {
		t.Errorf("JWT_SECRET: got %q, want github (defaultStore)", got)
	}
}

func TestResolveSecretStore_EnvOverride(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			DefaultStore: "github",
			Entries:      []config.SecretEntry{{Name: "MY_KEY"}},
		},
		Environments: map[string]*config.EnvironmentConfig{
			"staging": {SecretsStoreOverride: "vault"},
		},
	}
	// Env override applies (no per-secret store set)
	if got := ResolveSecretStore("MY_KEY", "staging", cfg); got != "vault" {
		t.Errorf("staging: got %q, want vault", got)
	}
	// Production uses defaultStore
	if got := ResolveSecretStore("MY_KEY", "production", cfg); got != "github" {
		t.Errorf("production: got %q, want github", got)
	}
}

func TestResolveSecretStore_PerSecretOverridesEnv(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Entries: []config.SecretEntry{
				{Name: "DB_URL", Store: "aws"}, // explicit per-secret store
			},
		},
		Environments: map[string]*config.EnvironmentConfig{
			"production": {SecretsStoreOverride: "vault"},
		},
	}
	// Per-secret store should win over env override
	if got := ResolveSecretStore("DB_URL", "production", cfg); got != "aws" {
		t.Errorf("got %q, want aws (per-secret wins)", got)
	}
}

func TestResolveSecretStore_LegacyProvider(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Provider: "vault",
			Entries:  []config.SecretEntry{{Name: "TOKEN"}},
		},
	}
	if got := ResolveSecretStore("TOKEN", "", cfg); got != "vault" {
		t.Errorf("legacy provider: got %q, want vault", got)
	}
}

func TestResolveSecretStore_EnvFallback(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Entries: []config.SecretEntry{{Name: "MY_VAR"}},
		},
	}
	if got := ResolveSecretStore("MY_VAR", "", cfg); got != "env" {
		t.Errorf("fallback: got %q, want env", got)
	}
}

func TestGetProviderForStore_EnvFromSecretStores(t *testing.T) {
	cfg := &config.WorkflowConfig{
		SecretStores: map[string]*config.SecretStoreConfig{
			"local-env": {Provider: "env"},
		},
	}
	p, err := getProviderForStore("local-env", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider")
	}
}

func TestGetProviderForStore_UnknownFallsToProvider(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	p, err := getProviderForStore("env", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider")
	}
}

func TestGetProviderForStore_UnknownProvider(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	_, err := getProviderForStore("nonexistent-provider", cfg)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestBuildSecretStatuses_Basic(t *testing.T) {
	// Set an env var for one secret
	t.Setenv("EXISTING_SECRET", "somevalue")

	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Entries: []config.SecretEntry{
				{Name: "EXISTING_SECRET"},
				{Name: "MISSING_SECRET"},
			},
		},
	}

	statuses, err := buildSecretStatuses(context.Background(), "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	existing := statuses[0]
	if existing.State != SecretSet {
		t.Errorf("EXISTING_SECRET: state=%v, want SecretSet", existing.State)
	}
	if !existing.IsSet {
		t.Error("EXISTING_SECRET: IsSet should be true")
	}

	missing := statuses[1]
	if missing.State != SecretNotSet {
		t.Errorf("MISSING_SECRET: state=%v, want SecretNotSet", missing.State)
	}
	if missing.IsSet {
		t.Error("MISSING_SECRET: IsSet should be false")
	}
}

func TestBuildSecretStatuses_MultiStore(t *testing.T) {
	t.Setenv("ENV_SECRET", "value")

	cfg := &config.WorkflowConfig{
		SecretStores: map[string]*config.SecretStoreConfig{
			"local": {Provider: "env"},
		},
		Secrets: &config.SecretsConfig{
			DefaultStore: "local",
			Entries: []config.SecretEntry{
				{Name: "ENV_SECRET", Store: "local"},
			},
		},
	}

	statuses, err := buildSecretStatuses(context.Background(), "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Store != "local" {
		t.Errorf("store: got %q, want local", statuses[0].Store)
	}
	if statuses[0].State != SecretSet {
		t.Errorf("state: got %v, want SecretSet", statuses[0].State)
	}
}

func TestBuildSecretStatuses_UnknownStore(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Entries: []config.SecretEntry{
				{Name: "MY_SECRET", Store: "nonexistent-provider"},
			},
		},
	}

	statuses, err := buildSecretStatuses(context.Background(), "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != SecretUnconfigured {
		t.Errorf("state: got %v, want SecretUnconfigured", statuses[0].State)
	}
	if statuses[0].Error == "" {
		t.Error("expected error message for unknown store")
	}
}

func TestEnvProviderCheck(t *testing.T) {
	p := &envProvider{}
	ctx := context.Background()

	key := "TEST_CHECK_SECRET_12345"
	os.Unsetenv(key)

	state, err := p.Check(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != SecretNotSet {
		t.Errorf("unset: got %v, want SecretNotSet", state)
	}

	os.Setenv(key, "hello")
	defer os.Unsetenv(key)

	state, err = p.Check(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != SecretSet {
		t.Errorf("set: got %v, want SecretSet", state)
	}
}
