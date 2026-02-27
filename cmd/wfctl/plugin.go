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
	switch args[0] {
	case "init":
		return runPluginInit(args[1:])
	case "docs":
		return runPluginDocs(args[1:])
	case "test":
		return runPluginTest(args[1:])
	case "search":
		return runPluginSearch(args[1:])
	case "install":
		return runPluginInstall(args[1:])
	case "list":
		return runPluginList(args[1:])
	case "update":
		return runPluginUpdate(args[1:])
	case "remove":
		return runPluginRemove(args[1:])
	case "validate":
		return runPluginValidate(args[1:])
	case "info":
		return runPluginInfo(args[1:])
	default:
		return pluginUsage()
	}
}

func pluginUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl plugin <subcommand> [options]

Subcommands:
  init     Scaffold a new plugin project
  docs     Generate documentation for an existing plugin
  test     Run a plugin through its full lifecycle in a test harness
  search   Search the plugin registry
  install  Install a plugin from the registry
  list     List installed plugins
  update   Update an installed plugin to its latest version
  remove   Uninstall a plugin
  validate Validate a plugin manifest from the registry or a local file
  info     Show details about an installed plugin
`)
	return fmt.Errorf("plugin subcommand is required")
}

func runPluginInit(args []string) error {
	fs := flag.NewFlagSet("plugin init", flag.ExitOnError)
	author := fs.String("author", "", "Plugin author (required)")
	ver := fs.String("version", "0.1.0", "Plugin version")
	desc := fs.String("description", "", "Plugin description")
	license := fs.String("license", "", "Plugin license")
	output := fs.String("output", "", "Output directory (defaults to plugin name)")
	withContract := fs.Bool("contract", false, "Include a contract skeleton")
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
		Name:         name,
		Version:      *ver,
		Author:       *author,
		Description:  *desc,
		License:      *license,
		OutputDir:    *output,
		WithContract: *withContract,
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
