package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	_ "github.com/GoCodeAlone/workflow/plugins/registry-do"
)

func runRegistryLogin(args []string) error {
	fs := flag.NewFlagSet("registry login", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	regName := fs.String("registry", "", "Login to this registry only (default: all)")
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
		if err := provider.Login(ctx, registry.ProviderConfig{Registry: reg}); err != nil {
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
