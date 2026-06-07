package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

func runSecretsGet(args []string) error {
	return runSecretsGetWithWriter(args, os.Stdout)
}

// runSecretsGetWithWriter is the testable core of "wfctl secrets get".
// w receives the secret value on success; pass os.Stdout for normal operation.
func runSecretsGetWithWriter(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("secrets get", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	providerName := fs.String("provider", "", "Ad-hoc provider override (keychain|env|aws); bypasses app.yaml")
	service := fs.String("service", "", "Service name / env prefix for the ad-hoc provider")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets get <name> [options]\n\nRetrieve a secret value from the provider.\n\nOptions:\n")
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
		val, err := p.Get(ctx, name)
		if err != nil {
			return fmt.Errorf("get secret %s: %w", name, err)
		}
		fmt.Fprintln(w, val)
		return nil
	}

	// Default path: load provider from app.yaml secrets block.
	cfg, err := loadSecretsConfig(*configFile)
	if err != nil {
		return err
	}
	provider, err := newSecretsProviderFromConfig(cfg)
	if err != nil {
		return err
	}
	val, err := provider.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("get secret %s: %w", name, err)
	}
	fmt.Fprintln(w, val)
	return nil
}
