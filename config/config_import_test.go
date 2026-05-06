package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromFile_WithImports(t *testing.T) {
	dir := t.TempDir()

	// Create imported file with a module
	modulesYAML := `
modules:
  - name: my-db
    type: storage.sqlite
    config:
      path: ./data/app.db
`
	if err := os.WriteFile(filepath.Join(dir, "modules.yaml"), []byte(modulesYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create imported routes file with a pipeline
	routesYAML := `
pipelines:
  get-items:
    steps:
      - name: query
        type: step.db_query
        config:
          query: "SELECT * FROM items"

triggers:
  get-items-trigger:
    type: http
    config:
      path: /items
      method: GET
`
	if err := os.WriteFile(filepath.Join(dir, "routes.yaml"), []byte(routesYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main file that imports both
	mainYAML := `
imports:
  - modules.yaml
  - routes.yaml

modules:
  - name: my-server
    type: http.server
    config:
      address: ":8080"

workflows:
  main:
    steps:
      - name: init
        type: step.log
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 modules (my-server from main + my-db from import)
	if len(cfg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(cfg.Modules))
	}

	// Main file's module should come first
	if cfg.Modules[0].Name != "my-server" {
		t.Errorf("expected first module to be 'my-server', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[1].Name != "my-db" {
		t.Errorf("expected second module to be 'my-db', got %q", cfg.Modules[1].Name)
	}

	// Should have the pipeline from import
	if _, ok := cfg.Pipelines["get-items"]; !ok {
		t.Error("expected 'get-items' pipeline from import")
	}

	// Should have the trigger from import
	if _, ok := cfg.Triggers["get-items-trigger"]; !ok {
		t.Error("expected 'get-items-trigger' trigger from import")
	}

	// Should have the workflow from main
	if _, ok := cfg.Workflows["main"]; !ok {
		t.Error("expected 'main' workflow from main file")
	}

	// Imports field should be cleared after processing
	if len(cfg.Imports) != 0 {
		t.Errorf("expected imports to be cleared, got %v", cfg.Imports)
	}
}

func TestLoadFromFile_ImportPrecedence(t *testing.T) {
	dir := t.TempDir()

	// Create imported file with a pipeline
	importedYAML := `
pipelines:
  my-pipeline:
    steps:
      - name: imported-step
        type: step.log
        config:
          message: "from import"

triggers:
  my-trigger:
    type: http
    config:
      path: /imported
`
	if err := os.WriteFile(filepath.Join(dir, "imported.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main file with same pipeline name — main should win
	mainYAML := `
imports:
  - imported.yaml

pipelines:
  my-pipeline:
    steps:
      - name: main-step
        type: step.log
        config:
          message: "from main"

triggers:
  my-trigger:
    type: http
    config:
      path: /main
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Main file's pipeline definition should win
	pipeline, ok := cfg.Pipelines["my-pipeline"]
	if !ok {
		t.Fatal("expected 'my-pipeline' pipeline")
	}
	pMap, ok := pipeline.(map[string]any)
	if !ok {
		t.Fatal("expected pipeline to be a map")
	}
	steps, ok := pMap["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatal("expected pipeline to have steps")
	}
	step0, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatal("expected step to be a map")
	}
	if step0["name"] != "main-step" {
		t.Errorf("expected main file's step 'main-step', got %q", step0["name"])
	}

	// Main file's trigger definition should win
	trigger, ok := cfg.Triggers["my-trigger"]
	if !ok {
		t.Fatal("expected 'my-trigger' trigger")
	}
	tMap, ok := trigger.(map[string]any)
	if !ok {
		t.Fatal("expected trigger to be a map")
	}
	tConfig, ok := tMap["config"].(map[string]any)
	if !ok {
		t.Fatal("expected trigger config to be a map")
	}
	if tConfig["path"] != "/main" {
		t.Errorf("expected main file's trigger path '/main', got %q", tConfig["path"])
	}
}

func TestLoadFromFile_CircularImport(t *testing.T) {
	dir := t.TempDir()

	// a.yaml imports b.yaml
	aYAML := `
imports:
  - b.yaml
modules:
  - name: mod-a
    type: test.a
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(aYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// b.yaml imports a.yaml — circular!
	bYAML := `
imports:
  - a.yaml
modules:
  - name: mod-b
    type: test.b
`
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(bYAML), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromFile(filepath.Join(dir, "a.yaml"))
	if err == nil {
		t.Fatal("expected circular import error, got nil")
	}
	if !strings.Contains(err.Error(), "circular import") {
		t.Errorf("expected error to contain 'circular import', got: %v", err)
	}
}

func TestLoadFromFile_NestedImports(t *testing.T) {
	dir := t.TempDir()

	// c.yaml — leaf, no imports
	cYAML := `
modules:
  - name: mod-c
    type: test.c

pipelines:
  pipeline-c:
    steps:
      - name: step-c
        type: step.log
`
	if err := os.WriteFile(filepath.Join(dir, "c.yaml"), []byte(cYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// b.yaml imports c.yaml
	bYAML := `
imports:
  - c.yaml

modules:
  - name: mod-b
    type: test.b

pipelines:
  pipeline-b:
    steps:
      - name: step-b
        type: step.log
`
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(bYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// a.yaml imports b.yaml
	aYAML := `
imports:
  - b.yaml

modules:
  - name: mod-a
    type: test.a

pipelines:
  pipeline-a:
    steps:
      - name: step-a
        type: step.log
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(aYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(filepath.Join(dir, "a.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 modules: mod-a (main), mod-b (from b.yaml), mod-c (from c.yaml via b.yaml)
	if len(cfg.Modules) != 3 {
		t.Errorf("expected 3 modules, got %d", len(cfg.Modules))
	}

	// Main file's module should be first
	if cfg.Modules[0].Name != "mod-a" {
		t.Errorf("expected first module 'mod-a', got %q", cfg.Modules[0].Name)
	}

	// Should have all three pipelines
	for _, name := range []string{"pipeline-a", "pipeline-b", "pipeline-c"} {
		if _, ok := cfg.Pipelines[name]; !ok {
			t.Errorf("expected pipeline %q", name)
		}
	}
}

func TestLoadFromFile_ImportNotFound(t *testing.T) {
	dir := t.TempDir()

	mainYAML := `
imports:
  - nonexistent.yaml

modules:
  - name: mod-main
    type: test.main
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromFile(mainPath)
	if err == nil {
		t.Fatal("expected error for missing import, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent.yaml") {
		t.Errorf("expected error to reference 'nonexistent.yaml', got: %v", err)
	}
}

func TestLoadFromFile_NoImports(t *testing.T) {
	dir := t.TempDir()

	// Standard config without imports — backward compatibility
	mainYAML := `
modules:
  - name: my-server
    type: http.server
    config:
      address: ":8080"

workflows:
  main:
    steps:
      - name: init
        type: step.log

triggers:
  main-trigger:
    type: http
    config:
      path: /
      method: GET

pipelines:
  main-pipeline:
    steps:
      - name: step1
        type: step.log
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "my-server" {
		t.Errorf("expected module 'my-server', got %q", cfg.Modules[0].Name)
	}
	if _, ok := cfg.Workflows["main"]; !ok {
		t.Error("expected 'main' workflow")
	}
	if _, ok := cfg.Triggers["main-trigger"]; !ok {
		t.Error("expected 'main-trigger' trigger")
	}
	if _, ok := cfg.Pipelines["main-pipeline"]; !ok {
		t.Error("expected 'main-pipeline' pipeline")
	}
}

func TestLoadFromFile_ImportRelativePath(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory for imported files
	subDir := filepath.Join(dir, "includes")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create imported file in subdirectory
	importedYAML := `
modules:
  - name: imported-mod
    type: test.imported

pipelines:
  imported-pipeline:
    steps:
      - name: step1
        type: step.log
`
	if err := os.WriteFile(filepath.Join(subDir, "shared.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Main file uses relative path to subdirectory
	mainYAML := `
imports:
  - includes/shared.yaml

modules:
  - name: main-mod
    type: test.main
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Modules) != 2 {
		t.Errorf("expected 2 modules, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "main-mod" {
		t.Errorf("expected first module 'main-mod', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[1].Name != "imported-mod" {
		t.Errorf("expected second module 'imported-mod', got %q", cfg.Modules[1].Name)
	}
	if _, ok := cfg.Pipelines["imported-pipeline"]; !ok {
		t.Error("expected 'imported-pipeline' from import")
	}
}

func TestLoadFromFile_ImportedImportsAlsoImport(t *testing.T) {
	dir := t.TempDir()

	// Three-level deep: main -> domain1 -> shared

	// shared.yaml — common modules
	sharedYAML := `
modules:
  - name: shared-db
    type: storage.sqlite
    config:
      path: ./shared.db

platform:
  logging:
    level: info
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(sharedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// domain1.yaml imports shared.yaml
	domain1YAML := `
imports:
  - shared.yaml

modules:
  - name: domain1-service
    type: http.server
    config:
      address: ":8081"

pipelines:
  domain1-handler:
    steps:
      - name: handle
        type: step.log

platform:
  domain1:
    feature: enabled
`
	if err := os.WriteFile(filepath.Join(dir, "domain1.yaml"), []byte(domain1YAML), 0644); err != nil {
		t.Fatal(err)
	}

	// main.yaml imports domain1.yaml
	mainYAML := `
imports:
  - domain1.yaml

modules:
  - name: main-gateway
    type: http.server
    config:
      address: ":8080"

workflows:
  main:
    steps:
      - name: init
        type: step.log

platform:
  main:
    version: "1.0"
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 modules: main-gateway, domain1-service, shared-db
	if len(cfg.Modules) != 3 {
		t.Errorf("expected 3 modules, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "main-gateway" {
		t.Errorf("expected first module 'main-gateway', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[1].Name != "domain1-service" {
		t.Errorf("expected second module 'domain1-service', got %q", cfg.Modules[1].Name)
	}
	if cfg.Modules[2].Name != "shared-db" {
		t.Errorf("expected third module 'shared-db', got %q", cfg.Modules[2].Name)
	}

	// Pipeline from domain1
	if _, ok := cfg.Pipelines["domain1-handler"]; !ok {
		t.Error("expected 'domain1-handler' pipeline from domain1 import")
	}

	// Workflow from main
	if _, ok := cfg.Workflows["main"]; !ok {
		t.Error("expected 'main' workflow")
	}

	// Platform should have all three keys
	if cfg.Platform == nil {
		t.Fatal("expected platform config")
	}
	for _, key := range []string{"main", "domain1", "logging"} {
		if _, ok := cfg.Platform[key]; !ok {
			t.Errorf("expected platform key %q", key)
		}
	}
}

func TestLoadFromFile_DiamondImports(t *testing.T) {
	dir := t.TempDir()

	// shared.yaml (D) - common dependency imported by both B and C
	sharedYAML := `
modules:
  - name: shared-db
    type: state.connector
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(sharedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// service-b.yaml (B) imports shared.yaml
	serviceBYAML := `
imports:
  - shared.yaml

modules:
  - name: service-b
    type: http.handler
`
	if err := os.WriteFile(filepath.Join(dir, "service-b.yaml"), []byte(serviceBYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// service-c.yaml (C) imports shared.yaml
	serviceCYAML := `
imports:
  - shared.yaml

modules:
  - name: service-c
    type: http.handler
`
	if err := os.WriteFile(filepath.Join(dir, "service-c.yaml"), []byte(serviceCYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// main.yaml (A) imports both B and C
	mainYAML := `
imports:
  - service-b.yaml
  - service-c.yaml

modules:
  - name: main-gateway
    type: http.server
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error loading diamond imports: %v", err)
	}

	gotModules := make(map[string]bool)
	for _, m := range cfg.Modules {
		if gotModules[m.Name] {
			t.Errorf("duplicate module %q found in diamond import scenario", m.Name)
		}
		gotModules[m.Name] = true
	}

	for _, name := range []string{"main-gateway", "service-b", "service-c", "shared-db"} {
		if !gotModules[name] {
			t.Errorf("expected module %q to be loaded in diamond import scenario", name)
		}
	}

	if len(cfg.Modules) != 4 {
		t.Errorf("expected exactly 4 modules (no duplicates), got %d", len(cfg.Modules))
	}
}

// TestLoadFromFile_ImportSecretsMerge pins the processImports behavior for
// top-level WorkflowConfig.Secrets: Generate (dedupe by Key) and Entries
// (dedupe by Name) are merged from imports; scalar/map fields follow
// main-wins precedence consistent with the rest of the import path.
func TestLoadFromFile_ImportSecretsMerge(t *testing.T) {
	dir := t.TempDir()

	importedYAML := `
secrets:
  defaultStore: import-store
  provider: import-provider
  generate:
    - key: IMPORTED_KEY
      type: random_hex
      length: 32
    - key: SHARED_KEY
      type: random_hex
      length: 16
  entries:
    - name: IMPORTED_ENTRY
      store: vault
    - name: SHARED_ENTRY
      store: vault
`
	if err := os.WriteFile(filepath.Join(dir, "imported.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
imports:
  - imported.yaml

secrets:
  defaultStore: main-store
  generate:
    - key: MAIN_KEY
      type: random_hex
      length: 64
    - key: SHARED_KEY
      type: provider_credential
      source: main
  entries:
    - name: MAIN_ENTRY
      store: vault
    - name: SHARED_ENTRY
      store: main-store
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Secrets == nil {
		t.Fatal("expected merged Secrets, got nil")
	}

	// Scalar precedence: main wins where set, import fills where unset.
	if cfg.Secrets.DefaultStore != "main-store" {
		t.Errorf("expected DefaultStore=main-store, got %q", cfg.Secrets.DefaultStore)
	}
	if cfg.Secrets.Provider != "import-provider" {
		t.Errorf("expected Provider=import-provider (filled from import), got %q", cfg.Secrets.Provider)
	}

	// Generate: dedupe by Key, main-wins on conflict, append new entries.
	genByKey := make(map[string]SecretGen)
	for _, g := range cfg.Secrets.Generate {
		if _, exists := genByKey[g.Key]; exists {
			t.Errorf("duplicate Generate key %q after merge", g.Key)
		}
		genByKey[g.Key] = g
	}
	for _, k := range []string{"MAIN_KEY", "IMPORTED_KEY", "SHARED_KEY"} {
		if _, ok := genByKey[k]; !ok {
			t.Errorf("expected Generate key %q present after merge", k)
		}
	}
	if shared, ok := genByKey["SHARED_KEY"]; ok && shared.Type != "provider_credential" {
		t.Errorf("expected main-wins for SHARED_KEY type, got %q", shared.Type)
	}

	// Entries: dedupe by Name, main-wins on conflict, append new entries.
	entryByName := make(map[string]SecretEntry)
	for _, e := range cfg.Secrets.Entries {
		if _, exists := entryByName[e.Name]; exists {
			t.Errorf("duplicate Entries name %q after merge", e.Name)
		}
		entryByName[e.Name] = e
	}
	for _, n := range []string{"MAIN_ENTRY", "IMPORTED_ENTRY", "SHARED_ENTRY"} {
		if _, ok := entryByName[n]; !ok {
			t.Errorf("expected Entries name %q present after merge", n)
		}
	}
	if shared, ok := entryByName["SHARED_ENTRY"]; ok && shared.Store != "main-store" {
		t.Errorf("expected main-wins for SHARED_ENTRY store, got %q", shared.Store)
	}
}

// TestLoadFromFile_ImportSecretsOnlyInImport covers the case the original
// PR-1 fix missed: top-level `secrets:` appears only in an imported file,
// not in main. Without the merge, cfg.Secrets is nil after LoadFromFile.
func TestLoadFromFile_ImportSecretsOnlyInImport(t *testing.T) {
	dir := t.TempDir()

	importedYAML := `
secrets:
  generate:
    - key: ONLY_IN_IMPORT
      type: random_hex
      length: 32
`
	if err := os.WriteFile(filepath.Join(dir, "imported.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
imports:
  - imported.yaml

modules:
  - name: dummy
    type: noop
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Secrets == nil {
		t.Fatal("expected cfg.Secrets to be populated from import, got nil")
	}
	if len(cfg.Secrets.Generate) != 1 || cfg.Secrets.Generate[0].Key != "ONLY_IN_IMPORT" {
		t.Errorf("expected Generate=[{Key:ONLY_IN_IMPORT}], got %v", cfg.Secrets.Generate)
	}
}

// TestProcessImports_MergesSecretStoresFromImport pins that
// WorkflowConfig.SecretStores is merged across imports. ResolveSecretStore
// and getProviderForStore look up store names against this map; without the
// merge an imported defaultStore or entries[*].store fails as an
// unknown-provider error.
func TestProcessImports_MergesSecretStoresFromImport(t *testing.T) {
	dir := t.TempDir()

	importedYAML := `
secretStores:
  vault:
    provider: vault
    config:
      address: https://vault.example.com
  shared-store:
    provider: aws-secrets-manager
    config:
      region: us-east-1
secrets:
  defaultStore: vault
`
	if err := os.WriteFile(filepath.Join(dir, "imported.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
imports:
  - imported.yaml

secretStores:
  shared-store:
    provider: gcp-secret-manager
    config:
      project: my-project
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// vault came from import only — must survive the merge.
	if _, ok := cfg.SecretStores["vault"]; !ok {
		t.Error("expected SecretStores[vault] from import")
	}
	// shared-store: parent wins.
	if shared, ok := cfg.SecretStores["shared-store"]; !ok {
		t.Error("expected SecretStores[shared-store]")
	} else if shared.Provider != "gcp-secret-manager" {
		t.Errorf("expected main-wins on shared-store provider, got %q", shared.Provider)
	}
	// defaultStore came from imported secrets:
	if cfg.Secrets == nil || cfg.Secrets.DefaultStore != "vault" {
		t.Errorf("expected Secrets.DefaultStore=vault from import, got %v", cfg.Secrets)
	}
}

// TestProcessImports_SecretsConfigMergesPerKey_LocalOverride pins the
// per-key merge for Secrets.Config. The "shared defaults + local override"
// pattern requires that an imported Config provides defaults (e.g. `repo`)
// while the main file overrides only specific keys (e.g. `token_env`); the
// imported keys not present in main must survive.
func TestProcessImports_SecretsConfigMergesPerKey_LocalOverride(t *testing.T) {
	dir := t.TempDir()

	importedYAML := `
secrets:
  config:
    repo: GoCodeAlone/workflow
    token_env: SHARED_TOKEN
    api_url: https://api.github.com
`
	if err := os.WriteFile(filepath.Join(dir, "imported.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
imports:
  - imported.yaml

secrets:
  config:
    token_env: MAIN_TOKEN
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Secrets == nil {
		t.Fatal("expected cfg.Secrets non-nil")
	}
	// Imported defaults survive.
	if cfg.Secrets.Config["repo"] != "GoCodeAlone/workflow" {
		t.Errorf("expected repo=GoCodeAlone/workflow (imported default), got %v", cfg.Secrets.Config["repo"])
	}
	if cfg.Secrets.Config["api_url"] != "https://api.github.com" {
		t.Errorf("expected api_url survived from import, got %v", cfg.Secrets.Config["api_url"])
	}
	// Main override wins on conflict.
	if cfg.Secrets.Config["token_env"] != "MAIN_TOKEN" {
		t.Errorf("expected token_env=MAIN_TOKEN (main override), got %v", cfg.Secrets.Config["token_env"])
	}
}

// TestProcessImports_MergesEnvironmentsFromImport pins that
// WorkflowConfig.Environments is merged across imports. ResolveSecretStore
// consults Environments[env].SecretsStoreOverride to route secrets to a
// specific store per environment; without the merge, an imported per-env
// override is dropped and secret resolution silently falls back to
// defaultStore/provider — fetching from the wrong backend.
func TestProcessImports_MergesEnvironmentsFromImport(t *testing.T) {
	dir := t.TempDir()

	importedYAML := `
environments:
  staging:
    provider: aws
    region: us-east-1
    secretsStoreOverride: vault
  production:
    provider: aws
    region: us-west-2
    secretsStoreOverride: aws-prod
`
	if err := os.WriteFile(filepath.Join(dir, "imported.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
imports:
  - imported.yaml

environments:
  production:
    provider: aws
    region: us-west-2
    secretsStoreOverride: aws-prod-main
  local:
    provider: docker
    region: localhost
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// staging came from import only — must survive merge.
	staging, ok := cfg.Environments["staging"]
	if !ok {
		t.Fatal("expected Environments[staging] from import")
	}
	if staging.SecretsStoreOverride != "vault" {
		t.Errorf("expected staging.SecretsStoreOverride=vault from import, got %q", staging.SecretsStoreOverride)
	}

	// production: parent wins (main override).
	prod, ok := cfg.Environments["production"]
	if !ok {
		t.Fatal("expected Environments[production]")
	}
	if prod.SecretsStoreOverride != "aws-prod-main" {
		t.Errorf("expected main-wins on production.SecretsStoreOverride, got %q", prod.SecretsStoreOverride)
	}

	// local came from main only.
	if _, ok := cfg.Environments["local"]; !ok {
		t.Error("expected Environments[local] from main")
	}
}

// TestProcessImports_MergesInfraFromImport pins that WorkflowConfig.Infra is
// merged from imported files when the main config has no infra: block.
// parseInfraConfig (cmd/wfctl) uses config.LoadFromFile, so this exercises
// the same code path as wfctl infra apply auto-bootstrap detection.
func TestProcessImports_MergesInfraFromImport(t *testing.T) {
	dir := t.TempDir()

	importedYAML := `
infra:
  auto_bootstrap: false
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(importedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
imports:
  - shared.yaml

modules:
  - name: dummy
    type: noop
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(mainPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Infra == nil {
		t.Fatal("expected cfg.Infra to be populated from import, got nil")
	}
	if cfg.Infra.AutoBootstrap == nil || *cfg.Infra.AutoBootstrap {
		t.Error("expected AutoBootstrap=false from import")
	}
}
