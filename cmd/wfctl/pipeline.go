package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
)

func runPipeline(args []string) error {
	if len(args) < 1 {
		return pipelineUsage()
	}
	switch args[0] {
	case "list":
		return runPipelineList(args[1:])
	case "run":
		return runPipelineRun(args[1:])
	default:
		return pipelineUsage()
	}
}

func pipelineUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl pipeline <subcommand> [options]

Subcommands:
  list   List available pipelines in a config file
  run    Execute a pipeline from a config file
`)
	return fmt.Errorf("pipeline subcommand is required")
}

// runPipelineList lists all pipelines defined in a config file.
func runPipelineList(args []string) error {
	fs := flag.NewFlagSet("pipeline list", flag.ContinueOnError)
	configPath := fs.String("c", "", "Path to workflow config YAML file (required)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl pipeline list -c <config.yaml>\n\nList available pipelines in a config file.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		fs.Usage()
		return fmt.Errorf("-c (config file) is required")
	}

	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(cfg.Pipelines) == 0 {
		fmt.Println("No pipelines defined in config.")
		return nil
	}

	// Sort pipeline names for stable output
	names := make([]string, 0, len(cfg.Pipelines))
	for name := range cfg.Pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Printf("Pipelines (%d):\n", len(names))
	for _, name := range names {
		// Extract step count if possible
		stepCount := 0
		if rawCfg, ok := cfg.Pipelines[name].(map[string]any); ok {
			if steps, ok := rawCfg["steps"].([]any); ok {
				stepCount = len(steps)
			}
		}
		if stepCount > 0 {
			fmt.Printf("  %-40s  (%d steps)\n", name, stepCount)
		} else {
			fmt.Printf("  %s\n", name)
		}
	}
	return nil
}

// stringSliceFlag is a flag.Value that accumulates multiple --var key=value flags.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runPipelineRun executes a named pipeline from a config file.
func runPipelineRun(args []string) error {
	fs := flag.NewFlagSet("pipeline run", flag.ContinueOnError)
	configPath := fs.String("c", "", "Path to workflow config YAML file (required)")
	pipelineName := fs.String("p", "", "Name of the pipeline to run (required)")
	inputJSON := fs.String("input", "", "Input data as JSON object")
	verbose := fs.Bool("verbose", false, "Show detailed step output")
	var vars stringSliceFlag
	fs.Var(&vars, "var", "Variable in key=value format (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl pipeline run -c <config.yaml> -p <pipeline-name> [options]

Execute a pipeline locally from a config file.

Examples:
  wfctl pipeline run -c app.yaml -p build-and-deploy
  wfctl pipeline run -c app.yaml -p deploy --var env=staging --var version=1.2.3
  wfctl pipeline run -c app.yaml -p process-data --input '{"items":[1,2,3]}'

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		fs.Usage()
		return fmt.Errorf("-c (config file) is required")
	}
	if *pipelineName == "" {
		fs.Usage()
		return fmt.Errorf("-p (pipeline name) is required")
	}

	// Build initial trigger data from --input JSON
	triggerData := make(map[string]any)
	if *inputJSON != "" {
		if err := json.Unmarshal([]byte(*inputJSON), &triggerData); err != nil {
			return fmt.Errorf("invalid --input JSON: %w", err)
		}
	}

	// Inject --var entries into trigger data
	for _, kv := range vars {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			return fmt.Errorf("invalid --var %q: expected key=value format", kv)
		}
		triggerData[kv[:idx]] = kv[idx+1:]
	}

	// Load config
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Verify the pipeline exists before building the engine
	if _, ok := cfg.Pipelines[*pipelineName]; !ok {
		available := make([]string, 0, len(cfg.Pipelines))
		for name := range cfg.Pipelines {
			available = append(available, name)
		}
		sort.Strings(available)
		if len(available) == 0 {
			return fmt.Errorf("pipeline %q not found (no pipelines defined in config)", *pipelineName)
		}
		return fmt.Errorf("pipeline %q not found; available: %s", *pipelineName, strings.Join(available, ", "))
	}

	// Set up a logger — suppress engine noise unless --verbose
	logLevel := slog.LevelError
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Build a minimal engine that can run pipelines without starting an HTTP server.
	// Strategy: register the pipeline workflow handler and the pipeline-steps plugin,
	// build from config (which wires all step factories and compiles pipelines),
	// then look up the named pipeline from the engine's pipeline registry directly.
	// We deliberately skip engine.Start() so no HTTP servers or triggers are started.
	eng, err := workflow.NewEngineBuilder().
		WithLogger(logger).
		WithHandler(handlers.NewPipelineWorkflowHandler()).
		WithPlugin(pluginpipeline.New()).
		BuildFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to build engine from config: %w", err)
	}

	// Retrieve the compiled pipeline from the engine's registry.
	pipeline, ok := eng.GetPipeline(*pipelineName)
	if !ok {
		return fmt.Errorf("pipeline %q was not compiled by the engine (check config)", *pipelineName)
	}

	// Attach a progress-reporting logger to the pipeline steps
	pipeline.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Print execution header
	fmt.Printf("Pipeline: %s\n", *pipelineName)
	if len(triggerData) > 0 {
		inputBytes, _ := json.Marshal(triggerData)
		fmt.Printf("Input: %s\n", inputBytes)
	}
	fmt.Println()

	totalStart := time.Now()

	// Execute the pipeline, printing step progress inline.
	pc, execErr := executePipelineWithProgress(context.Background(), pipeline, triggerData, *verbose)

	totalElapsed := time.Since(totalStart)

	if execErr != nil {
		fmt.Printf("\nPipeline FAILED in %s\n", totalElapsed.Round(time.Millisecond))
		return execErr
	}

	fmt.Printf("Pipeline completed successfully in %s\n", totalElapsed.Round(time.Millisecond))

	if *verbose && pc != nil && len(pc.Current) > 0 {
		fmt.Println("\nFinal context:")
		for k, v := range pc.Current {
			fmt.Printf("  %s = %v\n", k, v)
		}
	}

	return nil
}

// executePipelineWithProgress wraps pipeline.Execute and prints step-by-step progress to stdout.
// It intercepts step execution by wrapping each step in a progressStep decorator.
func executePipelineWithProgress(ctx context.Context, p *module.Pipeline, triggerData map[string]any, verbose bool) (*module.PipelineContext, error) {
	// Wrap each step with a progress reporter
	original := p.Steps
	wrapped := make([]module.PipelineStep, len(original))
	for i, step := range original {
		wrapped[i] = &progressStep{
			inner:   step,
			index:   i,
			total:   len(original),
			verbose: verbose,
		}
	}
	p.Steps = wrapped
	defer func() { p.Steps = original }()

	return p.Execute(ctx, triggerData)
}

// progressStep wraps a PipelineStep and prints progress before/after execution.
type progressStep struct {
	inner   module.PipelineStep
	index   int
	total   int
	verbose bool
}

func (ps *progressStep) Name() string { return ps.inner.Name() }

func (ps *progressStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	start := time.Now()
	fmt.Printf("Step %d/%d: %s ... ", ps.index+1, ps.total, ps.inner.Name())

	result, err := ps.inner.Execute(ctx, pc)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Printf("FAILED (%s)\n", elapsed.Round(time.Millisecond))
		fmt.Printf("  Error: %v\n", err)
		return result, err
	}

	fmt.Printf("OK (%s)\n", elapsed.Round(time.Millisecond))

	if ps.verbose && result != nil && len(result.Output) > 0 {
		for k, v := range result.Output {
			fmt.Printf("  %s = %v\n", k, v)
		}
	}

	return result, nil
}
