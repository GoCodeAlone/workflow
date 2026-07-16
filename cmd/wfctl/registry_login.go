package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-aws"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-azure"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-do"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-gcp"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-github"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-gitlab"
)

func runRegistryLogin(args []string) error {
	fs := flag.NewFlagSet("registry login", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	regName := fs.String("registry", "", "Login to this registry only (default: all)")
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
	prepared, err := prepareContainerRegistryCapabilities(ctx, *pluginDir, "login", requests)
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
			return fmt.Errorf("login %s: %w", reg.Name, err)
		}
		if handled {
			continue
		}
		if err := legacyProviders[index].Login(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
			return fmt.Errorf("login %s: %w", reg.Name, err)
		}
	}
	return nil
}

func resolveRegistries(cfg *config.WorkflowConfig, name string) []config.CIRegistry {
	if cfg.CI == nil {
		return nil
	}
	if name == "" {
		return cfg.CI.Registries
	}
	for _, r := range cfg.CI.Registries {
		if r.Name == name {
			return []config.CIRegistry{r}
		}
	}
	return nil
}
