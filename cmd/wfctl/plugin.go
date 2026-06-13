package main

import (
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/sdk"
)

func runPlugin(args []string) error {
	if len(args) < 1 {
		return pluginUsage()
	}
	if args[0] == "-h" || args[0] == "--help" {
		printPluginUsage()
		return nil
	}
	subArgs := args[1:]
	switch args[0] {
	case "init":
		return runPluginInit(subArgs)
	case "docs":
		return runPluginDocs(subArgs)
	case "test":
		return runPluginTest(subArgs)
	case "search":
		return runPluginSearch(subArgs)
	case "add":
		return runPluginAdd(subArgs)
	case "lock":
		return runPluginLock(subArgs)
	case "install":
		return runPluginInstall(subArgs)
	case "run":
		return runPluginRun(subArgs)
	case "list":
		return runPluginList(subArgs)
	case "update":
		return runPluginUpdate(subArgs)
	case "remove":
		return runPluginRemove(subArgs)
	case "validate":
		return runPluginValidate(subArgs)
	case "audit":
		return runPluginAudit(subArgs)
	case "validate-contract":
		return runPluginValidateContract(subArgs)
	case "release-workflow":
		return runPluginReleaseWorkflow(subArgs)
	case "verify-capabilities":
		return runPluginVerifyCapabilities(subArgs)
	case "registry-sync":
		return runPluginRegistrySync(subArgs)
	case "conformance":
		return runPluginConformance(subArgs)
	case "info":
		return runPluginInfo(subArgs)
	case "deps":
		return runPluginDeps(subArgs)
	case "marketplace-verify":
		return runPluginMarketplaceVerify(subArgs)
	default:
		return pluginUsage()
	}
}

func pluginUsage() error {
	printPluginUsage()
	return fmt.Errorf("plugin subcommand is required")
}

func printPluginUsage() {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl plugin <subcommand> [options]

Subcommands:
  add      Add a plugin to wfctl.yaml manifest
  lock     Regenerate .wfctl-lock.yaml from wfctl.yaml manifest
  init     Scaffold a new plugin project
  docs     Generate documentation for an existing plugin
  test     Run a plugin through its full lifecycle in a test harness
  search   Search the plugin registry
  install  Install plugins (reads .wfctl-lock.yaml when present)
  run      Install if requested, then run a plugin-provided wfctl command
  list     List installed plugins
  update   Update an installed plugin to its latest version
  remove   Uninstall a plugin (also removes from manifest + lockfile)
  validate Validate a plugin manifest from the registry or a local file
  audit    Audit a single plugin source directory
  validate-contract  Validate a plugin source directory against the release contract (workflow#758)
  release-workflow  Audit/fix plugin release workflow wfctl installation
  verify-capabilities  Spawn plugin binary, verify runtime GetManifest matches plugin.json
  registry-sync  Sync registry manifest versions/capabilities from upstream release tags; subcommands: core, readme (workflow#762)
  conformance Run executable plugin/host conformance checks
  info     Show details about an installed plugin
  deps     List dependencies for a plugin
  marketplace-verify  Scan a GitHub org's wfctl.yaml files for plugin usage; suggests manifest status (verified | experimental)

Use -plugin-dir to specify a custom plugin directory (replaces deprecated -data-dir).
`)
}

func runPluginInit(args []string) error {
	if err := checkTrailingFlags(args); err != nil {
		return err
	}
	fs := flag.NewFlagSet("plugin init", flag.ExitOnError)
	author := fs.String("author", "", "Plugin author (required)")
	ver := fs.String("version", "0.0.0", "Deprecated; release versions are injected from Git tags")
	desc := fs.String("description", "", "Plugin description")
	license := fs.String("license", "", "Plugin license")
	output := fs.String("output", "", "Output directory (defaults to plugin name)")
	withContract := fs.Bool("contract", false, "Include a contract skeleton")
	legacyContracts := fs.Bool("legacy-contracts", false, "Scaffold legacy map-based plugin contracts instead of strict typed contracts")
	module := fs.String("module", "", "Go module path (default: github.com/<author>/workflow-plugin-<name>)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin init [options] <name>\n\nScaffold a new plugin project.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}
	if *author == "" {
		return fmt.Errorf("-author is required")
	}

	name := fs.Arg(0)
	gen := sdk.NewTemplateGenerator()
	opts := sdk.GenerateOptions{
		Name:            name,
		Version:         *ver,
		Author:          *author,
		Description:     *desc,
		License:         *license,
		OutputDir:       *output,
		WithContract:    *withContract,
		LegacyContracts: *legacyContracts,
		GoModule:        *module,
		WorkflowReplace: sdk.DiscoverWorkflowModuleRoot("."),
	}
	if err := gen.Generate(opts); err != nil {
		return err
	}

	outDir := opts.OutputDir
	if outDir == "" {
		outDir = name
	}
	fmt.Printf("Plugin %q scaffolded in %s/\n", name, outDir)
	return nil
}

func runPluginDocs(args []string) error {
	fs := flag.NewFlagSet("plugin docs", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin docs <plugin-dir>\n\nGenerate markdown documentation for a plugin.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin directory is required")
	}

	dir := fs.Arg(0)
	manifest, err := plugin.LoadManifest(dir + "/plugin.json")
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	docGen := sdk.NewDocGenerator()
	doc := docGen.GeneratePluginDoc(manifest)
	fmt.Print(doc)
	return nil
}
