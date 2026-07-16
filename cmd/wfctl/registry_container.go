package main

import (
	"flag"
	"fmt"
	"io"
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
	return registryContainerUsageTo(os.Stderr)
}

func registryContainerUsageTo(w io.Writer) error {
	fmt.Fprintf(w, `Usage: wfctl registry <subcommand> [options]

Manage container registries declared in ci.registries[].

Subcommands:
  login   Authenticate to a container registry
  push    Push an image to a declared registry
  prune   Garbage-collect and prune old tags
  logout  Remove stored registry credentials

Options:
  --config <file>     Config file (default: workflow.yaml)
  --registry <name>   Registry name from ci.registries[] (default: all)
  --plugin-dir <dir>  Installed provider plugin directory

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
	pluginDir := fs.String("plugin-dir", "", "Directory containing installed provider plugins (default: $WFCTL_PLUGIN_DIR or data/plugins)")
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

	baseCtx, stopProviderCommand := boundedProviderCommandContext(containerRegistryOperationTimeout)
	defer stopProviderCommand()
	ctx := registry.NewContext(baseCtx, os.Stdout, *dryRun)
	requests := make([]containerRegistryOperationRequest, 0, len(regs))
	for _, reg := range regs {
		requests = append(requests, containerRegistryOperationRequest{Registry: reg})
	}
	prepared, err := prepareContainerRegistryCapabilities(ctx, *pluginDir, "prune", requests)
	if err != nil {
		return err
	}
	defer closePreparedContainerRegistryCapabilities(prepared)
	legacyProviders := make([]registry.RegistryProvider, len(regs))
	for index, capability := range prepared {
		if capability.handled {
			continue
		}
		provider, ok := registry.Get(regs[index].Type)
		if !ok {
			return fmt.Errorf("no provider registered for registry type %q (registry: %s)", regs[index].Type, regs[index].Name)
		}
		legacyProviders[index] = provider
	}
	for index, reg := range regs {
		if err := baseCtx.Err(); err != nil {
			return err
		}
		handled, err := executeContainerRegistryCapability(ctx, prepared[index], *dryRun, ctx.Out())
		if err != nil {
			return fmt.Errorf("prune %s: %w", reg.Name, err)
		}
		if handled {
			continue
		}
		if err := legacyProviders[index].Prune(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
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
	pluginDir := fs.String("plugin-dir", "", "Directory containing installed provider plugins (default: $WFCTL_PLUGIN_DIR or data/plugins)")
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

	baseCtx, stopProviderCommand := boundedProviderCommandContext(containerRegistryOperationTimeout)
	defer stopProviderCommand()
	ctx := registry.NewContext(baseCtx, os.Stdout, *dryRun)
	requests := make([]containerRegistryOperationRequest, 0, len(regs))
	for _, reg := range regs {
		requests = append(requests, containerRegistryOperationRequest{Registry: reg})
	}
	prepared, err := prepareContainerRegistryCapabilities(ctx, *pluginDir, "logout", requests)
	if err != nil {
		return err
	}
	defer closePreparedContainerRegistryCapabilities(prepared)
	legacyProviders := make([]registry.RegistryProvider, len(regs))
	for index, capability := range prepared {
		if capability.handled {
			continue
		}
		provider, ok := registry.Get(regs[index].Type)
		if !ok {
			return fmt.Errorf("no provider registered for registry type %q (registry: %s)", regs[index].Type, regs[index].Name)
		}
		legacyProviders[index] = provider
	}
	for index, reg := range regs {
		if err := baseCtx.Err(); err != nil {
			return err
		}
		handled, err := executeContainerRegistryCapability(ctx, prepared[index], *dryRun, ctx.Out())
		if err != nil {
			return fmt.Errorf("logout %s: %w", reg.Name, err)
		}
		if handled {
			continue
		}
		if err := legacyProviders[index].Logout(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
			return fmt.Errorf("logout %s: %w", reg.Name, err)
		}
	}
	return nil
}
