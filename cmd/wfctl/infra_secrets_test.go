package main

import (
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

func mustWriteTempYAML(t *testing.T, content string) string {
	t.Helper()
	path, err := writeTempYAML(t, content)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSecretsConfig_ValidYAML(t *testing.T) {
	yaml := `
secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow-dnd
    token_env: GITHUB_TOKEN
  generate:
    - key: DB_PASSWORD
      type: random_hex
      length: 32
    - key: SPACES_KEY
      type: provider_credential
      source: digitalocean.spaces
`
	f := mustWriteTempYAML(t, yaml)
	cfg, err := parseSecretsConfig(f)
	if err != nil {
		t.Fatalf("parseSecretsConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil SecretsConfig")
	}
	if cfg.Provider != "github" {
		t.Errorf("provider = %q, want %q", cfg.Provider, "github")
	}
	if cfg.Config["repo"] != "GoCodeAlone/workflow-dnd" {
		t.Errorf("repo = %v", cfg.Config["repo"])
	}
	if len(cfg.Generate) != 2 {
		t.Fatalf("expected 2 generate entries, got %d", len(cfg.Generate))
	}
	if cfg.Generate[0].Key != "DB_PASSWORD" || cfg.Generate[0].Length != 32 {
		t.Errorf("generate[0] = %+v", cfg.Generate[0])
	}
	if cfg.Generate[1].Source != "digitalocean.spaces" {
		t.Errorf("generate[1].source = %q", cfg.Generate[1].Source)
	}
}

func TestParseSecretsConfig_AbsentSection(t *testing.T) {
	yaml := `
modules:
  - name: db
    type: infra.database
`
	f := mustWriteTempYAML(t, yaml)
	cfg, err := parseSecretsConfig(f)
	if err != nil {
		t.Fatalf("parseSecretsConfig: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil when secrets section absent")
	}
}

func TestParseInfraConfig_AutoBootstrap(t *testing.T) {
	yaml := `
infra:
  auto_bootstrap: true
`
	f := mustWriteTempYAML(t, yaml)
	cfg, err := parseInfraConfig(f)
	if err != nil {
		t.Fatalf("parseInfraConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil InfraConfig")
	}
	if cfg.AutoBootstrap == nil || !*cfg.AutoBootstrap {
		t.Error("expected auto_bootstrap = true")
	}
}

func TestParseInfraConfig_AbsentSection(t *testing.T) {
	f := mustWriteTempYAML(t, "modules: []\n")
	cfg, err := parseInfraConfig(f)
	if err != nil {
		t.Fatalf("parseInfraConfig: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil when infra section absent")
	}
}

func TestResolveSecretsProvider_EnvProvider(t *testing.T) {
	cfg := &SecretsConfig{
		Provider: "env",
		Config:   map[string]any{"prefix": "TEST_"},
	}
	p, err := resolveSecretsProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSecretsProvider env: %v", err)
	}
	if p.Name() != "env" {
		t.Errorf("provider name = %q, want %q", p.Name(), "env")
	}
}

func TestResolveSecretsProvider_GitHubProvider(t *testing.T) {
	t.Setenv("GH_TOKEN_TEST", "fake-token")
	cfg := &SecretsConfig{
		Provider: "github",
		Config: map[string]any{
			"repo":      "owner/repo",
			"token_env": "GH_TOKEN_TEST",
		},
	}
	p, err := resolveSecretsProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSecretsProvider github: %v", err)
	}
	if p.Name() != "github" {
		t.Errorf("provider name = %q, want %q", p.Name(), "github")
	}
}

func TestResolveSecretsProvider_UnknownProvider(t *testing.T) {
	cfg := &SecretsConfig{Provider: "mystery"}
	_, err := resolveSecretsProvider(cfg)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestResolveSecretsProvider_Nil(t *testing.T) {
	_, err := resolveSecretsProvider(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestParseSecretsConfig_MissingFile(t *testing.T) {
	_, err := parseSecretsConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestResolveSecretsProvider_KeychainProvider(t *testing.T) {
	cfg := &SecretsConfig{
		Provider: "keychain",
		Config:   map[string]any{"service": "test-workflow-app"},
	}
	p, err := resolveSecretsProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSecretsProvider keychain: %v", err)
	}
	if p.Name() != "keychain" {
		t.Errorf("provider name = %q, want %q", p.Name(), "keychain")
	}
}

func TestResolveSecretsProvider_KeychainMissingService(t *testing.T) {
	cfg := &SecretsConfig{
		Provider: "keychain",
		Config:   map[string]any{},
	}
	_, err := resolveSecretsProvider(cfg)
	if err == nil {
		t.Error("expected error when 'service' is missing")
	}
}

// Ensure GitHubSecretsProvider satisfies secrets.Provider interface.
var _ secrets.Provider = (*secrets.GitHubSecretsProvider)(nil)

// Ensure KeychainProvider satisfies secrets.Provider interface.
var _ secrets.Provider = (*secrets.KeychainProvider)(nil)
