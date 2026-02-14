package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

func runInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	showDeps := fs.Bool("deps", false, "Show module dependency graph")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl inspect [options] <config.yaml>\n\nInspect modules, workflows, and triggers in a config.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	cfg, err := config.LoadFromFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Modules summary
	fmt.Printf("Modules (%d):\n", len(cfg.Modules))
	typeCount := make(map[string]int)
	for _, mod := range cfg.Modules {
		typeCount[mod.Type]++
		fmt.Printf("  %-30s  type=%s\n", mod.Name, mod.Type)
		if *showDeps && len(mod.DependsOn) > 0 {
			fmt.Printf("    depends on: %s\n", strings.Join(mod.DependsOn, ", "))
		}
	}

	// Module type summary
	fmt.Printf("\nModule types:\n")
	types := make([]string, 0, len(typeCount))
	for t := range typeCount {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Printf("  %-35s  x%d\n", t, typeCount[t])
	}

	// Workflows summary
	if len(cfg.Workflows) > 0 {
		fmt.Printf("\nWorkflows (%d):\n", len(cfg.Workflows))
		for name := range cfg.Workflows {
			fmt.Printf("  %s\n", name)
		}
	}

	// Triggers summary
	if len(cfg.Triggers) > 0 {
		fmt.Printf("\nTriggers (%d):\n", len(cfg.Triggers))
		for name := range cfg.Triggers {
			fmt.Printf("  %s\n", name)
		}
	}

	// Dependency graph
	if *showDeps {
		fmt.Printf("\nDependency graph:\n")
		for _, mod := range cfg.Modules {
			if len(mod.DependsOn) > 0 {
				for _, dep := range mod.DependsOn {
					fmt.Printf("  %s -> %s\n", mod.Name, dep)
				}
			}
		}
	}

	return nil
}
