package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const multistoreYAML = `
secretStores:
  github:
    provider: github-actions
    config:
      org: GoCodeAlone
  aws:
    provider: aws-secrets-manager
    config:
      region: us-east-1

secrets:
  defaultStore: github
  provider: env
  entries:
    - name: DATABASE_URL
      description: PostgreSQL connection string
      store: aws
    - name: JWT_SECRET
      description: JWT signing key
    - name: STRIPE_KEY
      description: Stripe API key
      store: aws

environments:
  local:
    provider: docker
    secretsStoreOverride: ""
  staging:
    provider: kubernetes
    secretsStoreOverride: github
  production:
    provider: kubernetes
    secretsStoreOverride: aws

infrastructure:
  resources:
    - name: main-db
      type: database
      environments:
        local:
          strategy: container
          dockerImage: postgres:16
          port: 5432
        staging:
          strategy: provision
          provider: aws
          config:
            instanceClass: db.t3.micro
        production:
          strategy: provision
          provider: aws
          config:
            instanceClass: db.r6g.large
    - name: cache
      type: cache
      environments:
        local:
          strategy: container
          dockerImage: redis:7
        production:
          strategy: existing
          connection:
            host: redis.internal
            port: 6379
            auth: mytoken
`

func TestMultiStoreSecretsConfig(t *testing.T) {
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(multistoreYAML), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// SecretStores
	if len(cfg.SecretStores) != 2 {
		t.Fatalf("SecretStores len: got %d, want 2", len(cfg.SecretStores))
	}
	gh, ok := cfg.SecretStores["github"]
	if !ok {
		t.Fatal("expected github store")
	}
	if gh.Provider != "github-actions" {
		t.Errorf("github provider: got %q", gh.Provider)
	}
	awsStore, ok := cfg.SecretStores["aws"]
	if !ok {
		t.Fatal("expected aws store")
	}
	if awsStore.Provider != "aws-secrets-manager" {
		t.Errorf("aws provider: got %q", awsStore.Provider)
	}

	// SecretsConfig
	if cfg.Secrets == nil {
		t.Fatal("expected secrets config")
	}
	if cfg.Secrets.DefaultStore != "github" {
		t.Errorf("DefaultStore: got %q, want github", cfg.Secrets.DefaultStore)
	}
	// Legacy provider still present
	if cfg.Secrets.Provider != "env" {
		t.Errorf("Provider: got %q, want env", cfg.Secrets.Provider)
	}
	if len(cfg.Secrets.Entries) != 3 {
		t.Fatalf("Entries len: got %d, want 3", len(cfg.Secrets.Entries))
	}

	// Per-secret store field
	dbEntry := cfg.Secrets.Entries[0]
	if dbEntry.Name != "DATABASE_URL" {
		t.Errorf("entry[0] name: got %q", dbEntry.Name)
	}
	if dbEntry.Store != "aws" {
		t.Errorf("DATABASE_URL store: got %q, want aws", dbEntry.Store)
	}

	jwtEntry := cfg.Secrets.Entries[1]
	if jwtEntry.Store != "" {
		t.Errorf("JWT_SECRET store should be empty (use default), got %q", jwtEntry.Store)
	}
}

func TestMultiStoreEnvironmentOverride(t *testing.T) {
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(multistoreYAML), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	local := cfg.Environments["local"]
	if local == nil {
		t.Fatal("expected local environment")
	}
	if local.SecretsStoreOverride != "" {
		t.Errorf("local SecretsStoreOverride: got %q, want empty", local.SecretsStoreOverride)
	}

	staging := cfg.Environments["staging"]
	if staging.SecretsStoreOverride != "github" {
		t.Errorf("staging SecretsStoreOverride: got %q, want github", staging.SecretsStoreOverride)
	}

	prod := cfg.Environments["production"]
	if prod.SecretsStoreOverride != "aws" {
		t.Errorf("production SecretsStoreOverride: got %q, want aws", prod.SecretsStoreOverride)
	}
}

func TestInfraPerEnvironmentResolution(t *testing.T) {
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(multistoreYAML), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Infrastructure == nil {
		t.Fatal("expected infrastructure config")
	}
	if len(cfg.Infrastructure.Resources) != 2 {
		t.Fatalf("Resources len: got %d, want 2", len(cfg.Infrastructure.Resources))
	}

	db := cfg.Infrastructure.Resources[0]
	if db.Name != "main-db" {
		t.Errorf("resource[0] name: got %q", db.Name)
	}
	if len(db.Environments) != 3 {
		t.Fatalf("main-db environments len: got %d, want 3", len(db.Environments))
	}

	local := db.Environments["local"]
	if local.Strategy != "container" {
		t.Errorf("local strategy: got %q, want container", local.Strategy)
	}
	if local.DockerImage != "postgres:16" {
		t.Errorf("local dockerImage: got %q", local.DockerImage)
	}
	if local.Port != 5432 {
		t.Errorf("local port: got %d, want 5432", local.Port)
	}

	staging := db.Environments["staging"]
	if staging.Strategy != "provision" {
		t.Errorf("staging strategy: got %q, want provision", staging.Strategy)
	}
	if staging.Provider != "aws" {
		t.Errorf("staging provider: got %q, want aws", staging.Provider)
	}

	// Cache resource with "existing" strategy
	cache := cfg.Infrastructure.Resources[1]
	if cache.Name != "cache" {
		t.Errorf("resource[1] name: got %q", cache.Name)
	}
	prodCache := cache.Environments["production"]
	if prodCache.Strategy != "existing" {
		t.Errorf("prod cache strategy: got %q, want existing", prodCache.Strategy)
	}
	if prodCache.Connection == nil {
		t.Fatal("expected connection config for existing strategy")
	}
	if prodCache.Connection.Host != "redis.internal" {
		t.Errorf("connection host: got %q", prodCache.Connection.Host)
	}
	if prodCache.Connection.Port != 6379 {
		t.Errorf("connection port: got %d", prodCache.Connection.Port)
	}
	if prodCache.Connection.Auth != "mytoken" {
		t.Errorf("connection auth: got %q", prodCache.Connection.Auth)
	}
}

func TestSecretStoreConfigRoundTrip(t *testing.T) {
	store := SecretStoreConfig{
		Provider: "vault",
		Config: map[string]any{
			"address": "https://vault.example.com",
			"mount":   "secret",
		},
	}
	out, err := yaml.Marshal(store)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var store2 SecretStoreConfig
	if err := yaml.Unmarshal(out, &store2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if store2.Provider != "vault" {
		t.Errorf("Provider: got %q", store2.Provider)
	}
	if store2.Config["address"] != "https://vault.example.com" {
		t.Errorf("Config.address: got %v", store2.Config["address"])
	}
}
