package main

import (
	"fmt"
	"os"
	"time"
)

var version = "dev"

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

func usage() {
	fmt.Fprintf(os.Stderr, `wfctl - Workflow Engine CLI (version %s)

Usage:
  wfctl <command> [options]

Commands:
  init       Scaffold a new workflow project from a template
  validate   Validate a workflow configuration file
  inspect    Inspect modules, workflows, and triggers in a config
  run        Run a workflow engine from a config file
  plugin     Plugin management (init, docs, search, install, list, update, remove)
  pipeline   Pipeline management (list, run)
  schema     Generate JSON Schema for workflow configs
  snippets   Export IDE snippets (--format vscode|jetbrains|json)
  manifest   Analyze config and report infrastructure requirements
  migrate    Manage database schema migrations
  build-ui   Build the application UI (npm install + npm run build + validate)
  ui         UI tooling (scaffold: generate Vite+React+TypeScript SPA from OpenAPI spec)
  publish    Prepare and publish a plugin manifest to the workflow-registry
  deploy     Deploy the workflow application (docker, kubernetes, cloud)
  api        API tooling (extract: generate OpenAPI 3.0 spec from config)
  diff       Compare two workflow config files and show what changed
  template   Template management (validate: check templates against known types)
  contract   Contract testing (test: generate/compare API contracts)
  compat     Compatibility checking (check: verify config works with current engine)
  generate   Code generation (github-actions: generate CI/CD workflows from config)
  git        Git integration (connect: link to GitHub repo, push: commit and push)
  registry   Registry management (list, add, remove plugin registry sources)
  update     Update wfctl to the latest version (use --check to only check)
  mcp        Start the MCP server over stdio for AI assistant integration

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

	// Start the update check in the background before running the command so
	// that it runs concurrently. For long-running commands (mcp, run) we skip
	// it entirely. After the command finishes we wait briefly for the result.
	var updateNoticeDone <-chan struct{}
	if cmd != "mcp" && cmd != "run" {
		updateNoticeDone = checkForUpdateNotice()
	}

	if err := fn(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) //nolint:gosec // G705: CLI error output
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
