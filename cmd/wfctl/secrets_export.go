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

	var keys []string

	if *providerName != "" {
		p, err := buildAdhocProvider(*providerName, *service)
		if err != nil {
			return err
		}
		keys, err = p.List(ctx)
		if err != nil {
			if errors.Is(err, secrets.ErrUnsupported) {
				return fmt.Errorf("provider %q does not support listing; export requires an enumerable provider (use --service to set a prefix for env)", *providerName)
			}
			return fmt.Errorf("list secrets from provider %q: %w", *providerName, err)
		}
	} else {
		// Default path: load declared entries from app.yaml.
		cfg, err := loadSecretsConfig(*configFile)
		if err != nil {
			return err
		}
		for _, e := range cfg.Entries {
			keys = append(keys, e.Name)
		}
	}

	// Header identifying the source.
	src := *providerName
	if src == "" {
		src = "app.yaml"
	}
	switch *format {
	case "dotenv":
		fmt.Fprintf(w, "# exported from %s\n", src)
	case "export":
		fmt.Fprintf(w, "# exported from %s — source this file: . <(wfctl secrets export ...)\n", src)
	}

	for _, key := range keys {
		val, err := fetchExportValue(ctx, *providerName, *service, key)
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

// fetchExportValue retrieves the value for a key returned by List().
// For env providers, List() returns full env var names; os.Getenv is used
// directly to avoid the double-prefix issue (envKey would prepend prefix again).
func fetchExportValue(ctx context.Context, providerName, service, key string) (string, error) {
	if providerName == "env" {
		// List() for EnvProvider returns full env var names already.
		// Reading via os.Getenv avoids double-applying the prefix.
		val, ok := os.LookupEnv(key)
		if !ok {
			return "", fmt.Errorf("env var %s not set", key)
		}
		return val, nil
	}
	// For all other providers (keychain, aws, etc.), List() returns logical key names
	// that Get() understands directly.
	if providerName == "" {
		// Config-file path: use local env provider fallback.
		return os.Getenv(key), nil
	}
	p, err := buildAdhocProvider(providerName, service)
	if err != nil {
		return "", err
	}
	return p.Get(ctx, key)
}
