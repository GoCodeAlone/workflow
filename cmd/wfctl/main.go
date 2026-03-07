package main

import (
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
)

// wfctlConfigBytes is the embedded workflow config that defines wfctl's CLI
// structure. The CLIWorkflowHandler reads this at startup to configure command
// dispatch and build the usage message from the declarative YAML definition.
//
//go:embed wfctl.yaml
var wfctlConfigBytes []byte

var version = "dev"

// commands maps each CLI command name to its Go implementation. The set of
// recognised commands and their descriptions are declared in wfctl.yaml; this
// map provides the runtime implementations that are wired to those declarations.
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

// buildCLIHandler loads the embedded wfctl.yaml config, injects the build-time
// version, and returns a configured CLIWorkflowHandler with all command runners
// registered. This wires the declarative YAML definition to Go implementations.
func buildCLIHandler() (*handlers.CLIWorkflowHandler, error) {
	cfg, err := config.LoadFromBytes(wfctlConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded config: %w", err)
	}

	cliHandler := handlers.NewCLIWorkflowHandler()
	cliHandler.SetOutput(os.Stderr)

	// Inject the build-time version into the config map before passing it to
	// ConfigureWorkflow so that --version and usage output the correct value.
	if wfCfg, ok := cfg.Workflows["cli"].(map[string]any); ok {
		wfCfg["version"] = version
		if err := cliHandler.ConfigureWorkflow(nil, wfCfg); err != nil {
			return nil, fmt.Errorf("failed to configure CLI handler: %w", err)
		}
	}

	// Register all command runners with the handler.
	for name, fn := range commands {
		cliHandler.RegisterCommand(name, fn)
	}

	return cliHandler, nil
}

func main() {
	cliHandler, err := buildCLIHandler()
	if err != nil {
		fmt.Fprintf(os.Stderr, "internal error: %v\n", err) //nolint:gosec // G705
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		// No subcommand: print usage and exit non-zero.
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
