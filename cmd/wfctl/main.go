package main

import (
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	workflow "github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

// wfctlConfigBytes is the embedded workflow config that declares wfctl's CLI
// structure and maps every command to a pipeline triggered via the "cli"
// trigger type. The engine resolves these pipelines at startup so each command
// flows through the workflow engine as a proper workflow primitive.
//
//go:embed wfctl.yaml
var wfctlConfigBytes []byte

var version = "dev"

// commands maps each CLI command name to its Go implementation. The command
// metadata (name, description) is declared in wfctl.yaml; this map provides
// the runtime functions that are registered in the CLICommandRegistry service
// and invoked by step.cli_invoke from within each command's pipeline.
var commands = map[string]func([]string) error{
	"init":     runInit,
	"validate": runValidate,
	"inspect":  runInspect,
	"run":      runRun,
	"plugin":   runPlugin,
	"pipeline": runPipeline,
	"schema":   runSchema,
	"snippets": runSnippets,
	"manifest": runManifest,
	"migrate":  runMigrate,
	"build-ui": runBuildUI,
	"ui":       runUI,
	"publish":  runPublish,
	"deploy":   runDeploy,
	"api":      runAPI,
	"diff":     runDiff,
	"template": runTemplate,
	"contract": runContract,
	"compat":   runCompat,
	"generate": runGenerate,
	"git":      runGit,
	"registry": runRegistry,
	"update":   runUpdate,
	"mcp":      runMCP,
}

func main() {
	// Load the embedded config. All command definitions and pipeline wiring
	// live in wfctl.yaml — no hardcoded routing in this file.
	cfg, err := config.LoadFromBytes(wfctlConfigBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "internal error: failed to load embedded config: %v\n", err) //nolint:gosec // G705
		os.Exit(1)
	}

	// Inject the build-time version into the cli workflow config map so that
	// --version and the usage header display the correct release string.
	if wfCfg, ok := cfg.Workflows["cli"].(map[string]any); ok {
		wfCfg["version"] = version
	}

	// Build the engine with all default handlers and triggers.
	// Engine startup logs are suppressed (discarded) — wfctl is a CLI tool
	// and should only emit output from the command itself.
	engineLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Suppress pipeline execution logs globally: wfctl is a CLI tool and
	// internal pipeline step/run logs should not leak to the user's terminal.
	// Each command creates its own logger when it needs output.
	slog.SetDefault(engineLogger)
	engineInst, err := workflow.NewEngineBuilder().
		WithLogger(engineLogger).
		WithAllDefaults().
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "internal error: failed to build engine: %v\n", err) //nolint:gosec // G705
		os.Exit(1)
	}

	// Register all Go command implementations in the CLICommandRegistry service
	// before BuildFromConfig so that step.cli_invoke can look them up at
	// pipeline execution time (service is resolved lazily on each Execute call).
	registry := module.NewCLICommandRegistry()
	for name, fn := range commands {
		registry.Register(name, module.CLICommandFunc(fn))
	}
	if err := engineInst.App().RegisterService(module.CLICommandRegistryServiceName, registry); err != nil {
		fmt.Fprintf(os.Stderr, "internal error: failed to register command registry: %v\n", err) //nolint:gosec // G705
		os.Exit(1)
	}

	// Register the CLI-specific step types on the engine's step registry.
	// step.cli_invoke calls a Go function by name from CLICommandRegistry.
	// step.cli_print writes a template-resolved message to stdout/stderr.
	// These are registered here rather than via the pipelinesteps plugin to
	// keep wfctl lean — only what the binary actually needs is loaded.
	engineInst.AddStepType("step.cli_invoke", module.NewCLIInvokeStepFactory())
	engineInst.AddStepType("step.cli_print", module.NewCLIPrintStepFactory())

	// BuildFromConfig wires the engine from wfctl.yaml:
	//   1. CLIWorkflowHandler is configured from workflows.cli (registers itself
	//      as "cliWorkflowHandler" in the app service registry).
	//   2. Each cmd-* pipeline is created and registered.
	//   3. CLITrigger is configured once per pipeline (via the "cli" inline
	//      trigger), accumulating command→pipeline mappings.
	if err := engineInst.BuildFromConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "internal error: failed to configure engine: %v\n", err) //nolint:gosec // G705
		os.Exit(1)
	}

	// Retrieve the CLIWorkflowHandler that registered itself during BuildFromConfig.
	var cliHandler *handlers.CLIWorkflowHandler
	if err := engineInst.App().GetService(handlers.CLIWorkflowHandlerServiceName, &cliHandler); err != nil || cliHandler == nil {
		fmt.Fprintf(os.Stderr, "internal error: CLIWorkflowHandler not found in service registry\n") //nolint:gosec // G705
		os.Exit(1)
	}
	// Error/usage output goes to stderr; command output goes to stdout.
	cliHandler.SetOutput(os.Stderr)

	if len(os.Args) < 2 {
		// No subcommand — print usage and exit non-zero.
		_ = cliHandler.Dispatch([]string{"-h"})
		os.Exit(1)
	}

	cmd := os.Args[1]

	// Start the update check in the background before running the command so
	// that it runs concurrently. For long-running commands (mcp, run) we skip
	// it entirely. After the command finishes we wait briefly for the result.
	var updateNoticeDone <-chan struct{}
	if cmd != "mcp" && cmd != "run" {
		updateNoticeDone = checkForUpdateNotice()
	}

	if err := cliHandler.Dispatch(os.Args[1:]); err != nil {
		// The handler already printed routing errors (unknown/missing command).
		// Only emit the "error:" prefix for actual command execution failures.
		if _, isKnown := commands[cmd]; isKnown {
			fmt.Fprintf(os.Stderr, "error: %v\n", err) //nolint:gosec // G705
		}
		os.Exit(1)
	}

	// Wait briefly for the update notice after the command completes.
	// A 1-second ceiling ensures we never meaningfully delay the shell prompt.
	if updateNoticeDone != nil {
		select {
		case <-updateNoticeDone:
		case <-time.After(time.Second):
		}
	}
}
