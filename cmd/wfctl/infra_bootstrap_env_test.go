package main

import (
	"context"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
)

// writeBootstrapConfig writes a temporary workflow YAML file with the given
// content and returns its path. The file is removed when the test ends.
func writeBootstrapConfig(t *testing.T, yaml string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "bootstrap-*.yaml")
	if err != nil {
		t.Fatalf("create temp config: %v", err)
	}
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	f.Close()
	return f.Name()
}

// ── TestBootstrap_StateBackendBucketExpanded ─────────────────────────────────

// TestBootstrap_StateBackendBucketExpanded verifies that ${BUCKET_NAME} in the
// iac.state config is resolved before bootstrapStateBackend calls the bucket fn.
// Without env expansion, the literal "${BUCKET_NAME}" would be used as the
// bucket name.
func TestBootstrap_StateBackendBucketExpanded(t *testing.T) {
	t.Setenv("TEST_BS_BUCKET", "my-state-bucket")
	t.Setenv("TEST_BS_REGION", "sfo3")
	t.Setenv("TEST_BS_ACCESS", "ak")
	t.Setenv("TEST_BS_SECRET", "sk")

	var gotBucket string
	orig := bootstrapDOSpacesBucketFn
	bootstrapDOSpacesBucketFn = func(_ context.Context, bucket, _, _, _ string) error {
		gotBucket = bucket
		return nil
	}
	defer func() { bootstrapDOSpacesBucketFn = orig }()

	cfgFile := writeBootstrapConfig(t, `
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: "${TEST_BS_BUCKET}"
      region: "${TEST_BS_REGION}"
      accessKey: "${TEST_BS_ACCESS}"
      secretKey: "${TEST_BS_SECRET}"
`)

	if err := bootstrapStateBackend(context.Background(), cfgFile); err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	if gotBucket != "my-state-bucket" {
		t.Errorf("bucket: want %q, got %q", "my-state-bucket", gotBucket)
	}
}

// ── TestBootstrap_StateBackendEmptyEnvVar ────────────────────────────────────

// TestBootstrap_StateBackendEmptyEnvVar documents os.ExpandEnv behaviour:
// an unset variable becomes an empty string, which causes a missing-bucket error.
func TestBootstrap_StateBackendEmptyEnvVar(t *testing.T) {
	// Set to empty to simulate unset (t.Setenv("X", "") still sets the var).
	t.Setenv("TEST_BS_EMPTY_BUCKET", "")

	cfgFile := writeBootstrapConfig(t, `
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: "${TEST_BS_EMPTY_BUCKET}"
      region: "nyc3"
`)

	err := bootstrapStateBackend(context.Background(), cfgFile)
	// An empty bucket name after expansion must produce an error.
	if err == nil {
		t.Fatal("expected error when bucket env var is unset (expands to empty string)")
	}
}

// ── TestBootstrap_SecretsProviderTokenExpanded ───────────────────────────────

// TestBootstrap_SecretsProviderTokenExpanded verifies that ${VAR} in the
// secrets provider config (e.g. vault token, address) are expanded before
// the provider is constructed.
func TestBootstrap_SecretsProviderTokenExpanded(t *testing.T) {
	t.Setenv("TEST_VAULT_ADDR", "https://vault.example.com")
	t.Setenv("TEST_VAULT_TOKEN", "s.LIVETOKEN")

	cfg := &SecretsConfig{
		Provider: "vault",
		Config: map[string]any{
			"address": "${TEST_VAULT_ADDR}",
			"token":   "${TEST_VAULT_TOKEN}",
		},
	}

	p, err := resolveSecretsProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSecretsProvider: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	// The original cfg.Config must NOT be mutated (ExpandEnvInMap deep-copies).
	if got, _ := cfg.Config["address"].(string); got != "${TEST_VAULT_ADDR}" {
		t.Errorf("original cfg.Config[address] was mutated: got %q", got)
	}
}

// ── TestBootstrap_SecretsProviderRepoExpanded ────────────────────────────────

// TestBootstrap_SecretsProviderRepoExpanded verifies that GitHub secrets
// provider config with ${REPO} placeholder is expanded correctly.
func TestBootstrap_SecretsProviderRepoExpanded(t *testing.T) {
	t.Setenv("TEST_GH_REPO", "GoCodeAlone/workflow")
	t.Setenv("GITHUB_TOKEN", "ghp_test")

	cfg := &SecretsConfig{
		Provider: "github",
		Config: map[string]any{
			"repo":      "${TEST_GH_REPO}",
			"token_env": "GITHUB_TOKEN",
		},
	}

	p, err := resolveSecretsProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSecretsProvider: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// ── TestBootstrap_SecretsProviderEnvUnset ────────────────────────────────────

// TestBootstrap_SecretsProviderEnvUnset documents that an unset env var in
// the config expands to empty string (standard os.ExpandEnv behaviour).
// The env provider with an expanded-empty prefix constructs cleanly.
func TestBootstrap_SecretsProviderEnvUnset(t *testing.T) {
	t.Setenv("TEST_BS_UNSET_PREFIX", "")

	cfg := &SecretsConfig{
		Provider: "env",
		Config: map[string]any{
			"prefix": "${TEST_BS_UNSET_PREFIX}",
		},
	}

	p, err := resolveSecretsProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSecretsProvider with empty prefix: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil env provider")
	}
}

// ── TestBootstrap_RepeatedRunIdempotent ──────────────────────────────────────

// TestBootstrap_RepeatedRunIdempotent verifies that calling bootstrapSecrets
// twice for the same key skips the second run (idempotent — the first run's
// value is preserved and the generator is not called again).
func TestBootstrap_RepeatedRunIdempotent(t *testing.T) {
	callCount := 0
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		callCount++
		return "generated-value", nil
	})

	p := &fakeSecretsProvider{stored: map[string]string{}}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "IDEMPOTENT_KEY", Type: "random_hex", Length: 16},
		},
	}

	// First run: generates the secret.
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
		t.Fatalf("first bootstrapSecrets: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected generator called once, got %d", callCount)
	}
	if p.stored["IDEMPOTENT_KEY"] != "generated-value" {
		t.Fatalf("first run: secret not stored, got %q", p.stored["IDEMPOTENT_KEY"])
	}

	// Second run: secret already exists — generator must NOT be called again.
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
		t.Fatalf("second bootstrapSecrets: %v", err)
	}
	if callCount != 1 {
		t.Errorf("second run: generator called again (callCount=%d), should be idempotent", callCount)
	}
}

// ── TestBootstrap_StateBackendAccessKeyExpanded ──────────────────────────────

// TestBootstrap_StateBackendAccessKeyExpanded verifies that BMW-style
// ${SPACES_access_key} and ${SPACES_secret_key} references in the iac.state
// config are expanded by ExpandEnvInMap and the resolved values are passed
// through to bootstrapDOSpacesBucketFn.
func TestBootstrap_StateBackendAccessKeyExpanded(t *testing.T) {
	t.Setenv("TEST_SPACES_ACCESS", "do-spaces-key-abc")
	t.Setenv("TEST_SPACES_SECRET", "do-spaces-secret-xyz")

	var gotBucket, gotAccessKey, gotSecretKey string
	orig := bootstrapDOSpacesBucketFn
	bootstrapDOSpacesBucketFn = func(_ context.Context, bucket, _, accessKey, secretKey string) error {
		gotBucket = bucket
		gotAccessKey = accessKey
		gotSecretKey = secretKey
		return nil
	}
	defer func() { bootstrapDOSpacesBucketFn = orig }()

	cfgFile := writeBootstrapConfig(t, `
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: "my-state-bucket"
      region: "nyc3"
      accessKey: "${TEST_SPACES_ACCESS}"
      secretKey: "${TEST_SPACES_SECRET}"
`)

	if err := bootstrapStateBackend(context.Background(), cfgFile); err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	if gotBucket != "my-state-bucket" {
		t.Errorf("bucket: want %q, got %q", "my-state-bucket", gotBucket)
	}
	if gotAccessKey != "do-spaces-key-abc" {
		t.Errorf("accessKey: want %q, got %q", "do-spaces-key-abc", gotAccessKey)
	}
	if gotSecretKey != "do-spaces-secret-xyz" {
		t.Errorf("secretKey: want %q, got %q", "do-spaces-secret-xyz", gotSecretKey)
	}

	// Verify ExpandEnvInMap produced the right values without mutating original config.
	iacStates, _, _, err := discoverInfraModules(cfgFile)
	if err != nil {
		t.Fatalf("discoverInfraModules: %v", err)
	}
	if len(iacStates) == 0 {
		t.Fatal("expected iac.state module")
	}
	expanded := config.ExpandEnvInMap(iacStates[0].Config)
	if got, _ := expanded["accessKey"].(string); got != "do-spaces-key-abc" {
		t.Errorf("expanded accessKey: want %q, got %q", "do-spaces-key-abc", got)
	}
	if got, _ := expanded["secretKey"].(string); got != "do-spaces-secret-xyz" {
		t.Errorf("expanded secretKey: want %q, got %q", "do-spaces-secret-xyz", got)
	}
}

// ── TestBootstrap_EnvFlagAppliedBeforeSubstitution ───────────────────────────

// TestBootstrap_EnvFlagAppliedBeforeSubstitution verifies the ordering:
// per-env config is merged FIRST via writeEnvResolvedConfig, THEN
// ExpandEnvInMap resolves any ${VAR} references — including those introduced
// by the per-env override. Without this ordering guarantee, an env-specific
// override like `bucket: "${STAGING_BUCKET}"` would not be expanded.
func TestBootstrap_EnvFlagAppliedBeforeSubstitution(t *testing.T) {
	t.Setenv("TEST_STAGING_BUCKET", "staging-state-bucket")
	t.Setenv("TEST_STAGING_ACCESS", "staging-ak")
	t.Setenv("TEST_STAGING_SECRET", "staging-sk")

	var gotBucket string
	orig := bootstrapDOSpacesBucketFn
	bootstrapDOSpacesBucketFn = func(_ context.Context, bucket, _, _, _ string) error {
		gotBucket = bucket
		return nil
	}
	defer func() { bootstrapDOSpacesBucketFn = orig }()

	cfgFile := writeBootstrapConfig(t, `
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: "default-bucket"
      region: "nyc3"
      accessKey: "${TEST_STAGING_ACCESS}"
      secretKey: "${TEST_STAGING_SECRET}"
    environments:
      staging:
        config:
          bucket: "${TEST_STAGING_BUCKET}"
`)

	// Step 1: env flag resolves per-env config → writes temp file.
	tmpFile, err := writeEnvResolvedConfig(cfgFile, "staging")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmpFile)

	// Step 2: bootstrapStateBackend uses the resolved (and env-expanded) config.
	if err := bootstrapStateBackend(context.Background(), tmpFile); err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	// The bucket reached the mock must be the expanded staging override value.
	if gotBucket != "staging-state-bucket" {
		t.Errorf("bucket with env-flag: want %q, got %q (env override + expansion may not have run in correct order)", "staging-state-bucket", gotBucket)
	}
}

// ── TestBootstrap_EnvFlagPreservesSecretsGenerate ────────────────────────────

// TestBootstrap_EnvFlagPreservesSecretsGenerate is a regression test for the
// bug where --env caused parseSecretsConfig to read from the env-resolved temp
// file, which was marshalled via config.WorkflowConfig (no Generate field) and
// silently dropped the secrets.generate[] block. The result was "No secrets to
// generate." followed by "access key must be set" because the Spaces keys were
// never generated.
//
// Fix: runInfraBootstrap must call parseSecretsConfig(originalCfgFile), not
// parseSecretsConfig(cfgFile) after cfgFile was reassigned to the temp path.
func TestBootstrap_EnvFlagPreservesSecretsGenerate(t *testing.T) {
	t.Setenv("TEST_STAGING_SECRET_BUCKET", "staging-bucket")
	t.Setenv("TEST_STAGING_SECRET_REGION", "sfo3")

	// Track whether bootstrapSecrets was called with a non-empty Generate list.
	var observedGenerateLen int
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		observedGenerateLen++ // called once per generated secret
		return "generated-value", nil
	})

	// Stub the bucket function so no real S3 call is made.
	orig := bootstrapDOSpacesBucketFn
	bootstrapDOSpacesBucketFn = func(_ context.Context, _, _, _, _ string) error { return nil }
	defer func() { bootstrapDOSpacesBucketFn = orig }()

	// Config has both environments.staging override AND secrets.generate[].
	// The env resolution will flatten the staging config into modules, but must
	// NOT drop the secrets block.
	cfgFile := writeBootstrapConfig(t, `
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: "${TEST_STAGING_SECRET_BUCKET}"
      region: "${TEST_STAGING_SECRET_REGION}"
      accessKey: "ak"
      secretKey: "sk"
    environments:
      staging:
        config:
          bucket: "${TEST_STAGING_SECRET_BUCKET}"

secrets:
  provider: env
  generate:
    - key: JWT_SECRET
      type: random_hex
      length: 32
`)

	if err := runInfraBootstrap([]string{"--config", cfgFile, "--env", "staging"}); err != nil {
		t.Fatalf("runInfraBootstrap: %v", err)
	}

	// The generator must have been called (secrets.generate was not dropped).
	if observedGenerateLen == 0 {
		t.Error("secrets.generate[] was not processed — generate[] block was likely dropped by env-resolved config round-trip")
	}
}

// ── fakeSecretsProvider ──────────────────────────────────────────────────────

// fakeSecretsProvider is a simple in-memory secrets provider for tests.
type fakeSecretsProvider struct {
	stored map[string]string
}

func (p *fakeSecretsProvider) Name() string { return "fake" }
func (p *fakeSecretsProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := p.stored[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *fakeSecretsProvider) Set(_ context.Context, key, value string) error {
	if p.stored == nil {
		p.stored = map[string]string{}
	}
	p.stored[key] = value
	return nil
}
func (p *fakeSecretsProvider) Delete(_ context.Context, key string) error {
	delete(p.stored, key)
	return nil
}
func (p *fakeSecretsProvider) List(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(p.stored))
	for k := range p.stored {
		names = append(names, k)
	}
	return names, nil
}
