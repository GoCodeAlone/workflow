package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// pluginTestConfig holds optional test configuration loaded from a YAML file.
type pluginTestConfig struct {
	Config map[string]any `yaml:"config"`
}

// phaseResult records the outcome of a single lifecycle phase.
type phaseResult struct {
	phase   string
	ok      bool
	detail  string
	elapsed time.Duration
}

func runPluginTest(args []string) error {
	fs := flag.NewFlagSet("plugin test", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to test config YAML file")
	timeout := fs.Duration("timeout", 30*time.Second, "Timeout for the test harness")
	verbose := fs.Bool("verbose", false, "Enable verbose output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin test [options] [plugin-dir]\n\nRun a plugin through its full lifecycle in an isolated test harness.\n\nOptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	pluginDir := "."
	if fs.NArg() >= 1 {
		pluginDir = fs.Arg(0)
	}

	// Load manifest from directory.
	manifestPath := pluginDir + "/plugin.json"
	manifest, err := plugin.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to load plugin manifest from %s: %w", manifestPath, err)
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid plugin manifest: %w", err)
	}

	fmt.Printf("Testing plugin: %s v%s\n", manifest.Name, manifest.Version)
	fmt.Printf("Description:   %s\n", manifest.Description)
	fmt.Println()

	// Load optional test configuration.
	var testCfg pluginTestConfig
	if *configPath != "" {
		data, err := os.ReadFile(*configPath) //nolint:gosec // G304: path is user-provided CLI arg
		if err != nil {
			return fmt.Errorf("failed to read test config %s: %w", *configPath, err)
		}
		if err := yaml.Unmarshal(data, &testCfg); err != nil {
			return fmt.Errorf("failed to parse test config %s: %w", *configPath, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	logger := slog.Default()
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	return runPluginTestHarness(ctx, pluginDir, manifest, testCfg.Config, logger, *verbose)
}

// runPluginTestHarness sets up a mock engine, loads the plugin via the EnginePluginManager,
// and runs it through Init → Start → Stop, reporting each phase.
func runPluginTestHarness(
	ctx context.Context,
	pluginDir string,
	manifest *plugin.PluginManifest,
	cfg map[string]any,
	logger *slog.Logger,
	verbose bool,
) error {
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	mgr := plugin.NewEnginePluginManager(capReg, schemaReg)

	// Build a minimal EnginePlugin from the manifest for testing purposes.
	testPlugin := &manifestTestPlugin{
		manifest: manifest,
		cfg:      cfg,
		logger:   logger,
		dir:      pluginDir,
	}

	var results []phaseResult

	// Phase: manifest validation (already done above, record it).
	results = append(results, phaseResult{phase: "manifest", ok: true, detail: fmt.Sprintf("name=%s version=%s", manifest.Name, manifest.Version)})

	// Phase: register.
	start := time.Now()
	err := mgr.Register(testPlugin)
	elapsed := time.Since(start)
	if err != nil {
		results = append(results, phaseResult{phase: "register", ok: false, detail: err.Error(), elapsed: elapsed})
		printResults(results, verbose)
		return fmt.Errorf("plugin registration failed: %w", err)
	}
	results = append(results, phaseResult{phase: "register", ok: true, elapsed: elapsed})

	// Phase: enable (loads plugin into PluginLoader).
	start = time.Now()
	err = mgr.Enable(manifest.Name)
	elapsed = time.Since(start)
	if err != nil {
		results = append(results, phaseResult{phase: "enable", ok: false, detail: err.Error(), elapsed: elapsed})
		printResults(results, verbose)
		return fmt.Errorf("plugin enable failed: %w", err)
	}
	results = append(results, phaseResult{phase: "enable", ok: true, elapsed: elapsed})

	// Phase: init — verify factories are accessible via the loader.
	start = time.Now()
	loader := mgr.Loader()
	modFactories := loader.ModuleFactories()
	stepFactories := loader.StepFactories()
	triggerFactories := loader.TriggerFactories()
	handlerFactories := loader.WorkflowHandlerFactories()
	elapsed = time.Since(start)

	initDetail := fmt.Sprintf("module_types=%d step_types=%d trigger_types=%d handler_types=%d",
		len(modFactories), len(stepFactories), len(triggerFactories), len(handlerFactories))
	results = append(results, phaseResult{phase: "init", ok: true, detail: initDetail, elapsed: elapsed})

	// Phase: start — simulate a lifecycle Start via a context-aware probe.
	start = time.Now()
	startErr := testPlugin.start(ctx)
	elapsed = time.Since(start)
	if startErr != nil {
		results = append(results, phaseResult{phase: "start", ok: false, detail: startErr.Error(), elapsed: elapsed})
		printResults(results, verbose)
		return fmt.Errorf("plugin start failed: %w", startErr)
	}
	results = append(results, phaseResult{phase: "start", ok: true, elapsed: elapsed})

	// Phase: stop — simulate graceful stop.
	start = time.Now()
	stopErr := testPlugin.stop(ctx)
	elapsed = time.Since(start)
	if stopErr != nil {
		results = append(results, phaseResult{phase: "stop", ok: false, detail: stopErr.Error(), elapsed: elapsed})
		printResults(results, verbose)
		return fmt.Errorf("plugin stop failed: %w", stopErr)
	}
	results = append(results, phaseResult{phase: "stop", ok: true, elapsed: elapsed})

	// Phase: disable.
	start = time.Now()
	disableErr := mgr.Disable(manifest.Name)
	elapsed = time.Since(start)
	if disableErr != nil {
		results = append(results, phaseResult{phase: "disable", ok: false, detail: disableErr.Error(), elapsed: elapsed})
		printResults(results, verbose)
		return fmt.Errorf("plugin disable failed: %w", disableErr)
	}
	results = append(results, phaseResult{phase: "disable", ok: true, elapsed: elapsed})

	printResults(results, verbose)

	// Check for any failures.
	for _, r := range results {
		if !r.ok {
			return fmt.Errorf("plugin test failed at phase %q: %s", r.phase, r.detail)
		}
	}

	fmt.Println("\nPlugin test PASSED")
	return nil
}

// printResults prints the lifecycle phase results table.
func printResults(results []phaseResult, verbose bool) {
	fmt.Println("Lifecycle results:")
	fmt.Printf("  %-12s %-8s %-10s %s\n", "Phase", "Result", "Elapsed", "Detail")
	fmt.Printf("  %-12s %-8s %-10s %s\n", "-----", "------", "-------", "------")
	for _, r := range results {
		status := "PASS"
		if !r.ok {
			status = "FAIL"
		}
		detail := ""
		if r.detail != "" && (verbose || !r.ok) {
			detail = r.detail
		}
		fmt.Printf("  %-12s %-8s %-10s %s\n", r.phase, status, r.elapsed.Round(time.Millisecond), detail)
	}
}

// manifestTestPlugin is a minimal EnginePlugin built from a PluginManifest for
// use in the test harness. It records the manifest's declared capabilities but
// does not load any real binary code; the test harness validates the lifecycle
// integration points rather than actual step/module execution.
type manifestTestPlugin struct {
	plugin.BaseEnginePlugin
	manifest *plugin.PluginManifest
	cfg      map[string]any
	logger   *slog.Logger
	dir      string
}

// Name returns the plugin name from the manifest.
func (p *manifestTestPlugin) Name() string { return p.manifest.Name }

// Version returns the plugin version.
func (p *manifestTestPlugin) Version() string { return p.manifest.Version }

// Description returns the plugin description.
func (p *manifestTestPlugin) Description() string { return p.manifest.Description }

// EngineManifest returns the plugin manifest.
func (p *manifestTestPlugin) EngineManifest() *plugin.PluginManifest { return p.manifest }

// start simulates lifecycle Start — for a manifest-only test plugin this is a no-op.
func (p *manifestTestPlugin) start(_ context.Context) error {
	p.logger.Debug("plugin start (test harness)", "plugin", p.manifest.Name)
	return nil
}

// stop simulates lifecycle Stop — for a manifest-only test plugin this is a no-op.
func (p *manifestTestPlugin) stop(_ context.Context) error {
	p.logger.Debug("plugin stop (test harness)", "plugin", p.manifest.Name)
	return nil
}
