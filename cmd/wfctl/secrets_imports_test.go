package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadSecretsConfig_HonorsImports verifies that loadSecretsConfig processes
// import directives, making secrets declared only in imported files visible.
func TestLoadSecretsConfig_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	shared := `secrets:
  defaultStore: vault
  entries:
    - name: API_TOKEN
      description: API authentication token
    - name: DB_PASSWORD
`
	main := `imports:
  - shared.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadSecretsConfig(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("loadSecretsConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil SecretsConfig")
	}
	if cfg.DefaultStore != "vault" {
		t.Errorf("defaultStore = %q, want %q", cfg.DefaultStore, "vault")
	}
	if len(cfg.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(cfg.Entries), cfg.Entries)
	}
	names := make([]string, len(cfg.Entries))
	for i, e := range cfg.Entries {
		names[i] = e.Name
	}
	for _, want := range []string{"API_TOKEN", "DB_PASSWORD"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("entry %q not found in imported secrets; got %v", want, names)
		}
	}
}

// TestLoadSecretsConfig_MainWinsOverImport verifies that when the same entry
// is declared in both main and imported files, the main file's definition wins.
func TestLoadSecretsConfig_MainWinsOverImport(t *testing.T) {
	dir := t.TempDir()

	shared := `secrets:
  defaultStore: imported-store
  entries:
    - name: SHARED_SECRET
`
	main := `imports:
  - shared.yaml
secrets:
  defaultStore: main-store
  entries:
    - name: MAIN_SECRET
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadSecretsConfig(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("loadSecretsConfig: %v", err)
	}
	// Main file's defaultStore wins.
	if cfg.DefaultStore != "main-store" {
		t.Errorf("defaultStore = %q, want %q", cfg.DefaultStore, "main-store")
	}
	// Both entries visible.
	names := make(map[string]bool)
	for _, e := range cfg.Entries {
		names[e.Name] = true
	}
	if !names["MAIN_SECRET"] {
		t.Error("expected MAIN_SECRET in merged entries")
	}
	if !names["SHARED_SECRET"] {
		t.Error("expected SHARED_SECRET from import in merged entries")
	}
}

// TestLoadWorkflowConfigForSecrets_HonorsImports verifies that
// loadWorkflowConfigForSecrets merges secretStores from imported files.
func TestLoadWorkflowConfigForSecrets_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	shared := `secretStores:
  vault:
    provider: vault
    config:
      address: https://vault.example.com
secrets:
  defaultStore: vault
  entries:
    - name: API_TOKEN
`
	main := `imports:
  - shared.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadWorkflowConfigForSecrets(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("loadWorkflowConfigForSecrets: %v", err)
	}
	if cfg.SecretStores == nil {
		t.Fatal("expected SecretStores to be populated from import")
	}
	if _, ok := cfg.SecretStores["vault"]; !ok {
		t.Errorf("expected 'vault' store in SecretStores, got %v", cfg.SecretStores)
	}
	if cfg.Secrets == nil || cfg.Secrets.DefaultStore != "vault" {
		t.Errorf("expected defaultStore=vault from import, got %+v", cfg.Secrets)
	}
	if len(cfg.Secrets.Entries) == 0 {
		t.Error("expected entries from import, got none")
	}
}

// TestParseSecretsConfig_HonorsImports verifies that parseSecretsConfig
// (used by infra commands) merges the secrets section from imported files.
func TestParseSecretsConfig_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	shared := `secrets:
  provider: env
  entries:
    - name: IMPORTED_SECRET
  generate:
    - key: IMPORTED_KEY
      type: random_hex
      length: 32
`
	main := `imports:
  - shared.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseSecretsConfig(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("parseSecretsConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil SecretsConfig from import")
	}
	if len(cfg.Entries) == 0 {
		t.Error("expected entries from import, got none")
	}
	if cfg.Entries[0].Name != "IMPORTED_SECRET" {
		t.Errorf("entry[0].Name = %q, want %q", cfg.Entries[0].Name, "IMPORTED_SECRET")
	}
	if len(cfg.Generate) == 0 {
		t.Error("expected generate entries from import, got none")
	}
	if cfg.Generate[0].Key != "IMPORTED_KEY" {
		t.Errorf("generate[0].Key = %q, want %q", cfg.Generate[0].Key, "IMPORTED_KEY")
	}
}

// TestSecretsValidate_HonorsImports verifies that runSecretsValidate sees
// secrets entries that come from imported files.
func TestSecretsValidate_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	// Pre-set the env var so the secret is "set" during validate.
	t.Setenv("IMPORTED_VALIDATE_KEY", "test-value")

	shared := `secrets:
  provider: env
  entries:
    - name: IMPORTED_VALIDATE_KEY
`
	main := `imports:
  - shared.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runSecretsValidate([]string{"--config", filepath.Join(dir, "main.yaml")})
	if err != nil {
		t.Errorf("runSecretsValidate: expected success with set secret from import, got: %v", err)
	}
}

// TestSecretsValidate_ImportedEntryUnset verifies that runSecretsValidate
// reports missing imported entries as errors.
func TestSecretsValidate_ImportedEntryUnset(t *testing.T) {
	dir := t.TempDir()

	const envKey = "WFCTL_IMPORT_TEST_UNSET_KEY_XYZ123"
	os.Unsetenv(envKey)

	shared := `secrets:
  provider: env
  entries:
    - name: ` + envKey + `
`
	main := `imports:
  - shared.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runSecretsValidate([]string{"--config", filepath.Join(dir, "main.yaml")})
	if err == nil {
		t.Error("expected error for unset imported secret, got nil")
	}
	if !strings.Contains(err.Error(), envKey) {
		t.Errorf("expected error to mention %q, got: %v", envKey, err)
	}
}

// TestSecretsSetup_HonorsImportedDefaultStore verifies that resolveSecretStoreForSetup
// uses the defaultStore from an imported secrets section.
func TestSecretsSetup_HonorsImportedDefaultStore(t *testing.T) {
	dir := t.TempDir()

	shared := `secrets:
  defaultStore: vault
  entries:
    - name: MY_SECRET
`
	main := `imports:
  - shared.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	wfCfg, err := loadWorkflowConfigForSecrets(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("loadWorkflowConfigForSecrets: %v", err)
	}

	// Find the MY_SECRET entry in the merged config.
	var entry SecretsConfig
	if wfCfg.Secrets != nil {
		entry = *wfCfg.Secrets
	}
	_ = entry

	if len(wfCfg.Secrets.Entries) == 0 {
		t.Fatal("expected entries from import, got none")
	}
	secretEntry := wfCfg.Secrets.Entries[0]
	store := resolveSecretStoreForSetup(secretEntry, "local", wfCfg)
	if store != "vault" {
		t.Errorf("resolveSecretStoreForSetup = %q, want %q (from imported defaultStore)", store, "vault")
	}
}

// TestLoadSecretsConfig_MissingFile verifies that a missing config file
// returns a default env-provider config rather than an error.
func TestLoadSecretsConfig_MissingFile_DefaultsToEnv(t *testing.T) {
	cfg, err := loadSecretsConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Errorf("expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected default config, got nil")
	}
	if cfg.Provider != "env" {
		t.Errorf("expected default provider 'env', got %q", cfg.Provider)
	}
}
