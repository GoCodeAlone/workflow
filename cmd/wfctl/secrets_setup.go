package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// runSecretsSetup implements `wfctl secrets setup --env <name>`.
// It iterates over all secrets declared in the config for the given environment
// and prompts for values, using hidden terminal input where possible.
func runSecretsSetup(args []string) error {
	fs := flag.NewFlagSet("secrets setup", flag.ContinueOnError)
	envName := fs.String("env", "local", "Target environment name")
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	autoGenKeys := fs.Bool("auto-gen-keys", false, "Auto-generate random values for secrets ending in _KEY, _SECRET, or _TOKEN")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl secrets setup [options]

Interactively set all secrets declared in the config for a given environment.
For each secret, you are prompted for a value. Input is hidden.
Secrets in no-access stores (e.g., cloud KMS without credentials) are skipped.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	data, err := os.ReadFile(*configFile)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if cfg.Secrets == nil || len(cfg.Secrets.Entries) == 0 {
		fmt.Println("No secrets declared in config.")
		return nil
	}

	fmt.Printf("Setting up secrets for environment: %s\n\n", *envName)

	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	var set, skipped int
	for _, entry := range cfg.Secrets.Entries {
		storeName := resolveSecretStoreForSetup(entry, *envName, &cfg)
		provider, provErr := newSecretsProvider(storeName)
		if provErr != nil {
			fmt.Printf("  %-24s  [SKIP] store %q not accessible: %v\n", entry.Name, storeName, provErr)
			skipped++
			continue
		}

		// Check if already set.
		existing, _ := provider.Get(ctx, entry.Name)
		hint := ""
		if existing != "" {
			hint = " (already set — press Enter to keep)"
		}

		// Auto-generate for key/secret/token names if flag is set.
		if *autoGenKeys && existing == "" && isAutoGenCandidate(entry.Name) {
			val := generateSecretValue()
			if val == "" {
				fmt.Printf("  %-24s  [ERROR] crypto/rand unavailable; cannot auto-generate\n", entry.Name)
				skipped++
				continue
			}
			if setErr := provider.Set(ctx, entry.Name, val); setErr != nil {
				fmt.Printf("  %-24s  [ERROR] %v\n", entry.Name, setErr)
			} else {
				fmt.Printf("  %-24s  [auto-generated]\n", entry.Name)
				set++
			}
			continue
		}

		// Prompt for value.
		desc := ""
		if entry.Description != "" {
			desc = " — " + entry.Description
		}
		fmt.Printf("  %s%s%s\n  Value: ", entry.Name, desc, hint)

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("read input: %w", readErr)
		}
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			if existing != "" {
				fmt.Printf("  %-24s  [kept]\n", entry.Name)
			} else {
				fmt.Printf("  %-24s  [skipped]\n", entry.Name)
				skipped++
			}
			continue
		}

		if setErr := provider.Set(ctx, entry.Name, line); setErr != nil {
			fmt.Printf("  %-24s  [ERROR] %v\n", entry.Name, setErr)
		} else {
			fmt.Printf("  %-24s  [set]\n", entry.Name)
			set++
		}
	}

	fmt.Printf("\nDone: %d set, %d skipped.\n", set, skipped)
	return nil
}

// resolveSecretStoreForSetup determines which store to use for a secret in a given environment.
// Priority: per-secret store field → environment override → defaultStore → legacy provider → "env".
// This matches the order in ResolveSecretStore so that setup and runtime agree on which store owns a secret.
func resolveSecretStoreForSetup(entry config.SecretEntry, envName string, cfg *config.WorkflowConfig) string {
	// 1. Per-secret store field (highest priority).
	if entry.Store != "" {
		return entry.Store
	}
	// 2. Environment-level store override.
	if cfg.Environments != nil {
		if env, ok := cfg.Environments[envName]; ok && env.SecretsProvider != "" {
			return env.SecretsProvider
		}
	}
	// 3. Default store from secretStores config.
	if cfg.Secrets != nil && cfg.Secrets.DefaultStore != "" {
		return cfg.Secrets.DefaultStore
	}
	// 4. Legacy provider field.
	if cfg.Secrets != nil && cfg.Secrets.Provider != "" {
		return cfg.Secrets.Provider
	}
	return "env"
}

// isAutoGenCandidate returns true if the secret name looks like a key, token, or signing secret.
func isAutoGenCandidate(name string) bool {
	upper := strings.ToUpper(name)
	for _, suffix := range []string{"_KEY", "_SECRET", "_TOKEN", "_SIGNING"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}
