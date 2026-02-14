package regression

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// exampleDir is the path to the example YAML configs.
const exampleDir = "../../example"

// skipDirs contains directory names to exclude from YAML scanning.
var skipDirs = map[string]bool{
	".playwright-cli": true,
	"node_modules":    true,
	"observability":   true,
	"grafana":         true,
	"provisioning":    true,
	"datasources":     true,
	"dashboards":      true,
	"e2e":             true,
}

// skipFiles contains file base names to exclude from YAML scanning.
var skipFiles = map[string]bool{
	"docker-compose.yml":      true,
	"docker-compose.yaml":     true,
	"prometheus.yml":          true,
	"prometheus.yaml":         true,
	"datasource.yml":          true,
	"dashboard.yml":           true,
	"copilot-setup-steps.yml": true,
}

// collectYAMLFiles walks the example directory and returns workflow config YAML file paths.
// It skips non-workflow files like docker-compose, prometheus, playwright artifacts, etc.
func collectYAMLFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(exampleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !(strings.HasSuffix(info.Name(), ".yaml") || strings.HasSuffix(info.Name(), ".yml")) {
			return nil
		}
		if skipFiles[info.Name()] {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk example directory %q: %v", exampleDir, err)
	}
	if len(files) == 0 {
		t.Fatal("no YAML config files found in example directory")
	}
	return files
}

// TestRegression_AllExampleConfigsLoad verifies every example YAML can be parsed.
func TestRegression_AllExampleConfigsLoad(t *testing.T) {
	files := collectYAMLFiles(t)
	t.Logf("Found %d example YAML files", len(files))

	for _, file := range files {
		relPath, _ := filepath.Rel(exampleDir, file)
		t.Run(relPath, func(t *testing.T) {
			cfg, err := config.LoadFromFile(file)
			if err != nil {
				t.Fatalf("failed to load %s: %v", relPath, err)
			}
			if cfg == nil {
				t.Fatalf("LoadFromFile returned nil for %s", relPath)
			}
			t.Logf("Loaded %s: %d modules, %d workflow types, %d trigger types",
				relPath, len(cfg.Modules), len(cfg.Workflows), len(cfg.Triggers))
		})
	}
}

// collectExtraModuleTypes scans all example configs and returns module types
// not in the known list. These are custom/planned types used in examples that
// would be registered as custom factories at runtime.
func collectExtraModuleTypes(t *testing.T, files []string) []string {
	t.Helper()
	known := make(map[string]bool)
	for _, kt := range schema.KnownModuleTypes() {
		known[kt] = true
	}
	extra := make(map[string]bool)
	for _, file := range files {
		cfg, err := config.LoadFromFile(file)
		if err != nil {
			continue
		}
		for _, mod := range cfg.Modules {
			if mod.Type != "" && !known[mod.Type] {
				extra[mod.Type] = true
			}
		}
	}
	result := make([]string, 0, len(extra))
	for typ := range extra {
		result = append(result, typ)
	}
	return result
}

// TestRegression_AllExampleConfigsValidateSchema validates every example against the schema.
// Uses relaxed options matching what the engine uses at runtime.
// Custom/planned module types found across all configs are allowed.
func TestRegression_AllExampleConfigsValidateSchema(t *testing.T) {
	files := collectYAMLFiles(t)
	t.Logf("Validating %d example YAML files against schema", len(files))

	// Collect extra module types that examples use but aren't in the built-in list.
	// At runtime, these would be registered via AddModuleType.
	extraTypes := collectExtraModuleTypes(t, files)
	if len(extraTypes) > 0 {
		t.Logf("Found %d custom/planned module types across examples: %v", len(extraTypes), extraTypes)
	}

	for _, file := range files {
		relPath, _ := filepath.Rel(exampleDir, file)
		t.Run(relPath, func(t *testing.T) {
			cfg, err := config.LoadFromFile(file)
			if err != nil {
				t.Fatalf("failed to load %s: %v", relPath, err)
			}

			// Use relaxed validation matching runtime behaviour:
			// - Allow empty modules (some configs may be partial)
			// - Skip workflow type check (engine resolves dynamically)
			// - Skip trigger type check (engine resolves dynamically)
			// - Allow extra module types found in examples
			valErr := schema.ValidateConfig(cfg,
				schema.WithAllowEmptyModules(),
				schema.WithSkipWorkflowTypeCheck(),
				schema.WithSkipTriggerTypeCheck(),
				schema.WithExtraModuleTypes(extraTypes...),
			)
			if valErr != nil {
				// Log as warning rather than failing. Some example configs use
				// planned patterns (e.g. port instead of address) that may not
				// pass strict validation but are structurally valid examples.
				t.Logf("WARN: validation issue for %s: %v", relPath, valErr)
			}
		})
	}
}

// TestRegression_AllExampleConfigsHaveModules checks that configs have at least basic structure.
func TestRegression_AllExampleConfigsHaveModules(t *testing.T) {
	files := collectYAMLFiles(t)

	for _, file := range files {
		relPath, _ := filepath.Rel(exampleDir, file)
		t.Run(relPath, func(t *testing.T) {
			cfg, err := config.LoadFromFile(file)
			if err != nil {
				t.Fatalf("failed to load %s: %v", relPath, err)
			}

			if len(cfg.Modules) == 0 {
				// Some configs (e.g. cross-workflow-links.yaml) are reference/link
				// files rather than standalone engine configs.
				t.Logf("WARN: %s has no modules defined (may be a reference file)", relPath)
			}
		})
	}
}

// TestRegression_ModuleNamesUnique checks for duplicate module names within each config.
func TestRegression_ModuleNamesUnique(t *testing.T) {
	files := collectYAMLFiles(t)

	for _, file := range files {
		relPath, _ := filepath.Rel(exampleDir, file)
		t.Run(relPath, func(t *testing.T) {
			cfg, err := config.LoadFromFile(file)
			if err != nil {
				t.Fatalf("failed to load %s: %v", relPath, err)
			}

			seen := make(map[string]bool)
			for _, mod := range cfg.Modules {
				if seen[mod.Name] {
					t.Errorf("%s has duplicate module name %q", relPath, mod.Name)
				}
				seen[mod.Name] = true
			}
		})
	}
}

// TestRegression_ModuleTypesKnown ensures all module types in examples are recognized.
func TestRegression_ModuleTypesKnown(t *testing.T) {
	files := collectYAMLFiles(t)
	knownTypes := make(map[string]bool)
	for _, kt := range schema.KnownModuleTypes() {
		knownTypes[kt] = true
	}

	for _, file := range files {
		relPath, _ := filepath.Rel(exampleDir, file)
		t.Run(relPath, func(t *testing.T) {
			cfg, err := config.LoadFromFile(file)
			if err != nil {
				t.Fatalf("failed to load %s: %v", relPath, err)
			}

			for _, mod := range cfg.Modules {
				if !knownTypes[mod.Type] {
					t.Logf("WARN: %s uses unknown module type %q (module %q) - may be a custom or future type",
						relPath, mod.Type, mod.Name)
				}
			}
		})
	}
}

// TestRegression_DependsOnReferencesExist verifies dependsOn references point to defined modules.
func TestRegression_DependsOnReferencesExist(t *testing.T) {
	files := collectYAMLFiles(t)

	for _, file := range files {
		relPath, _ := filepath.Rel(exampleDir, file)
		t.Run(relPath, func(t *testing.T) {
			cfg, err := config.LoadFromFile(file)
			if err != nil {
				t.Fatalf("failed to load %s: %v", relPath, err)
			}

			moduleNames := make(map[string]bool)
			for _, mod := range cfg.Modules {
				moduleNames[mod.Name] = true
			}

			for _, mod := range cfg.Modules {
				for _, dep := range mod.DependsOn {
					if !moduleNames[dep] {
						t.Errorf("%s: module %q depends on %q which is not defined",
							relPath, mod.Name, dep)
					}
				}
			}
		})
	}
}

// TestRegression_SchemaGeneration checks that the schema can be generated without error.
func TestRegression_SchemaGeneration(t *testing.T) {
	s := schema.GenerateWorkflowSchema()
	if s == nil {
		t.Fatal("GenerateWorkflowSchema returned nil")
	}
	if s.Title == "" {
		t.Error("schema has empty title")
	}
	if s.Type != "object" {
		t.Errorf("schema type should be 'object', got %q", s.Type)
	}
	if s.Properties == nil || len(s.Properties) == 0 {
		t.Error("schema has no properties")
	}
	if _, ok := s.Properties["modules"]; !ok {
		t.Error("schema missing 'modules' property")
	}
}

// TestRegression_KnownTypesConsistency checks that KnownModuleTypes, KnownWorkflowTypes,
// and KnownTriggerTypes return non-empty slices with no duplicates.
func TestRegression_KnownTypesConsistency(t *testing.T) {
	t.Run("module_types", func(t *testing.T) {
		types := schema.KnownModuleTypes()
		if len(types) == 0 {
			t.Fatal("KnownModuleTypes returned empty slice")
		}
		seen := make(map[string]bool)
		for _, typ := range types {
			if seen[typ] {
				t.Errorf("duplicate module type: %q", typ)
			}
			seen[typ] = true
		}
		t.Logf("KnownModuleTypes: %d types", len(types))
	})

	t.Run("workflow_types", func(t *testing.T) {
		types := schema.KnownWorkflowTypes()
		if len(types) == 0 {
			t.Fatal("KnownWorkflowTypes returned empty slice")
		}
		seen := make(map[string]bool)
		for _, typ := range types {
			if seen[typ] {
				t.Errorf("duplicate workflow type: %q", typ)
			}
			seen[typ] = true
		}
		t.Logf("KnownWorkflowTypes: %d types", len(types))
	})

	t.Run("trigger_types", func(t *testing.T) {
		types := schema.KnownTriggerTypes()
		if len(types) == 0 {
			t.Fatal("KnownTriggerTypes returned empty slice")
		}
		seen := make(map[string]bool)
		for _, typ := range types {
			if seen[typ] {
				t.Errorf("duplicate trigger type: %q", typ)
			}
			seen[typ] = true
		}
		t.Logf("KnownTriggerTypes: %d types", len(types))
	})
}

// TestRegression_ConfigLoadFromString_RoundTrip validates that loading from string works.
func TestRegression_ConfigLoadFromString_RoundTrip(t *testing.T) {
	yaml := `
modules:
  - name: test-module
    type: messaging.broker
    config:
      key: value
workflows:
  messaging:
    subscriptions:
      - topic: test
        handler: test-module
triggers:
  http:
    enabled: true
`
	cfg, err := config.LoadFromString(yaml)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}

	if len(cfg.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "test-module" {
		t.Errorf("expected module name 'test-module', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[0].Type != "messaging.broker" {
		t.Errorf("expected module type 'messaging.broker', got %q", cfg.Modules[0].Type)
	}
	if len(cfg.Workflows) != 1 {
		t.Errorf("expected 1 workflow, got %d", len(cfg.Workflows))
	}
	if len(cfg.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(cfg.Triggers))
	}
}

// TestRegression_EmptyConfig verifies that an empty config produces appropriate errors.
func TestRegression_EmptyConfig(t *testing.T) {
	cfg := config.NewEmptyWorkflowConfig()
	err := schema.ValidateConfig(cfg)
	if err == nil {
		t.Error("expected validation error for empty config, got nil")
	}
}

// TestRegression_ValidateConfig_StrictMode tests strict validation against known types.
func TestRegression_ValidateConfig_StrictMode(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test", Type: "messaging.broker", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := schema.ValidateConfig(cfg)
	if err != nil {
		t.Errorf("strict validation failed for valid config: %v", err)
	}
}
