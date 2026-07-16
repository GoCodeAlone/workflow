package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/GoCodeAlone/workflow/config"
	registrypkg "github.com/GoCodeAlone/workflow/plugin/registry"
)

// runRegistryPush implements `wfctl registry push`.
// Reads ci.registries[] + each container's push_to[], and for each
// registry calls provider.Push(). Uses docker push as the default
// implementation; provider-specific push is wired via T22/T23.
func runRegistryPush(args []string) error {
	fs := flag.NewFlagSet("registry push", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Config file")
	dryRun := fs.Bool("dry-run", false, "Print planned push actions without executing")
	imageRef := fs.String("image", "", "Specific image ref to push (overrides auto-detect)")
	registryName := fs.String("registry", "", "Push to this registry only (default: all in push_to)")
	pluginDir := fs.String("plugin-dir", "", "Directory containing installed provider plugins (default: $WFCTL_PLUGIN_DIR or data/plugins)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		*dryRun = true
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("registry push: load config: %w", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil {
		fmt.Println("registry push: no ci.build section — nothing to push")
		return nil
	}

	// Build a map of registry name → CIRegistry for quick lookup.
	regs := make(map[string]config.CIRegistry, len(cfg.CI.Registries))
	for _, r := range cfg.CI.Registries {
		regs[r.Name] = r
	}

	// Collect image refs to push.
	type pushJob struct {
		imageRef     string
		registryName string
		registry     config.CIRegistry
	}
	var jobs []pushJob

	for i := range cfg.CI.Build.Containers {
		ctr := cfg.CI.Build.Containers[i]
		if ctr.External {
			continue // external refs are not built locally → don't push
		}
		targets := ctr.PushTo
		if *registryName != "" {
			targets = []string{*registryName}
		}
		for _, regName := range targets {
			reg, ok := regs[regName]
			if !ok {
				return fmt.Errorf("registry push: container %q push_to references unknown registry %q", ctr.Name, regName)
			}
			ref := *imageRef
			if ref == "" {
				ref = reg.Path + "/" + ctr.Name + ":latest"
			}
			jobs = append(jobs, pushJob{imageRef: ref, registryName: regName, registry: reg})
		}
	}

	if len(jobs) == 0 {
		fmt.Println("registry push: no push targets found")
		return nil
	}

	baseCtx, stopProviderCommand := boundedProviderCommandContext(containerRegistryOperationTimeout)
	defer stopProviderCommand()
	requests := make([]containerRegistryOperationRequest, 0, len(jobs))
	for _, job := range jobs {
		requests = append(requests, containerRegistryOperationRequest{Registry: job.registry, ImageReference: job.imageRef})
	}
	prepared, err := prepareContainerRegistryCapabilities(baseCtx, *pluginDir, "push", requests)
	if err != nil {
		return err
	}
	defer closePreparedContainerRegistryCapabilities(prepared)
	legacyProviders := make([]registrypkg.RegistryProvider, len(jobs))
	for index, capability := range prepared {
		if capability.handled {
			continue
		}
		legacyProviders[index], _ = registrypkg.Get(jobs[index].registry.Type)
	}
	for index, job := range jobs {
		if err := baseCtx.Err(); err != nil {
			return err
		}
		handled, err := executeContainerRegistryCapability(baseCtx, prepared[index], *dryRun, os.Stdout)
		if err != nil {
			return fmt.Errorf("push %s via %s: %w", job.imageRef, job.registry.Type, err)
		}
		if handled {
			continue
		}
		if *dryRun {
			fmt.Printf("[dry-run] push %s → %s (%s)\n", job.imageRef, job.registryName, job.registry.Path)
			continue
		}

		provider := legacyProviders[index]
		if provider == nil {
			// Fallback: docker push directly when no provider registered.
			fmt.Printf("push %s → %s (docker push)\n", job.imageRef, job.registryName)
			if err := dockerPushToRegistry(baseCtx, job.imageRef); err != nil {
				return fmt.Errorf("push %s: %w", job.imageRef, err)
			}
			continue
		}

		ctx := registrypkg.NewContext(baseCtx, os.Stdout, false)
		pcfg := registrypkg.ProviderConfig{Registry: job.registry}
		if err := provider.Push(ctx, pcfg, job.imageRef); err != nil {
			return fmt.Errorf("push %s via %s: %w", job.imageRef, job.registry.Type, err)
		}
	}
	return nil
}

var dockerPushToRegistry = func(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, "docker", "push", ref) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
