package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// secretFieldPatterns are field name substrings that indicate a secret value.
var secretFieldPatterns = []string{
	"dsn", "apikey", "api_key", "apitoken", "api_token",
	"token", "secret", "password", "passwd", "signingkey", "signing_key",
	"clientsecret", "client_secret", "privatekey", "private_key",
	"credential", "auth_key", "authkey",
}

func runSecretsDetect(args []string) error {
	fs := flag.NewFlagSet("secrets detect", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets detect [options]\n\nScan config for secret-like field values.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	detected := detectSecrets(cfg)
	if len(detected) == 0 {
		fmt.Println("No secret-like values detected.")
		return nil
	}

	fmt.Printf("Detected %d potential secret(s):\n\n", len(detected))
	for _, d := range detected {
		fmt.Printf("  module: %s\n", d.module)
		fmt.Printf("  field:  %s\n", d.field)
		fmt.Printf("  reason: %s\n", d.reason)
		fmt.Printf("  value:  %s\n", d.maskedValue)
		fmt.Println()
	}
	fmt.Println("Recommendation: move these to environment variables or a secrets provider.")
	return nil
}

type detectedSecret struct {
	module      string
	field       string
	reason      string
	maskedValue string
}

func detectSecrets(cfg *config.WorkflowConfig) []detectedSecret {
	var found []detectedSecret

	for _, mod := range cfg.Modules {
		for k, v := range mod.Config {
			val, ok := v.(string)
			if !ok {
				continue
			}

			// Check for env var references like ${VAR} or $VAR.
			if strings.Contains(val, "${") || (strings.HasPrefix(val, "$") && !strings.Contains(val, " ")) {
				found = append(found, detectedSecret{
					module:      mod.Name,
					field:       k,
					reason:      "env var reference",
					maskedValue: maskValue(val),
				})
				continue
			}

			// Check for field name patterns.
			if isSecretFieldName(k) && val != "" {
				found = append(found, detectedSecret{
					module:      mod.Name,
					field:       k,
					reason:      "secret-like field name",
					maskedValue: maskValue(val),
				})
			}
		}
	}

	// Also check secrets: entries against the provider.
	if cfg.Secrets != nil {
		provider, err := newSecretsProvider(cfg.Secrets.Provider)
		if err == nil {
			ctx := context.Background()
			for _, entry := range cfg.Secrets.Entries {
				val, _ := provider.Get(ctx, entry.Name)
				if val == "" {
					found = append(found, detectedSecret{
						module:      "(secrets section)",
						field:       entry.Name,
						reason:      "declared secret not set in provider",
						maskedValue: "<not set>",
					})
				}
			}
		}
	}

	return found
}

// isSecretFieldName returns true if the field name matches a known secret pattern.
func isSecretFieldName(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range secretFieldPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// maskValue obscures a secret value for display.
func maskValue(val string) string {
	if len(val) <= 4 {
		return "****"
	}
	return val[:2] + strings.Repeat("*", len(val)-4) + val[len(val)-2:]
}

func runSecretsSet(args []string) error {
	return runSecretsSetWithReader(args, nil)
}

// runSecretsSetWithReader is the testable core of "wfctl secrets set".
// r is an optional reader for the secret value (used in tests); pass nil for normal operation.
func runSecretsSetWithReader(args []string, r io.Reader) error {
	fs := flag.NewFlagSet("secrets set", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	value := fs.String("value", "", "Secret value to set")
	fromFile := fs.String("from-file", "", "Read secret value from file (for certs/keys)")
	providerName := fs.String("provider", "", "Ad-hoc provider override (keychain|env|aws); bypasses app.yaml")
	service := fs.String("service", "", "Service name for keychain provider")
	scope := fs.String("scope", "", "GitHub secret scope: repo (default) | env | org")
	envName := fs.String("env", "", "GitHub Actions environment name (required with --scope=env)")
	org := fs.String("org", "", "GitHub org name (required with --scope=org)")
	orgVisibility := fs.String("visibility", "all", "Org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl secrets set <name> [options]

Set a secret value in the configured provider.

Scope flags (GitHub only):
  --scope repo         Default. Writes to the configured app.yaml repo provider.
  --scope env --env <name>
                       Writes to the repo-environment of the same repo.
  --scope org --org <slug> [--visibility all|selected|private] [--token-env <var>]
                       Writes an org-level secret. Requires admin:org token scope.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("secret name is required")
	}
	name := fs.Arg(0)

	// Resolve the secret value from the highest-priority source available.
	var secretValue string
	switch {
	case *fromFile != "":
		data, err := os.ReadFile(*fromFile)
		if err != nil {
			return fmt.Errorf("read file %s: %w", *fromFile, err)
		}
		secretValue = string(data)
	case *value != "":
		secretValue = *value
	case r != nil: // explicit reader (e.g. piped input or test)
		b, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("read secret value: %w", err)
		}
		secretValue = strings.TrimRight(string(b), "\n")
	case isatty.IsTerminal(os.Stdin.Fd()): // interactive: masked prompt
		fmt.Fprintf(os.Stderr, "Value for %s: ", name)
		fd, err := stdinFileDescriptor()
		if err != nil {
			return err
		}
		b, err := term.ReadPassword(fd)
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		fmt.Fprintln(os.Stderr)
		secretValue = string(b)
	default:
		return fmt.Errorf("must provide --value, --from-file, or run interactively (TTY)")
	}

	// When --provider is given, bypass app.yaml and use the ad-hoc provider directly.
	if *providerName != "" {
		p, err := buildAdhocProvider(*providerName, *service)
		if err != nil {
			return err
		}
		if err := p.Set(context.Background(), name, secretValue); err != nil {
			return fmt.Errorf("set secret %s: %w", name, err)
		}
		fmt.Printf("set %s\n", name)
		return nil
	}

	// Org-scope: build an org GH provider directly. Bypasses app.yaml
	// since org secrets are out-of-band of the repo-scoped config.
	if *scope == "org" {
		if *org == "" {
			return fmt.Errorf("--scope=org requires --org <slug>")
		}
		vis, err := parseGitHubOrgVisibility(*orgVisibility)
		if err != nil {
			return err
		}
		p, err := secrets.NewGitHubOrgSecretsProvider(*org, *tokenEnv, vis, nil)
		if err != nil {
			return err
		}
		if err := p.Set(context.Background(), name, secretValue); err != nil {
			return fmt.Errorf("set org secret %s: %w", name, err)
		}
		fmt.Printf("set %s (org=%s, visibility=%s)\n", name, *org, *orgVisibility)
		return nil
	}

	// Env-scope: build a repo-scoped GH provider, then flip into env
	// mode. Requires the repo to be derived from --config app.yaml's
	// secret block (provider=github + config.repo).
	if *scope == "env" {
		if *envName == "" {
			return fmt.Errorf("--scope=env requires --env <environment-name>")
		}
		repo, err := readGitHubRepoFromAppYAML(*configFile)
		if err != nil {
			return err
		}
		p, err := secrets.NewGitHubSecretsProvider(repo, *tokenEnv)
		if err != nil {
			return err
		}
		p.SetEnvironment(*envName)
		if err := p.Set(context.Background(), name, secretValue); err != nil {
			return fmt.Errorf("set env secret %s: %w", name, err)
		}
		fmt.Printf("set %s (env=%s)\n", name, *envName)
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
	if err := provider.Set(context.Background(), name, secretValue); err != nil {
		return fmt.Errorf("set secret %s: %w", name, err)
	}
	fmt.Printf("set %s\n", name)
	return nil
}

// readGitHubRepoFromAppYAML loads app.yaml and returns the configured
// github repo from secrets.config.repo (or secrets.secretStores.<name>.config.repo).
func readGitHubRepoFromAppYAML(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	// Lightweight regexp scan — avoids full YAML round-trip and tolerates
	// either `secrets.config.repo` or `secretStores.<name>.config.repo`.
	re := regexp.MustCompile(`(?m)^\s*repo:\s*([^\s#]+)`)
	m := re.FindStringSubmatch(string(data))
	if len(m) < 2 {
		return "", fmt.Errorf("could not find `repo:` in %s (expected secrets.config.repo or secretStores.<name>.config.repo)", path)
	}
	return strings.Trim(m[1], `"'`), nil
}

// parseGitHubOrgVisibility canonicalises the --visibility flag.
func parseGitHubOrgVisibility(s string) (secrets.GitHubOrgVisibility, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all":
		return secrets.OrgVisibilityAll, nil
	case "selected":
		return secrets.OrgVisibilitySelected, nil
	case "private":
		return secrets.OrgVisibilityPrivate, nil
	default:
		return "", fmt.Errorf("invalid visibility %q (must be all|selected|private)", s)
	}
}

func stdinFileDescriptor() (int, error) {
	fd := os.Stdin.Fd()
	maxInt := int(^uint(0) >> 1)
	if fd > uintptr(maxInt) {
		return 0, fmt.Errorf("stdin file descriptor %d exceeds supported int range", fd)
	}
	return int(fd), nil //nolint:gosec // fd is range-checked before conversion.
}

func runSecretsList(args []string) error {
	fs := flag.NewFlagSet("secrets list", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	envName := fs.String("env", "", "Environment name for store resolution (optional)")
	providerName := fs.String("provider", "", "Ad-hoc provider override (keychain|env|aws); bypasses app.yaml")
	service := fs.String("service", "", "Service name for keychain provider")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets list [options]\n\nList declared secrets and their status.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// When --provider is given, bypass app.yaml and list directly from the ad-hoc provider.
	if *providerName != "" {
		p, err := buildAdhocProvider(*providerName, *service)
		if err != nil {
			return err
		}
		keys, err := p.List(context.Background())
		if err != nil {
			if errors.Is(err, secrets.ErrUnsupported) {
				fmt.Fprintf(os.Stderr, "Provider %q does not support listing secrets\n", *providerName)
				return nil
			}
			return fmt.Errorf("list secrets from provider %q: %w", *providerName, err)
		}
		fmt.Printf("Provider: %s (ad-hoc)\n\n", *providerName)
		fmt.Printf("%-40s\n", "NAME")
		fmt.Printf("%-40s\n", strings.Repeat("-", 40))
		for _, k := range keys {
			fmt.Printf("%-40s\n", k)
		}
		return nil
	}

	// Load the full WorkflowConfig so we can use multi-store resolution.
	wfCfg, err := loadWorkflowConfigForSecrets(*configFile)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Use multi-store aware status building when secretStores are configured.
	if wfCfg.SecretStores != nil || (wfCfg.Secrets != nil && wfCfg.Secrets.DefaultStore != "") {
		statuses, err := buildSecretStatuses(ctx, *envName, wfCfg)
		if err != nil {
			return err
		}
		fmt.Printf("%-40s  %-12s  %-10s\n", "NAME", "STORE", "STATUS")
		fmt.Printf("%-40s  %-12s  %-10s\n", strings.Repeat("-", 40), strings.Repeat("-", 12), strings.Repeat("-", 10))
		for _, s := range statuses {
			fmt.Printf("%-40s  %-12s  %-10s\n", s.Name, s.Store, secretStateLabel(s.State))
		}
		return nil
	}

	// Legacy single-provider path.
	secretsCfg := wfCfg.Secrets
	if secretsCfg == nil {
		secretsCfg = &config.SecretsConfig{Provider: "env"}
	}
	provider, err := newSecretsProvider(secretsCfg.Provider)
	if err != nil {
		return err
	}

	fmt.Printf("Provider: %s\n\n", cmp(secretsCfg.Provider, "env"))
	fmt.Printf("%-40s  %-6s\n", "NAME", "STATUS")
	fmt.Printf("%-40s  %-6s\n", strings.Repeat("-", 40), "------")

	for _, entry := range secretsCfg.Entries {
		val, _ := provider.Get(ctx, entry.Name)
		status := "unset"
		if val != "" {
			status = "set"
		}
		desc := ""
		if entry.Description != "" {
			desc = "  # " + entry.Description
		}
		fmt.Printf("%-40s  %-6s%s\n", entry.Name, status, desc)
	}
	return nil
}

// secretStateLabel returns a human-readable label for a SecretState.
func secretStateLabel(state SecretState) string {
	switch state {
	case SecretSet:
		return "set"
	case SecretNotSet:
		return "unset"
	case SecretNoAccess:
		return "no-access"
	case SecretFetchError:
		return "error"
	case SecretUnconfigured:
		return "unconfigured"
	default:
		return "unknown"
	}
}

// loadWorkflowConfigForSecrets loads the full WorkflowConfig for secret operations.
// Falls back to a default env-provider config if the file does not exist.
func loadWorkflowConfigForSecrets(configFile string) (*config.WorkflowConfig, error) {
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &config.WorkflowConfig{ //nolint:nilerr // gracefully fall back when file is absent
				Secrets: &config.SecretsConfig{Provider: "env"},
			}, nil
		}
		return nil, fmt.Errorf("load config: %w", err)
	}
	if cfg.Secrets == nil {
		cfg.Secrets = &config.SecretsConfig{Provider: "env"}
	}
	return cfg, nil
}

func runSecretsValidate(args []string) error {
	fs := flag.NewFlagSet("secrets validate", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets validate [options]\n\nValidate that all declared secrets are set.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadSecretsConfig(*configFile)
	if err != nil {
		return err
	}

	provider, err := newSecretsProvider(cfg.Provider)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var missing []string
	for _, entry := range cfg.Entries {
		val, _ := provider.Get(ctx, entry.Name)
		if val == "" {
			missing = append(missing, entry.Name)
		}
	}

	if len(missing) == 0 {
		fmt.Printf("All %d secret(s) are set.\n", len(cfg.Entries))
		return nil
	}
	return fmt.Errorf("%d secret(s) not set: %s", len(missing), strings.Join(missing, ", "))
}

func runSecretsInit(args []string) error {
	fs := flag.NewFlagSet("secrets init", flag.ContinueOnError)
	providerName := fs.String("provider", "env", "Secrets provider: env")
	envName := fs.String("env", "", "Target environment name")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets init [options]\n\nInitialize secrets provider configuration.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if _, err := newSecretsProvider(*providerName); err != nil {
		return err
	}

	envSuffix := ""
	if *envName != "" {
		envSuffix = " for environment " + *envName
	}
	fmt.Printf("Initialized secrets provider %q%s\n", *providerName, envSuffix)
	fmt.Printf("Provider %q uses OS environment variables — no additional setup required.\n", *providerName)
	return nil
}

func runSecretsRotate(args []string) error {
	fs := flag.NewFlagSet("secrets rotate", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	envName := fs.String("env", "", "Target environment name")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets rotate <name> [options]\n\nTrigger rotation of a secret.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("secret name is required")
	}
	name := fs.Arg(0)

	cfg, err := loadSecretsConfig(*configFile)
	if err != nil {
		return err
	}

	if cfg.Rotation == nil || !cfg.Rotation.Enabled {
		return fmt.Errorf("rotation is not enabled in secrets config")
	}

	envSuffix := ""
	if *envName != "" {
		envSuffix = " in environment " + *envName
	}
	fmt.Printf("Rotation triggered for %q%s\n", name, envSuffix)
	fmt.Printf("  strategy: %s\n", cfg.Rotation.Strategy)
	fmt.Printf("  interval: %s\n", cfg.Rotation.Interval)
	fmt.Printf("  NOTE: actual rotation implementation depends on provider — Tier 2 feature\n")
	return nil
}

func runSecretsSync(args []string) error {
	fs := flag.NewFlagSet("secrets sync", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fromEnv := fs.String("from", "", "Source environment (required)")
	toEnv := fs.String("to", "", "Destination environment (required)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl secrets sync [options]\n\nCopy secret structure between environments.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fromEnv == "" || *toEnv == "" {
		return fmt.Errorf("--from and --to are required")
	}

	cfg, err := loadSecretsConfig(*configFile)
	if err != nil {
		return err
	}

	fmt.Printf("Syncing secret structure from %q to %q (provider: %s)\n", *fromEnv, *toEnv, cfg.Provider)
	fmt.Printf("  %d secret definition(s) to sync\n", len(cfg.Entries))
	for _, entry := range cfg.Entries {
		fmt.Printf("  - %s\n", entry.Name)
	}
	fmt.Printf("  NOTE: actual value sync depends on provider — Tier 2 feature\n")
	return nil
}

// loadSecretsConfig reads a workflow config and returns its SecretsConfig.
// Returns a default env-provider config if no secrets: section is defined.
func loadSecretsConfig(configFile string) (*config.SecretsConfig, error) {
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &config.SecretsConfig{Provider: "env"}, nil //nolint:nilerr // gracefully fall back when file is absent
		}
		return nil, fmt.Errorf("load config %q: %w", configFile, err)
	}
	if cfg.Secrets == nil {
		return &config.SecretsConfig{Provider: "env"}, nil
	}
	return cfg.Secrets, nil
}
