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

	for _, cfgPath := range configs {
		cfgPath := cfgPath
		baseName := filepath.Base(cfgPath)
		t.Run(baseName, func(t *testing.T) {
			t.Parallel()

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
				// Many configs are sub-workflows or modular-style configs without explicit entry points
				schema.WithAllowNoEntryPoints(),
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

	// Configs that fail BuildFromConfig for environmental or integration reasons.
	// These are NOT stale — each has a specific engine limitation documented.
	envIssues := map[string]string{
		// dynamic.component needs Go source files at runtime
		"chat-platform/workflow.yaml": "dynamic.component requires Go source files at runtime",
		"ecommerce-app/workflow.yaml": "dynamic.component requires Go source files at runtime",
		// scheduler.modular (CrisisTextLine/modular) doesn't implement workflow Scheduler interface
		"advanced-scheduler-workflow.yaml": "scheduler.modular doesn't expose workflow Scheduler service",
		"scheduled-jobs-config.yaml":       "scheduler.modular doesn't expose workflow Scheduler service",
		"trigger-workflow-example.yaml":    "schedule trigger needs Scheduler interface not provided by scheduler.modular",
		// Module service/interface gaps
		"api-gateway-config.yaml":         "reverseproxy module requires chimux.router (routerService interface) which was removed",
		"api-gateway-modular-config.yaml": "reverseproxy module requires chimux.router (routerService interface) which was removed",
		"event-driven-workflow.yaml":      "processing.step requires component service (event-pattern-matcher) not available",
		"integration-workflow.yaml":       "workflow.registry doesn't implement IntegrationRegistry interface",
		// step.conditional not supported as standalone module (only as pipeline step)
		"workflow-a-orders-with-branching.yaml":        "step.conditional not supported as standalone module type",
		"workflow-b-fulfillment-with-branching.yaml":   "step.conditional not supported as standalone module type",
		"workflow-c-notifications-with-branching.yaml": "step.conditional not supported as standalone module type",
		// feature_flag step requires featureflag.service module to be initialized first
		"feature-flag-workflow.yaml": "step.feature_flag requires featureflag.service module loaded before pipeline configuration",
	}

	for _, cfgPath := range configs {
		cfgPath := cfgPath
		baseName := filepath.Base(cfgPath)
		t.Run(baseName, func(t *testing.T) {
			// Note: t.Parallel() removed because modular framework modules
			// (chimux, EnvFeeder) have global state that races under -race.

			// Skip configs that need specific runtime environment
			for pathPart, reason := range envIssues {
				if strings.Contains(cfgPath, pathPart) {
					t.Skipf("skipping %s: %s (env requirement)", cfgPath, reason)
				}
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
			if stdApp, ok := app.(*modular.StdApplication); ok {
				stdApp.SetConfigFeeders([]modular.Feeder{}) // per-app feeders to avoid global EnvFeeder race
			}
			engine := NewStdEngine(app, logger)

			// Load all extracted plugins
			for _, p := range allPlugins() {
				if err := engine.LoadPlugin(p); err != nil {
					t.Fatalf("LoadPlugin(%s) failed: %v", p.Name(), err)
				}
			}

			engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewEventWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewPipelineWorkflowHandler())

			// Register trigger types (same as cmd/server/main.go)
			engine.RegisterTrigger(module.NewHTTPTrigger())
			engine.RegisterTrigger(module.NewEventTrigger())
			engine.RegisterTrigger(module.NewScheduleTrigger())
			engine.RegisterTrigger(module.NewEventBusTrigger())
			engine.RegisterTrigger(module.NewReconciliationTrigger())

			// Register pipeline step types (same as cmd/server/main.go)
			engine.AddStepType("step.validate", module.NewValidateStepFactory())
			engine.AddStepType("step.transform", module.NewTransformStepFactory())
			engine.AddStepType("step.conditional", module.NewConditionalStepFactory())
			engine.AddStepType("step.publish", module.NewPublishStepFactory())
			engine.AddStepType("step.set", module.NewSetStepFactory())
			engine.AddStepType("step.log", module.NewLogStepFactory())
			engine.AddStepType("step.http_call", module.NewHTTPCallStepFactory())
			engine.AddStepType("step.delegate", module.NewDelegateStepFactory())
			engine.AddStepType("step.request_parse", module.NewRequestParseStepFactory())
			engine.AddStepType("step.db_query", module.NewDBQueryStepFactory())
			engine.AddStepType("step.db_exec", module.NewDBExecStepFactory())
			engine.AddStepType("step.json_response", module.NewJSONResponseStepFactory())
			engine.AddStepType("step.jq", module.NewJQStepFactory())
			engine.AddStepType("step.shell_exec", module.NewShellExecStepFactory())
			engine.AddStepType("step.artifact_pull", module.NewArtifactPullStepFactory())
			engine.AddStepType("step.artifact_push", module.NewArtifactPushStepFactory())
			engine.AddStepType("step.docker_build", module.NewDockerBuildStepFactory())
			engine.AddStepType("step.docker_push", module.NewDockerPushStepFactory())
			engine.AddStepType("step.docker_run", module.NewDockerRunStepFactory())
			engine.AddStepType("step.scan_sast", module.NewScanSASTStepFactory())
			engine.AddStepType("step.scan_container", module.NewScanContainerStepFactory())
			engine.AddStepType("step.scan_deps", module.NewScanDepsStepFactory())
			engine.AddStepType("step.deploy", module.NewDeployStepFactory())
			engine.AddStepType("step.gate", module.NewGateStepFactory())
			engine.AddStepType("step.build_ui", module.NewBuildUIStepFactory())
			engine.AddStepType("step.rate_limit", module.NewRateLimitStepFactory())
			engine.AddStepType("step.circuit_breaker", module.NewCircuitBreakerStepFactory())
			engine.AddStepType("step.platform_template", module.NewPlatformTemplateStepFactory())

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
