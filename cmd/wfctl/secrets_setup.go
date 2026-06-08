package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	for _, a := range args {
		if a == "--manifest" || strings.HasPrefix(a, "--manifest=") {
			manifestArgs, err := parseManifestSetupFlags(args)
			if err != nil {
				return err
			}
			return runSecretsSetupManifestWithIO(manifestArgs, nil, os.Stdout)
		}
	}

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
	// Manifest-backed GitHub setup flags. These also keep the legacy --plugin
	// path compatible because runSecrets dispatches --plugin before this path.
	scope := fs.String("scope", "repo", "GitHub scope for manifest-backed setup: repo | env | org")
	org := fs.String("org", "", "Organization slug for manifest-backed --scope=org")
	visibility := fs.String("visibility", "all", "Manifest-backed org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT for manifest-backed setup")
	lockFile := fs.String("lock-file", ".wfctl-lock.yaml", "wfctl plugin lockfile used when wfctl.yaml is auto-discovered")
	pluginDir := fs.String("plugin-dir", "", "Plugin install dir used when wfctl.yaml is auto-discovered")

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
	explicitConfig := hasFlag(args, "config")
	if !explicitConfig {
		target, err := resolveDefaultSecretsSetupTarget(!useNonInteractive)
		if err != nil {
			return err
		}
		if target.kind == secretsSetupTargetManifest {
			return runSecretsSetupManifestWithIO(&manifestSetupArgs{
				manifestPath:   target.path,
				lockfilePath:   *lockFile,
				pluginDir:      *pluginDir,
				configPatterns: defaultManifestSetupConfigPatterns(),
				scope:          *scope,
				envName:        *envName,
				org:            *org,
				visibility:     *visibility,
				tokenEnv:       *tokenEnv,
				fromEnv:        *fromEnv,
				nonInteractive: useNonInteractive,
				secretLiterals: []string(secretFlag),
				only:           only,
				skipExisting:   *skipExisting,
			}, nil, os.Stdout)
		}
		if target.path != "" {
			*configFile = target.path
		}
	} else if !fileExists(*configFile) {
		return missingExplicitSecretsConfigError(*configFile)
	}

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

type secretsSetupTargetKind string

const (
	secretsSetupTargetConfig   secretsSetupTargetKind = "config"
	secretsSetupTargetManifest secretsSetupTargetKind = "manifest"
)

type secretsSetupTarget struct {
	kind secretsSetupTargetKind
	path string
}

func hasFlag(args []string, name string) bool {
	long := "--" + name
	for _, arg := range args {
		if arg == long || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}

func resolveDefaultSecretsSetupTarget(interactive bool) (secretsSetupTarget, error) {
	if fileExists("app.yaml") {
		return secretsSetupTarget{kind: secretsSetupTargetConfig, path: "app.yaml"}, nil
	}
	if fileExists("wfctl.yaml") {
		return secretsSetupTarget{kind: secretsSetupTargetManifest, path: "wfctl.yaml"}, nil
	}
	configs := discoverSecretsSetupConfigCandidates()
	if len(configs) == 1 {
		return secretsSetupTarget{kind: secretsSetupTargetConfig, path: configs[0]}, nil
	}
	if len(configs) > 1 {
		if interactive {
			options := append([]string{}, configs...)
			options = append(options, "Enter another path")
			idx, err := prompt.Select("Pick a secrets config", options)
			if err != nil {
				return secretsSetupTarget{}, err
			}
			if idx < len(configs) {
				return secretsSetupTarget{kind: secretsSetupTargetConfig, path: configs[idx]}, nil
			}
			path, err := prompt.InputWithSuggestions("Config path", false, discoverYAMLPathSuggestions("."))
			if err != nil {
				return secretsSetupTarget{}, err
			}
			path = strings.TrimSpace(path)
			if path == "" {
				return secretsSetupTarget{}, fmt.Errorf("config path is required")
			}
			if !fileExists(path) {
				return secretsSetupTarget{}, missingExplicitSecretsConfigError(path)
			}
			return secretsSetupTarget{kind: secretsSetupTargetConfig, path: path}, nil
		}
		return secretsSetupTarget{}, ambiguousSecretsConfigError(configs)
	}
	if interactive {
		path, err := prompt.InputWithSuggestions("Config path", false, discoverYAMLPathSuggestions("."))
		if err != nil {
			return secretsSetupTarget{}, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return secretsSetupTarget{}, missingSecretsSetupConfigError(nil)
		}
		if fileExists(path) {
			if filepath.Base(path) == "wfctl.yaml" {
				return secretsSetupTarget{kind: secretsSetupTargetManifest, path: path}, nil
			}
			return secretsSetupTarget{kind: secretsSetupTargetConfig, path: path}, nil
		}
		return secretsSetupTarget{}, missingExplicitSecretsConfigError(path)
	}
	return secretsSetupTarget{}, missingSecretsSetupConfigError(nil)
}

func discoverSecretsSetupConfigCandidates() []string {
	return existingFiles([]string{
		"app.yml",
		"workflow.yaml",
		"workflow.yml",
		"infra.yaml",
		"infra.yml",
	})
}

func defaultManifestSetupConfigPatterns() string {
	candidates := []string{
		"app.yaml",
		"app.yml",
		"workflow.yaml",
		"workflow.yml",
		"infra.yaml",
		"infra.yml",
		"infra/*.wfctl.yaml",
		"*.wfctl.yaml",
	}
	var patterns []string
	for _, candidate := range candidates {
		if strings.ContainsAny(candidate, "*?[") {
			matches, err := filepath.Glob(candidate)
			if err == nil && len(matches) > 0 {
				patterns = append(patterns, candidate)
			}
			continue
		}
		if fileExists(candidate) {
			patterns = append(patterns, candidate)
		}
	}
	if len(patterns) == 0 {
		return "app.yaml,infra/*.wfctl.yaml,*.wfctl.yaml"
	}
	return strings.Join(patterns, ",")
}

func existingFiles(paths []string) []string {
	var out []string
	for _, path := range paths {
		if fileExists(path) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func discoverYAMLPathSuggestions(root string) []string {
	var suggestions []string
	seen := map[string]bool{}
	for _, pattern := range []string{"*.yaml", "*.yml", "*.wfctl.yaml", "infra/*.yaml", "infra/*.yml", "infra/*.wfctl.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if rel, err := filepath.Rel(root, match); err == nil {
				match = rel
			}
			if seen[match] {
				continue
			}
			seen[match] = true
			suggestions = append(suggestions, match)
		}
	}
	sort.Strings(suggestions)
	return suggestions
}

func missingExplicitSecretsConfigError(path string) error {
	return fmt.Errorf("secrets setup config %q not found; pass --config <path> for app/workflow secrets or --manifest wfctl.yaml for plugin/infra secrets", path)
}

func ambiguousSecretsConfigError(configs []string) error {
	return fmt.Errorf("multiple plausible secrets setup configs found: %s; rerun with --config <path> or --manifest wfctl.yaml", strings.Join(configs, ", "))
}

func missingSecretsSetupConfigError(configs []string) error {
	if len(configs) > 0 {
		return ambiguousSecretsConfigError(configs)
	}
	return fmt.Errorf("no secrets setup config found; looked for app.yaml/app.yml, workflow.yaml/workflow.yml, infra.yaml/infra.yml, and wfctl.yaml. Rerun with --config <path> for app/workflow secrets or --manifest wfctl.yaml for plugin/infra secrets")
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
