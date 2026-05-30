package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// runSecretsSetup implements `wfctl secrets setup [options]`.
// In non-interactive mode (--non-interactive or non-TTY stdin) it routes
// through runSecretsSetupNonInteractive. In interactive mode it falls back
// to the legacy prompt loop.
func runSecretsSetup(args []string) error {
	fs := flag.NewFlagSet("secrets setup", flag.ContinueOnError)
	envName := fs.String("env", "local", "Target environment name")
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	autoGenKeys := fs.Bool("auto-gen-keys", false, "Auto-generate random values for secrets ending in _KEY, _SECRET, or _TOKEN")
	nonInteractive := fs.Bool("non-interactive", false, "Non-interactive mode (also auto when stdin is not a TTY)")
	fromEnv := fs.Bool("from-env", false, "Read each secret value from $NAME (recommended for CI; avoids process-table leaks)")
	var secretFlag multiStringFlag
	fs.Var(&secretFlag, "secret", "NAME=VALUE literal (WARNING: leaks to process table; use --from-env in CI). Repeatable.")
	onlyFlag := fs.String("only", "", "Comma-separated list of secret names to set (default: all)")
	skipExisting := fs.Bool("skip-existing", false, "Skip secrets that already have a value in the store")
	storeName := fs.String("store", "", "Named store to use (overrides config defaultStore)")
	// Legacy flags (kept for backwards compatibility with --plugin path).
	fs.String("scope", "repo", "GitHub scope: repo | env | org (legacy --plugin path)")
	fs.String("org", "", "Organization slug (legacy --plugin path)")
	fs.String("visibility", "all", "Org-scope visibility (legacy --plugin path)")
	fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT (legacy --plugin path)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl secrets setup [options]

Set secrets declared in the config for a given environment.

In non-interactive mode (--non-interactive, or when stdin is not a TTY):
  --from-env        Read each value from $NAME. Recommended for CI.
  --secret NAME=VAL Set a specific secret inline (WARNING: leaks to process table).
  Pipe KEY=VALUE    Read KEY=VALUE lines from stdin.
  --only A,B        Restrict which secrets to set.
  --skip-existing   Skip secrets already set in the store.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Determine if we should use non-interactive mode.
	useNonInteractive := *nonInteractive || !isatty.IsTerminal(os.Stdin.Fd())

	// Parse --only.
	var only []string
	if *onlyFlag != "" {
		for _, n := range strings.Split(*onlyFlag, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				only = append(only, n)
			}
		}
	}

	// Collect stdin KV pairs when in non-interactive mode and stdin is a pipe.
	var stdinKV []string
	if useNonInteractive {
		stdinKV = readStdinKV()
	}

	if useNonInteractive {
		return runSecretsSetupNonInteractiveCtx(context.Background(), &nonInteractiveSetupArgs{
			configFile:     *configFile,
			storeName:      *storeName,
			fromEnv:        *fromEnv,
			secretLiterals: []string(secretFlag),
			stdinKV:        stdinKV,
			only:           only,
			skipExisting:   *skipExisting,
		}, os.Stdout)
	}

	// ---- Interactive (legacy) path ----
	cfg, err := config.LoadFromFile(*configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
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
		resolvedStore := resolveSetupStore(*storeName, entry.Name, *envName, cfg)
		provider, provErr := getProviderForStore(resolvedStore, cfg)
		if provErr != nil {
			fmt.Printf("  %-24s  [SKIP] store %q not accessible: %v\n", entry.Name, resolvedStore, provErr)
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

		if isSecretSensitive(entry.Name) && isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Printf("  %s%s%s\n  Value (hidden): ", entry.Name, desc, hint)
			fd, fdErr := stdinFileDescriptor()
			if fdErr != nil {
				return fdErr
			}
			b, readErr := term.ReadPassword(fd)
			fmt.Println()
			if readErr != nil {
				return fmt.Errorf("read masked: %w", readErr)
			}
			line := string(b)
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
			continue
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

// resolveSetupStore picks the store for the given secret in the given env,
// applying the --store override first.
func resolveSetupStore(storeFlag, secretName, envName string, cfg *config.WorkflowConfig) string {
	if storeFlag != "" {
		return storeFlag
	}
	return ResolveSecretStore(secretName, envName, cfg)
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
