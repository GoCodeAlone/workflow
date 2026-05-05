package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
)

// runRegistry is the new container-registry dispatcher for `wfctl registry`.
// Routes login|push|prune|logout to the appropriate sub-handler.
// The old plugin-catalog registry is now at `wfctl plugin-registry`.
func runRegistry(args []string) error {
	if len(args) < 1 || (len(args) > 0 && args[0] == "--help") {
		return registryContainerUsage()
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "login":
		return runRegistryLogin(rest)
	case "push":
		return runRegistryPush(rest)
	case "prune":
		return runRegistryPrune(rest)
	case "logout":
		return runRegistryLogout(rest)
	default:
		fmt.Fprintf(os.Stderr, "wfctl registry: unknown subcommand %q\n", sub)
		_ = registryContainerUsage()
		return fmt.Errorf("unknown registry subcommand %q — valid: login, push, prune, logout", sub)
	}
}

func registryContainerUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl registry <subcommand> [options]

Manage container registries declared in ci.registries[].

Subcommands:
  login   Authenticate to a container registry
  push    Push an image to a declared registry
  prune   Garbage-collect and prune old tags
  logout  Remove stored registry credentials

Options:
  --config <file>   Config file (default: workflow.yaml)
  --registry <name> Registry name from ci.registries[] (default: all)

Use 'wfctl plugin-registry' for plugin catalog management.
`)
	return fmt.Errorf("missing or unknown subcommand")
}

// runRegistryPrune garbage-collects and prunes old tags for all registries (or one named by --registry).
func runRegistryPrune(args []string) error {
	fs := flag.NewFlagSet("registry prune", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	regName := fs.String("registry", "", "Prune this registry only (default: all)")
	dryRun := fs.Bool("dry-run", false, "Print planned commands without executing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	regs := resolveRegistries(cfg, *regName)
	if *regName != "" && len(regs) == 0 {
		return fmt.Errorf("no registry named %q found in config", *regName)
	}

	ctx := registry.NewContext(context.Background(), os.Stdout, *dryRun)
	for _, reg := range regs {
		provider, ok := registry.Get(reg.Type)
		if !ok {
			return fmt.Errorf("no provider registered for registry type %q (registry: %s)", reg.Type, reg.Name)
		}
		if err := provider.Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
			return fmt.Errorf("prune %s: %w", reg.Name, err)
		}
	}
	return nil
}

// runRegistryLogout removes stored credentials for all registries (or one named by --registry).
func runRegistryLogout(args []string) error {
	fs := flag.NewFlagSet("registry logout", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	regName := fs.String("registry", "", "Logout from this registry only (default: all)")
	dryRun := fs.Bool("dry-run", false, "Print planned commands without executing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	regs := resolveRegistries(cfg, *regName)
	if *regName != "" && len(regs) == 0 {
		return fmt.Errorf("no registry named %q found in config", *regName)
	}

	ctx := registry.NewContext(context.Background(), os.Stdout, *dryRun)
	for _, reg := range regs {
		provider, ok := registry.Get(reg.Type)
		if !ok {
			return fmt.Errorf("no provider registered for registry type %q (registry: %s)", reg.Type, reg.Name)
		}
		if err := provider.Logout(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
			return fmt.Errorf("logout %s: %w", reg.Name, err)
		}
	}
	return nil
}
