package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func runRegistry(args []string) error {
	if len(args) < 1 {
		return registryUsage()
	}
	switch args[0] {
	case "list":
		return runRegistryList(args[1:])
	case "add":
		return runRegistryAdd(args[1:])
	case "remove":
		return runRegistryRemove(args[1:])
	default:
		return registryUsage()
	}
}

func registryUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl registry <subcommand> [options]

Subcommands:
  list     Show configured plugin registries
  add      Add a plugin registry source
  remove   Remove a plugin registry source
`)
	return fmt.Errorf("registry subcommand is required")
}

func runRegistryList(args []string) error {
	fs := flag.NewFlagSet("registry list", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Registry config file path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl registry list [options]\n\nShow configured plugin registries.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return err
	}

	fmt.Printf("%-15s %-10s %-25s %-25s %s\n", "NAME", "TYPE", "OWNER", "REPO", "PRIORITY")
	fmt.Printf("%-15s %-10s %-25s %-25s %s\n", "----", "----", "-----", "----", "--------")
	for _, r := range cfg.Registries {
		fmt.Printf("%-15s %-10s %-25s %-25s %d\n", r.Name, r.Type, r.Owner, r.Repo, r.Priority)
	}
	return nil
}

func runRegistryAdd(args []string) error {
	fs := flag.NewFlagSet("registry add", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Registry config file path (default: ~/.config/wfctl/config.yaml)")
	regType := fs.String("type", "github", "Registry type (github)")
	owner := fs.String("owner", "", "GitHub owner/org (required)")
	repo := fs.String("repo", "", "GitHub repo name (required)")
	branch := fs.String("branch", "main", "Git branch")
	priority := fs.Int("priority", 10, "Priority (lower = higher priority)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl registry add [options] <name>\n\nAdd a plugin registry source.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("registry name is required")
	}
	if *owner == "" || *repo == "" {
		return fmt.Errorf("--owner and --repo are required")
	}

	name := fs.Arg(0)
	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return err
	}

	// Check for duplicate
	for _, r := range cfg.Registries {
		if r.Name == name {
			return fmt.Errorf("registry %q already exists", name)
		}
	}

	cfg.Registries = append(cfg.Registries, RegistrySourceConfig{
		Name:     name,
		Type:     *regType,
		Owner:    *owner,
		Repo:     *repo,
		Branch:   *branch,
		Priority: *priority,
	})

	// Determine save path
	savePath := *cfgPath
	if savePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("determine home directory: %w", err)
		}
		savePath = filepath.Join(home, ".config", "wfctl", "config.yaml")
	}

	if err := SaveRegistryConfig(savePath, cfg); err != nil {
		return err
	}
	fmt.Printf("Added registry %q (%s/%s)\n", name, *owner, *repo)
	return nil
}

func runRegistryRemove(args []string) error {
	fs := flag.NewFlagSet("registry remove", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Registry config file path (default: ~/.config/wfctl/config.yaml)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl registry remove [options] <name>\n\nRemove a plugin registry source.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("registry name is required")
	}

	name := fs.Arg(0)
	if name == "default" {
		return fmt.Errorf("cannot remove the default registry")
	}

	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return err
	}

	found := false
	filtered := make([]RegistrySourceConfig, 0, len(cfg.Registries))
	for _, r := range cfg.Registries {
		if r.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}
	if !found {
		return fmt.Errorf("registry %q not found", name)
	}
	cfg.Registries = filtered

	savePath := *cfgPath
	if savePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("determine home directory: %w", err)
		}
		savePath = filepath.Join(home, ".config", "wfctl", "config.yaml")
	}

	if err := SaveRegistryConfig(savePath, cfg); err != nil {
		return err
	}
	fmt.Printf("Removed registry %q\n", name)
	return nil
}
