package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/builder"
	_ "github.com/GoCodeAlone/workflow/plugins/builder-go"
)

func runBuildGo(args []string) error {
	fs := flag.NewFlagSet("build go", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	target := fs.String("target", "", "Build only the named go target (default: all)")
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

	b, ok := builder.Get("go")
	if !ok {
		return fmt.Errorf("go builder not registered")
	}

	var ran int
	for _, t := range cfg.CI.Build.Targets {
		if t.Type != "go" {
			continue
		}
		if *target != "" && t.Name != *target {
			continue
		}
		ran++
		bcfg := builder.Config{
			TargetName: t.Name,
			Path:       t.Path,
			Fields:     t.Config,
		}
		if bcfg.Fields == nil {
			bcfg.Fields = map[string]any{}
		}
		out := &builder.Outputs{}
		if err := b.Build(context.Background(), bcfg, out); err != nil {
			return fmt.Errorf("build %s: %w", t.Name, err)
		}
		for _, a := range out.Artifacts {
			fmt.Printf("built %s → %v\n", a.Name, a.Paths)
		}
	}

	if *target != "" && ran == 0 {
		return fmt.Errorf("no go target named %q found in config", *target)
	}
	return nil
}
