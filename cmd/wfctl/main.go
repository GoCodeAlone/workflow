package main

import (
	"fmt"
	"os"
)

var version = "dev"

var commands = map[string]func([]string) error{
	"init":     runInit,
	"validate": runValidate,
	"inspect":  runInspect,
	"run":      runRun,
	"plugin":   runPlugin,
	"schema":   runSchema,
	"manifest": runManifest,
	"migrate":  runMigrate,
	"build-ui": runBuildUI,
	"publish":  runPublish,
}

func usage() {
	fmt.Fprintf(os.Stderr, `wfctl - Workflow Engine CLI (version %s)

Usage:
  wfctl <command> [options]

Commands:
  init       Scaffold a new workflow project from a template
  validate   Validate a workflow configuration file
  inspect    Inspect modules, workflows, and triggers in a config
  run        Run a workflow engine from a config file
  plugin     Plugin management (init, docs)
  schema     Generate JSON Schema for workflow configs
  manifest   Analyze config and report infrastructure requirements
  migrate    Manage database schema migrations
  build-ui   Build the application UI (npm install + npm run build + validate)
  publish    Prepare and publish a plugin manifest to the workflow-registry

Run 'wfctl <command> -h' for command-specific help.
`, version)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage()
		os.Exit(0)
	}
	if cmd == "-v" || cmd == "--version" || cmd == "version" {
		fmt.Println(version)
		os.Exit(0)
	}

	fn, ok := commands[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd) //nolint:gosec // G705: CLI error output
		usage()
		os.Exit(1)
	}

	if err := fn(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) //nolint:gosec // G705: CLI error output
		os.Exit(1)
	}
}
