package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func runInfra(args []string) error {
	if len(args) < 1 {
		return infraUsage()
	}
	switch args[0] {
	case "plan":
		return runInfraPlan(args[1:])
	case "apply":
		return runInfraApply(args[1:])
	case "status":
		return runInfraStatus(args[1:])
	case "drift":
		return runInfraDrift(args[1:])
	case "destroy":
		return runInfraDestroy(args[1:])
	default:
		return infraUsage()
	}
}

func infraUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl infra <action> [options] [config.yaml]

Manage infrastructure defined in a workflow config.

Actions:
  plan      Show planned infrastructure changes
  apply     Apply infrastructure changes
  status    Show current infrastructure status
  drift     Detect configuration drift
  destroy   Tear down infrastructure

Options:
  --config <file>    Config file (default: infra.yaml or config/infra.yaml)
  --auto-approve     Skip confirmation prompt (apply/destroy only)
`)
	return fmt.Errorf("missing or unknown action")
}

// resolveInfraConfig finds the config file from flags or defaults.
func resolveInfraConfig(fs *flag.FlagSet) (string, error) {
	configFile := fs.Lookup("config").Value.String()
	if configFile != "" {
		return configFile, nil
	}
	for _, candidate := range []string{"infra.yaml", "config/infra.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Check remaining args for a positional config file
	for _, arg := range fs.Args() {
		if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
			return arg, nil
		}
	}
	return "", fmt.Errorf("no infrastructure config found (tried infra.yaml, config/infra.yaml).\nCreate an infra config with cloud.account and platform.* modules.\nRun 'wfctl init --template full-stack' for a starter config with infrastructure.")
}

// infraModuleEntry is a minimal struct for parsing modules from YAML.
type infraModuleEntry struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config"`
}

// discoverInfraModules parses the config and finds IaC-related modules.
func discoverInfraModules(cfgFile string) (iacState []infraModuleEntry, platforms []infraModuleEntry, cloudAccounts []infraModuleEntry, err error) {
	data, readErr := os.ReadFile(cfgFile)
	if readErr != nil {
		return nil, nil, nil, fmt.Errorf("read %s: %w", cfgFile, readErr)
	}

	var parsed struct {
		Modules []infraModuleEntry `yaml:"modules"`
	}
	if yamlErr := yaml.Unmarshal(data, &parsed); yamlErr != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", cfgFile, yamlErr)
	}

	for _, m := range parsed.Modules {
		switch {
		case m.Type == "iac.state":
			iacState = append(iacState, m)
		case m.Type == "cloud.account":
			cloudAccounts = append(cloudAccounts, m)
		case strings.HasPrefix(m.Type, "platform."):
			platforms = append(platforms, m)
		}
	}
	return
}

func runInfraPlan(args []string) error {
	fs := flag.NewFlagSet("infra plan", flag.ContinueOnError)
	_ = fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs)
	if err != nil {
		return err
	}

	iacStates, platforms, cloudAccounts, err := discoverInfraModules(cfgFile)
	if err != nil {
		return err
	}

	fmt.Printf("Infrastructure Plan\n")
	fmt.Printf("===================\n")
	fmt.Printf("Config:  %s\n\n", cfgFile)

	if len(cloudAccounts) == 0 {
		fmt.Printf("WARNING: No cloud.account modules found.\n\n")
	} else {
		for _, ca := range cloudAccounts {
			provider, _ := ca.Config["provider"].(string)
			fmt.Printf("Cloud Account: %s (provider: %s)\n", ca.Name, provider)
		}
		fmt.Println()
	}

	if len(iacStates) == 0 {
		fmt.Printf("WARNING: No iac.state modules found — state will not be persisted.\n\n")
	} else {
		for _, is := range iacStates {
			backend, _ := is.Config["backend"].(string)
			dir, _ := is.Config["directory"].(string)
			fmt.Printf("State Backend: %s (backend: %s, dir: %s)\n", is.Name, backend, dir)
		}
		fmt.Println()
	}

	if len(platforms) == 0 {
		return fmt.Errorf("no platform.* modules found in %s", cfgFile)
	}

	fmt.Printf("Resources to manage (%d):\n", len(platforms))
	for _, p := range platforms {
		fmt.Printf("  + %s (%s)\n", p.Name, p.Type)
		for k, v := range p.Config {
			if k == "account" || k == "provider" {
				continue
			}
			fmt.Printf("      %s: %v\n", k, v)
		}
	}
	fmt.Println()

	// Execute plan via wfctl pipeline run
	fmt.Printf("Running plan pipeline...\n")
	return runPipelineRun([]string{"-c", cfgFile, "-p", "plan"})
}

func runInfraApply(args []string) error {
	fs := flag.NewFlagSet("infra apply", flag.ContinueOnError)
	configFlag := fs.String("config", "", "Config file")
	autoApprove := fs.Bool("auto-approve", false, "Skip confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := *configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs)
		if err != nil {
			return err
		}
	}

	if !*autoApprove {
		fmt.Printf("Apply infrastructure changes from %s? [y/N]: ", cfgFile)
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Printf("Applying infrastructure from %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "apply"})
}

func runInfraStatus(args []string) error {
	fs := flag.NewFlagSet("infra status", flag.ContinueOnError)
	_ = fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs)
	if err != nil {
		return err
	}

	fmt.Printf("Infrastructure status from %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "status"})
}

func runInfraDrift(args []string) error {
	fs := flag.NewFlagSet("infra drift", flag.ContinueOnError)
	_ = fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs)
	if err != nil {
		return err
	}

	fmt.Printf("Detecting drift for %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "drift"})
}

func runInfraDestroy(args []string) error {
	fs := flag.NewFlagSet("infra destroy", flag.ContinueOnError)
	configFlag := fs.String("config", "", "Config file")
	autoApprove := fs.Bool("auto-approve", false, "Skip confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := *configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs)
		if err != nil {
			return err
		}
	}

	if !*autoApprove {
		fmt.Printf("DESTROY all infrastructure defined in %s? This cannot be undone. [y/N]: ", cfgFile)
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Printf("Destroying infrastructure from %s...\n", cfgFile)
	return runPipelineRun([]string{"-c", cfgFile, "-p", "destroy"})
}
