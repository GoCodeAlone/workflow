package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/builder"
	_ "github.com/GoCodeAlone/workflow/plugins/builder-nodejs"
)

// runBuildUIPlugin handles `wfctl build ui` — dispatches nodejs targets via the
// builder-nodejs plugin. Distinct from runBuildUI (the legacy `wfctl build-ui` command).
func runBuildUIPlugin(args []string) error {
	fs := flag.NewFlagSet("build ui", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	target := fs.String("target", "", "Build only the named nodejs target (default: all)")
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

	b, ok := builder.Get("nodejs")
	if !ok {
		return fmt.Errorf("nodejs builder not registered")
	}

	var ran int
	for _, t := range cfg.CI.Build.Targets {
		if t.Type != "nodejs" {
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
		return fmt.Errorf("no nodejs target named %q found in config", *target)
	}
	return nil
}
