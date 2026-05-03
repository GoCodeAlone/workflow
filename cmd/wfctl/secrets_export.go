package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/GoCodeAlone/workflow/secrets"
)

func runSecretsExport(args []string) error {
	return runSecretsExportWithWriter(args, os.Stdout)
}

// runSecretsExportWithWriter is the testable core of "wfctl secrets export".
// w receives the rendered secret block; pass os.Stdout for normal operation.
// Supported formats: "dotenv" (KEY=value) and "export" (export KEY="value").
func runSecretsExportWithWriter(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("secrets export", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	providerName := fs.String("provider", "", "Ad-hoc provider override (keychain|env|aws); bypasses app.yaml")
	service := fs.String("service", "", "Service name / env prefix for the ad-hoc provider")
	format := fs.String("format", "dotenv", `Output format: "dotenv" (KEY=value) or "export" (export KEY="value")`)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets export [options]\n\nExport secrets as a shell-sourceable file.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch *format {
	case "dotenv", "export":
		// valid
	default:
		return fmt.Errorf("unknown format %q (supported: dotenv, export)", *format)
	}

	ctx := context.Background()

	// Resolve provider and keys once — no per-key rebuilds.
	var (
		provider secrets.Provider
		keys     []string
		src      string
	)

	if *providerName != "" {
		// Ad-hoc path: build provider from --provider/--service flags.
		p, err := buildAdhocProvider(*providerName, *service)
		if err != nil {
			return err
		}
		provider = p
		k, err := provider.List(ctx)
		if err != nil {
			if errors.Is(err, secrets.ErrUnsupported) {
				return fmt.Errorf("provider %q does not support listing; export requires an enumerable provider (use --service to set a prefix for env)", *providerName)
			}
			return fmt.Errorf("list secrets from provider %q: %w", *providerName, err)
		}
		keys = k
		src = *providerName
	} else {
		// Default path: load the real configured provider from app.yaml.
		cfg, err := loadSecretsConfig(*configFile)
		if err != nil {
			return err
		}
		p, err := resolveSecretsProvider(cfg)
		if err != nil {
			return err
		}
		provider = p
		for _, e := range cfg.Entries {
			keys = append(keys, e.Name)
		}
		src = "app.yaml"
	}

	// Header identifying the source.
	switch *format {
	case "dotenv":
		fmt.Fprintf(w, "# exported from %s\n", src)
	case "export":
		fmt.Fprintf(w, "# exported from %s — source this file: . <(wfctl secrets export ...)\n", src)
	}

	for _, key := range keys {
		val, err := exportGetValue(ctx, *providerName, provider, key)
		if err != nil {
			// Skip keys we cannot read (e.g. access-denied) with a comment.
			fmt.Fprintf(w, "# SKIP %s: %v\n", key, err)
			continue
		}
		switch *format {
		case "dotenv":
			fmt.Fprintf(w, "%s=%s\n", key, val)
		case "export":
			fmt.Fprintf(w, "export %s=%q\n", key, val)
		}
	}
	return nil
}

// exportGetValue retrieves the secret value for a key returned by the provider's List().
// For env providers, List() returns full env var names; os.LookupEnv is used directly to
// avoid double-prefix application (the provider's envKey would prepend the prefix again).
// For all other providers, the listed key is the logical key name understood by Get().
func exportGetValue(ctx context.Context, providerName string, p secrets.Provider, key string) (string, error) {
	if providerName == "env" {
		// EnvProvider.List() returns full env var names (e.g. "MYPREFIX_KEY").
		// Calling p.Get("MYPREFIX_KEY") with prefix "MYPREFIX_" would look up
		// "MYPREFIX_MYPREFIX_KEY" — wrong. Read the env var directly instead.
		val, ok := os.LookupEnv(key)
		if !ok {
			return "", fmt.Errorf("env var %s not set", key)
		}
		return val, nil
	}
	return p.Get(ctx, key)
}
