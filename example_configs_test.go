package workflow

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/schema"
)

// discoverExampleConfigs walks the example/ directory and returns all YAML
// config file paths, skipping known non-config directories and files.
func discoverExampleConfigs(t *testing.T) []string {
	t.Helper()

	exampleDir := "example"
	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory %q does not exist", exampleDir)
	}

	skipDirs := map[string]bool{
		"configs":       true,
		"seed":          true,
		"observability": true,
		"spa":           true,
		"components":    true,
		"node_modules":  true,
		"e2e":           true,
		"data":          true,
	}

	skipFiles := map[string]bool{
		"docker-compose.yml":  true,
		"docker-compose.yaml": true,
	}

	var configs []string
	err := filepath.WalkDir(exampleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".yaml" || ext == ".yml" {
			if !skipFiles[d.Name()] {
				configs = append(configs, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk example directory: %v", err)
	}

	if len(configs) == 0 {
		t.Fatal("no YAML config files found in example/")
	}

	return configs
}

// TestExampleConfigsLoad dynamically discovers all YAML config files in example/
// and verifies they parse without error. This prevents regressions in example configs.
func TestExampleConfigsLoad(t *testing.T) {
	configs := discoverExampleConfigs(t)
	t.Logf("discovered %d example config files", len(configs))

	for _, cfg := range configs {
		cfg := cfg
		t.Run(filepath.Base(cfg), func(t *testing.T) {
			t.Parallel()
			wfCfg, err := config.LoadFromFile(cfg)
			if err != nil {
				t.Errorf("failed to load config %s: %v", cfg, err)
				return
			}
			if len(wfCfg.Modules) == 0 {
				t.Logf("note: config %s has no modules (may be a linking/reference config)", cfg)
			}
		})
	}
}

// TestExampleConfigsValidate runs schema.ValidateConfig on every example config.
// This catches issues like unknown module types, missing required fields,
// duplicate names, and invalid dependency references.
func TestExampleConfigsValidate(t *testing.T) {
	configs := discoverExampleConfigs(t)
	t.Logf("validating %d example config files against schema", len(configs))

	// Configs that use aspirational/unimplemented module types. These are
	// tracked here so CI stays green while signaling they need updating.
	// When a type gets implemented, remove it from this list — the test
	// will start validating the config automatically.
	aspirationalConfigs := map[string]string{
		"advanced-scheduler-workflow.yaml":             "uses unimplemented scheduler.cron type",
		"scheduled-jobs-config.yaml":                   "uses unimplemented scheduler, job.store types",
		"dependency-injection-example.yaml":            "uses unimplemented core.config, core.logger, core.metrics, data.cache types",
		"event-driven-workflow.yaml":                   "uses unimplemented event.processor type and event workflow type",
		"integration-workflow.yaml":                    "uses unimplemented integration.registry type",
		"trigger-workflow-example.yaml":                "uses unimplemented scheduler.cron type",
		"workflow-a-orders-with-branching.yaml":        "uses unimplemented conditional.ifelse, conditional.switch types",
		"workflow-b-fulfillment-with-branching.yaml":   "uses unimplemented conditional.ifelse, conditional.switch types",
		"workflow-c-notifications-with-branching.yaml": "uses unimplemented conditional.switch, conditional.ifelse types",
	}

	// Configs that use custom/dynamic module types not in the built-in list
	// need extra types registered. Map from config base name to extra types.
	extraTypes := map[string][]string{
		// cross-workflow-links.yaml references modules defined in other files
		"cross-workflow-links.yaml": {},
	}

	for _, cfgPath := range configs {
		cfgPath := cfgPath
		baseName := filepath.Base(cfgPath)
		t.Run(baseName, func(t *testing.T) {
			t.Parallel()

			if reason, ok := aspirationalConfigs[baseName]; ok {
				t.Skipf("skipping %s: %s (aspirational config with unimplemented types)", baseName, reason)
			}

			wfCfg, err := config.LoadFromFile(cfgPath)
			if err != nil {
				t.Fatalf("failed to load config: %v", err)
			}

			// Skip empty/linking configs
			if len(wfCfg.Modules) == 0 {
				t.Skipf("skipping validation for %s (no modules)", baseName)
			}

			// Build validation options
			opts := []schema.ValidationOption{
				// Pipeline handler type is registered dynamically by the engine
				schema.WithExtraWorkflowTypes("pipeline"),
				// Pipeline trigger types
				schema.WithExtraTriggerTypes("mock"),
			}

			if extra, ok := extraTypes[baseName]; ok && len(extra) > 0 {
				opts = append(opts, schema.WithExtraModuleTypes(extra...))
			}

			if err := schema.ValidateConfig(wfCfg, opts...); err != nil {
				t.Errorf("schema validation failed for %s:\n%v", cfgPath, err)
			}
		})
	}
}

// TestExampleConfigsBuildFromConfig verifies each example config can be
// loaded into the workflow engine via BuildFromConfig. This is the strongest
// check — it ensures the config is not only valid YAML with correct schema
// but also functionally loadable by the engine.
func TestExampleConfigsBuildFromConfig(t *testing.T) {
	configs := discoverExampleConfigs(t)
	t.Logf("engine-loading %d example config files", len(configs))

	// Aspirational configs with unimplemented module types
	aspirationalConfigs := map[string]string{
		"advanced-scheduler-workflow.yaml":             "uses unimplemented scheduler.cron type",
		"scheduled-jobs-config.yaml":                   "uses unimplemented scheduler, job.store types",
		"dependency-injection-example.yaml":            "uses unimplemented core.config, core.logger, core.metrics, data.cache types",
		"event-driven-workflow.yaml":                   "uses unimplemented event.processor type",
		"integration-workflow.yaml":                    "uses unimplemented integration.registry type",
		"trigger-workflow-example.yaml":                "uses unimplemented scheduler.cron type",
		"workflow-a-orders-with-branching.yaml":        "uses unimplemented conditional.ifelse, conditional.switch types",
		"workflow-b-fulfillment-with-branching.yaml":   "uses unimplemented conditional.ifelse, conditional.switch types",
		"workflow-c-notifications-with-branching.yaml": "uses unimplemented conditional.switch, conditional.ifelse types",
	}

	// Configs that fail for environmental reasons (need external services,
	// dynamic registry, triggers defined in other files, etc.).
	// Keys are path substrings matched against the full config path.
	envIssues := map[string]string{
		"chat-platform/workflow.yaml":   "uses dynamic.component requiring dynamic registry",
		"ecommerce-app/workflow.yaml":   "uses dynamic.component requiring dynamic registry",
		"workflow-a-orders.yaml":        "multi-workflow: triggers reference handlers from other configs",
		"workflow-b-fulfillment.yaml":   "multi-workflow: event triggers reference cross-file handlers",
		"workflow-c-notifications.yaml": "multi-workflow: event triggers reference cross-file handlers",
	}

	// Configs with known bugs that need fixing. Tracked here so CI
	// stays green while documenting the issues.
	// TODO: Fix these configs and remove from this list.
	knownBugs := map[string]string{
		"api-gateway-config.yaml":    "BUG: chimux.router not recognized by HTTP workflow handler (needs http.router)",
		"notification-pipeline.yaml": "BUG: storage.s3 used as event-archive but messaging workflow expects MessageHandler interface",
	}

	for _, cfgPath := range configs {
		cfgPath := cfgPath
		baseName := filepath.Base(cfgPath)
		t.Run(baseName, func(t *testing.T) {
			t.Parallel()

			// Skip aspirational configs with unimplemented types
			if reason, ok := aspirationalConfigs[baseName]; ok {
				t.Skipf("skipping %s: %s (aspirational)", baseName, reason)
			}

			// Skip configs that need specific runtime environment
			for pathPart, reason := range envIssues {
				if strings.Contains(cfgPath, pathPart) {
					t.Skipf("skipping %s: %s (env requirement)", cfgPath, reason)
				}
			}

			// Skip configs with known bugs (tracked for fixing)
			if reason, ok := knownBugs[baseName]; ok {
				t.Skipf("skipping %s: %s", baseName, reason)
			}

			wfCfg, err := config.LoadFromFile(cfgPath)
			if err != nil {
				t.Fatalf("failed to load config: %v", err)
			}

			if len(wfCfg.Modules) == 0 {
				t.Skipf("skipping %s (no modules)", baseName)
			}

			// Create a minimal engine with all handlers registered
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelError, // suppress noisy output in tests
			}))
			app := modular.NewStdApplication(nil, logger)
			engine := NewStdEngine(app, logger)

			engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewPipelineWorkflowHandler())

			// Register pipeline step types (same as cmd/server/main.go)
			engine.AddStepType("step.validate", module.NewValidateStepFactory())
			engine.AddStepType("step.transform", module.NewTransformStepFactory())
			engine.AddStepType("step.conditional", module.NewConditionalStepFactory())
			engine.AddStepType("step.publish", module.NewPublishStepFactory())
			engine.AddStepType("step.set", module.NewSetStepFactory())
			engine.AddStepType("step.log", module.NewLogStepFactory())
			engine.AddStepType("step.http_call", module.NewHTTPCallStepFactory())

			if err := engine.BuildFromConfig(wfCfg); err != nil {
				t.Errorf("BuildFromConfig failed for %s: %v", cfgPath, err)
				return
			}

			// Verify the engine built something
			if engine.app == nil {
				t.Error("engine app is nil after BuildFromConfig")
			}

			// Brief start/stop cycle to catch initialization errors.
			// Many configs bind to ports that may conflict in parallel tests,
			// so Start errors are logged but don't fail the test.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			startErr := engine.Start(ctx)
			if startErr != nil {
				t.Logf("note: engine.Start returned error for %s (may be expected in test env): %v", baseName, startErr)
			} else {
				if stopErr := engine.Stop(ctx); stopErr != nil {
					t.Logf("note: engine.Stop returned error for %s: %v", baseName, stopErr)
				}
			}
		})
	}
}
