package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
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
		provider, err := newSecretsProviderFromConfig(cfg.Secrets)
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
	orgVisibility := fs.String("visibility", "private", "Org-scope visibility: all | selected | private")
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
	case prompt.CanPrompt(): // interactive: masked prompt
		value, err := prompt.Input("Value for "+name, true)
		if err != nil {
			return err
		}
		secretValue = value
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
	provider, err := newSecretsProviderFromConfig(cfg)
	if err != nil {
		return err
	}
	if err := provider.Set(context.Background(), name, secretValue); err != nil {
		return fmt.Errorf("set secret %s: %w", name, err)
	}
	fmt.Printf("set %s\n", name)
	return nil
}

// readGitHubRepoFromAppYAML returns the target GitHub repository for repo/env
// secrets. It first honors explicit YAML configuration
// (secrets.config.repo or GitHub secretStores.<name>.config.repo), then falls
// back to the current git remote so repo-local wfctl.yaml setup works for
// infra-only configs that only contain ${ENV_VAR} references.
func readGitHubRepoFromAppYAML(path string) (string, error) {
	repo, _, err := readGitHubRepoForSecretsSetup(path)
	return repo, err
}

func readGitHubRepoForSecretsSetup(path string) (string, string, error) {
	if repo, source, err := readGitHubRepoFromWorkflowConfig(path); err != nil {
		return "", "", err
	} else if repo != "" {
		return repo, "configured by " + source, nil
	}

	dir := "."
	if strings.TrimSpace(path) != "" {
		dir = filepath.Dir(path)
	}
	repo, err := readGitHubRepoFromGitRemote(dir)
	if err != nil {
		return "", "", fmt.Errorf("could not determine GitHub repo for secrets setup from %s (checked secrets.config.repo, GitHub secretStores.<name>.config.repo, and git remote.origin.url): %w", path, err)
	}
	return repo, "inferred from git remote.origin.url", nil
}

func readGitHubRepoFromWorkflowConfig(path string) (string, string, error) {
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return "", "", fmt.Errorf("load config %s: %w", path, err)
	}
	if cfg.Secrets != nil {
		if repo := stringConfigValue(cfg.Secrets.Config, "repo"); repo != "" {
			return repo, "secrets.config.repo", nil
		}
		if cfg.Secrets.DefaultStore != "" {
			if repo := githubRepoFromSecretStore(cfg.SecretStores[cfg.Secrets.DefaultStore]); repo != "" {
				return repo, fmt.Sprintf("secretStores.%s.config.repo", cfg.Secrets.DefaultStore), nil
			}
		}
	}
	if len(cfg.SecretStores) > 0 {
		names := make([]string, 0, len(cfg.SecretStores))
		for name := range cfg.SecretStores {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if repo := githubRepoFromSecretStore(cfg.SecretStores[name]); repo != "" {
				return repo, fmt.Sprintf("secretStores.%s.config.repo", name), nil
			}
		}
	}
	return "", "", nil
}

func githubRepoFromSecretStore(store *config.SecretStoreConfig) string {
	if store == nil || store.Provider != "github" {
		return ""
	}
	return stringConfigValue(store.Config, "repo")
}

func stringConfigValue(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	value, _ := cfg[key].(string)
	return strings.TrimSpace(strings.Trim(value, `"'`))
}

func readGitHubRepoFromGitRemote(dir string) (string, error) {
	remote, err := gitRemoteOriginURLFromConfig(dir)
	if err != nil {
		return "", err
	}
	if repo, ok := githubRepoFromRemoteURL(remote); ok {
		return repo, nil
	}
	return "", fmt.Errorf("remote.origin.url is not a GitHub repo URL: %s", remote)
}

func gitRemoteOriginURLFromConfig(start string) (string, error) {
	if strings.TrimSpace(start) == "" {
		start = "."
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve git search path: %w", err)
	}
	if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, statErr := os.Stat(gitPath); statErr == nil {
			configs, configErr := gitConfigCandidates(gitPath, info)
			if configErr != nil {
				return "", configErr
			}
			for _, configPath := range configs {
				remote, remoteErr := remoteOriginURLFromGitConfig(configPath)
				if remoteErr == nil && remote != "" {
					return remote, nil
				}
			}
			return "", fmt.Errorf("git remote.origin.url is empty")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no .git directory found")
}

func gitConfigCandidates(gitPath string, info os.FileInfo) ([]string, error) {
	if info.IsDir() {
		return []string{filepath.Join(gitPath, "config")}, nil
	}
	data, err := os.ReadFile(gitPath) //nolint:gosec // repository-local .git file
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", gitPath, err)
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return nil, fmt.Errorf("unsupported .git file format in %s", gitPath)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(filepath.Dir(gitPath), gitDir)
	}
	candidates := []string{filepath.Join(gitDir, "config")}
	if commonDirData, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		commonDir := strings.TrimSpace(string(commonDirData))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(gitDir, commonDir)
		}
		candidates = append(candidates, filepath.Join(commonDir, "config"))
	}
	return candidates, nil
}

func remoteOriginURLFromGitConfig(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // repository-local git config
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin || !strings.HasPrefix(line, "url") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		return strings.TrimSpace(value), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan %s: %w", path, err)
	}
	return "", nil
}

func githubRepoFromRemoteURL(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", false
	}
	if strings.HasPrefix(remote, "git@github.com:") {
		return cleanGitHubRepoPath(strings.TrimPrefix(remote, "git@github.com:"))
	}
	if strings.HasPrefix(remote, "github.com/") {
		return cleanGitHubRepoPath(strings.TrimPrefix(remote, "github.com/"))
	}
	if u, err := url.Parse(remote); err == nil && strings.EqualFold(u.Hostname(), "github.com") {
		return cleanGitHubRepoPath(u.Path)
	}
	return "", false
}

func cleanGitHubRepoPath(path string) (string, bool) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// parseGitHubOrgVisibility canonicalises the --visibility flag.
func parseGitHubOrgVisibility(s string) (secrets.GitHubOrgVisibility, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return secrets.OrgVisibilityPrivate, nil
	case "all":
		return secrets.OrgVisibilityAll, nil
	case "selected":
		return secrets.OrgVisibilitySelected, nil
	case "private":
		return secrets.OrgVisibilityPrivate, nil
	default:
		return "", fmt.Errorf("invalid visibility %q (must be all|selected|private)", s)
	}
}

// secretListJSONEntry is the JSON output shape for a single secret in --json mode.
type secretListJSONEntry struct {
	Name      string `json:"name"`
	Store     string `json:"store,omitempty"`
	State     string `json:"state"`
	Exists    bool   `json:"exists"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func runSecretsList(args []string) error {
	fs := flag.NewFlagSet("secrets list", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	envName := fs.String("env", "", "Environment name for store resolution (optional)")
	providerName := fs.String("provider", "", "Ad-hoc provider override (keychain|env|aws); bypasses app.yaml")
	service := fs.String("service", "", "Service name for keychain provider")
	asJSON := fs.Bool("json", false, "Output as JSON array")
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
		if *asJSON {
			return printSecretsJSON(statuses)
		}
		fmt.Printf("%-40s  %-12s  %-10s  %-20s\n", "NAME", "STORE", "STATUS", "UPDATED")
		fmt.Printf("%-40s  %-12s  %-10s  %-20s\n", strings.Repeat("-", 40), strings.Repeat("-", 12), strings.Repeat("-", 10), strings.Repeat("-", 20))
		for _, s := range statuses {
			updatedAt := formatUpdatedAt(s.LastRotated)
			fmt.Printf("%-40s  %-12s  %-10s  %-20s\n", s.Name, s.Store, secretStateLabel(s.State), updatedAt)
		}
		return nil
	}

	// Legacy single-provider path.
	secretsCfg := wfCfg.Secrets
	if secretsCfg == nil {
		secretsCfg = &config.SecretsConfig{Provider: "env"}
	}
	provider, err := newSecretsProviderFromConfig(secretsCfg)
	if err != nil {
		return err
	}

	// Check access if the provider supports it (only print in text mode).
	if !*asJSON {
		if adapter, ok := provider.(secretsProviderAdapter); ok {
			if accessErr := adapter.checkAccess(ctx); accessErr != nil {
				fmt.Printf("Store access: ✗ %s\n", accessErr.Error())
			} else {
				fmt.Printf("Store access: ✓\n")
			}
		}
	}

	// Build statuses for all declared entries so we can use them for --json or UPDATED column.
	// Use Check (not Get) so the adapter's StatAll→Get→List precedence applies — this is
	// essential for write-only stores like github where Get returns ErrUnsupported.
	var statuses []SecretStatus
	for _, entry := range secretsCfg.Entries {
		state, _ := provider.Check(ctx, entry.Name)
		statuses = append(statuses, SecretStatus{
			Name:  entry.Name,
			Store: cmp(secretsCfg.Provider, "env"),
			State: state,
			IsSet: state == SecretSet,
		})
	}

	// Enrich with metadata if supported.
	if adapter, ok := provider.(secretsProviderAdapter); ok {
		if mp, ok2 := adapter.p.(secrets.MetadataProvider); ok2 {
			if metas, metaErr := mp.StatAll(ctx); metaErr == nil {
				metaByName := make(map[string]secrets.SecretMeta, len(metas))
				for _, m := range metas {
					metaByName[m.Name] = m
				}
				for i, s := range statuses {
					if m, found := metaByName[s.Name]; found {
						statuses[i].LastRotated = m.UpdatedAt
					}
				}
			}
		}
	}

	if *asJSON {
		return printSecretsJSON(statuses)
	}

	fmt.Printf("Provider: %s\n\n", cmp(secretsCfg.Provider, "env"))
	fmt.Printf("%-40s  %-6s  %-20s\n", "NAME", "STATUS", "UPDATED")
	fmt.Printf("%-40s  %-6s  %-20s\n", strings.Repeat("-", 40), "------", strings.Repeat("-", 20))

	for i, entry := range secretsCfg.Entries {
		desc := ""
		if entry.Description != "" {
			desc = "  # " + entry.Description
		}
		updatedAt := "—"
		if i < len(statuses) {
			updatedAt = formatUpdatedAt(statuses[i].LastRotated)
		}
		fmt.Printf("%-40s  %-6s  %-20s%s\n", entry.Name, secretStateLabel(statuses[i].State), updatedAt, desc)
	}
	return nil
}

// printSecretsJSON marshals statuses to a JSON array and writes to stdout.
func printSecretsJSON(statuses []SecretStatus) error {
	entries := make([]secretListJSONEntry, len(statuses))
	for i, s := range statuses {
		entry := secretListJSONEntry{
			Name:   s.Name,
			Store:  s.Store,
			State:  secretStateLabel(s.State),
			Exists: s.IsSet,
		}
		if !s.LastRotated.IsZero() {
			entry.UpdatedAt = s.LastRotated.UTC().Format(time.RFC3339)
		}
		entries[i] = entry
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

// formatUpdatedAt returns a human-readable string for a LastRotated timestamp.
// Returns "—" when the timestamp is zero.
func formatUpdatedAt(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04")
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

	provider, err := newSecretsProviderFromConfig(cfg)
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
	switch *providerName {
	case "env", "":
		fmt.Printf("Provider %q uses OS environment variables — no additional setup required.\n", *providerName)
	case "github":
		fmt.Printf("Provider %q reads from GitHub Actions secrets — ensure GITHUB_TOKEN is set.\n", *providerName)
	case "vault":
		fmt.Printf("Provider %q reads from HashiCorp Vault — configure address and token in secrets.config.\n", *providerName)
	case "aws":
		fmt.Printf("Provider %q reads from AWS Secrets Manager — ensure AWS credentials are available.\n", *providerName)
	case "keychain":
		fmt.Printf("Provider %q reads from the OS keychain — no additional setup required.\n", *providerName)
	default:
		fmt.Printf("Provider %q initialized — check provider documentation for setup.\n", *providerName)
	}
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
