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
