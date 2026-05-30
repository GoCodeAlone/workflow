package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
	"github.com/mattn/go-isatty"
)

// runSecretsSetup implements `wfctl secrets setup [options]`.
//
// Routing:
//   - --non-interactive, --auto-gen-keys, or a non-TTY stdin → the engine-backed
//     non-interactive path (flag/env-sourced selector + valuer).
//   - otherwise → the interactive wizard (prompt.MultiSelect selector +
//     masked prompt.Input valuer). If the wizard detects a non-TTY mid-flow
//     it returns prompt.ErrNotInteractive and we fall back to non-interactive.
//
// Both paths share the same runSetupEngine + audit logic.
func runSecretsSetup(args []string) error {
	fs := flag.NewFlagSet("secrets setup", flag.ContinueOnError)
	envName := fs.String("env", "local", "Target environment name")
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	autoGenKeys := fs.Bool("auto-gen-keys", false, "Auto-generate random values for secrets ending in _KEY, _SECRET, or _TOKEN (implies non-interactive)")
	nonInteractive := fs.Bool("non-interactive", false, "Non-interactive mode (also auto when stdin is not a TTY)")
	fromEnv := fs.Bool("from-env", false, "Read each secret value from $NAME (recommended for CI; avoids process-table leaks)")
	var secretFlag multiStringFlag
	fs.Var(&secretFlag, "secret", "NAME=VALUE literal (WARNING: leaks to process table; use --from-env in CI). Repeatable.")
	onlyFlag := fs.String("only", "", "Comma-separated list of secret names to set (default: all)")
	allFlag := fs.Bool("all", false, "Set all declared secrets (the default when --only is absent; explicit form)")
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

Interactive (default when stdin is a TTY):
  Lists each declared secret with its status, lets you select which to set,
  prompts to pick a store when none is configured, and masks sensitive input.

Non-interactive (--non-interactive, --auto-gen-keys, or when stdin is not a TTY):
  --from-env        Read each value from $NAME. Recommended for CI.
  --secret NAME=VAL Set a specific secret inline (WARNING: leaks to process table).
  Pipe KEY=VALUE    Read KEY=VALUE lines from stdin.
  --all             Set all declared secrets (default when --only is absent).
  --only A,B        Restrict which secrets to set (mutually exclusive with --all).
  --skip-existing   Skip secrets already set in the store.
  --auto-gen-keys   Generate random values for _KEY/_SECRET/_TOKEN/_SIGNING names.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// --auto-gen-keys is inherently non-interactive (it generates values).
	useNonInteractive := *nonInteractive || *autoGenKeys || !isatty.IsTerminal(os.Stdin.Fd())

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

	// --all and --only are mutually exclusive. --all is the explicit form of
	// the default (set every declared secret); when present it just means
	// "don't restrict", which is what an empty --only already does.
	if *allFlag && len(only) > 0 {
		return fmt.Errorf("--all and --only are mutually exclusive")
	}

	ctx := context.Background()

	if useNonInteractive {
		return runSecretsSetupNonInteractiveCtx(ctx, &nonInteractiveSetupArgs{
			configFile:     *configFile,
			storeName:      *storeName,
			fromEnv:        *fromEnv,
			secretLiterals: []string(secretFlag),
			stdinKV:        readStdinKV(),
			only:           only,
			skipExisting:   *skipExisting,
			autoGenKeys:    *autoGenKeys,
		}, os.Stdout)
	}

	// ---- Interactive wizard ----
	err := runSecretsSetupInteractive(ctx, &interactiveSetupArgs{
		configFile: *configFile,
		storeName:  *storeName,
		envName:    *envName,
	}, os.Stdout)
	if errors.Is(err, prompt.ErrNotInteractive) {
		// stdin turned out not to be a TTY mid-flow — fall back gracefully.
		return runSecretsSetupNonInteractiveCtx(ctx, &nonInteractiveSetupArgs{
			configFile:     *configFile,
			storeName:      *storeName,
			fromEnv:        *fromEnv,
			secretLiterals: []string(secretFlag),
			stdinKV:        readStdinKV(),
			only:           only,
			skipExisting:   *skipExisting,
			autoGenKeys:    *autoGenKeys,
		}, os.Stdout)
	}
	return err
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
