package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/secrets"
)

func runSecretsDelete(args []string) error {
	fs := flag.NewFlagSet("secrets delete", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	providerName := fs.String("provider", "", "Ad-hoc provider override (keychain|env|aws); bypasses app.yaml")
	service := fs.String("service", "", "Service name / env prefix for the ad-hoc provider")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets delete <name> [options]\n\nDelete a secret from the provider (idempotent).\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("secret name is required")
	}
	name := fs.Arg(0)

	ctx := context.Background()

	// When --provider is given, bypass app.yaml and use the ad-hoc provider directly.
	if *providerName != "" {
		p, err := buildAdhocProvider(*providerName, *service)
		if err != nil {
			return err
		}
		if err := p.Delete(ctx, name); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return fmt.Errorf("delete secret %s: %w", name, err)
		}
		fmt.Printf("deleted %s\n", name)
		return nil
	}

	// Default path: load provider from app.yaml secrets block.
	cfg, err := loadSecretsConfig(*configFile)
	if err != nil {
		return err
	}
	provider, err := newSecretsProvider(cfg.Provider)
	if err != nil {
		return err
	}
	if err := provider.Delete(ctx, name); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("delete secret %s: %w", name, err)
	}
	fmt.Printf("deleted %s\n", name)
	return nil
}
