package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/GoCodeAlone/workflow/config"
)

func runBuildPush(args []string) error {
	fs := flag.NewFlagSet("build push", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.CI == nil || cfg.CI.Build == nil {
		return nil
	}

	// Build registry lookup by name.
	registries := make(map[string]config.CIRegistry, len(cfg.CI.Registries))
	for _, r := range cfg.CI.Registries {
		registries[r.Name] = r
	}

	dryRun := os.Getenv("WFCTL_BUILD_DRY_RUN") == "1"

	for _, ct := range cfg.CI.Build.Containers {
		if ct.External {
			continue
		}
		if len(ct.PushTo) == 0 {
			continue
		}
		for _, regName := range ct.PushTo {
			reg, ok := registries[regName]
			if !ok {
				return fmt.Errorf("container %q references unknown registry %q", ct.Name, regName)
			}
			imageRef := fmt.Sprintf("%s/%s:%s", reg.Path, ct.Name, imageTag(ct.Tag))
			if dryRun {
				fmt.Printf("push %s → %s\n", ct.Name, imageRef)
				continue
			}
			if err := dockerPush(imageRef); err != nil {
				return fmt.Errorf("push %s: %w", imageRef, err)
			}
			fmt.Printf("pushed %s\n", imageRef)
		}
	}
	return nil
}

func dockerPush(imageRef string) error {
	cmd := exec.CommandContext(context.Background(), "docker", "push", imageRef) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func imageTag(tag string) string {
	if tag == "" {
		return "latest"
	}
	return tag
}
