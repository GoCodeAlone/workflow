package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
	"gopkg.in/yaml.v3"
)

var manifestEnvRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type manifestDiscoveredSecret struct {
	PluginRequiredSecret
	Sources   []string
	StoreHint string
}

type manifestSecretTarget struct {
	Secret   manifestDiscoveredSecret
	Store    string
	Label    string
	Provider SecretsProvider
	Status   SecretStatus
}

type manifestSecretTargetProvider struct {
	Store    string
	Label    string
	Provider SecretsProvider
}

type manifestSetupArgs struct {
	manifestPath   string
	lockfilePath   string
	pluginDir      string
	configPatterns string
	scope          string
	scopeExplicit  bool
	envName        string
	org            string
	visibility     string
	tokenEnv       string
	fromEnv        bool
	nonInteractive bool
	secretLiterals []string
	only           []string
	all            bool
	skipExisting   bool
}

func runSecretsSetupManifestWithIO(a *manifestSetupArgs, in io.Reader, out io.Writer) error {
	discovered, err := discoverManifestSecrets(a.manifestPath, a.lockfilePath, a.pluginDir, a.configPatterns)
	if err != nil {
		return err
	}
	if len(discovered) == 0 {
		fmt.Fprintln(out, "No plugin required_secrets[] or config env references found.")
		return nil
	}

	secretMap, err := buildSecretLiteralMap(a.secretLiterals)
	if err != nil {
		return err
	}
	if in != nil {
		for _, kv := range readKVLines(in) {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				secretMap[k] = v
			}
		}
	}
	interactive := in == nil && !a.nonInteractive && prompt.CanPrompt()

	var promptErr error
	valuer := func(secret manifestDiscoveredSecret) (string, bool, error) {
		value, provided, err := manifestSecretValue(secret, manifestSecretValueOptions{
			interactive: interactive,
			fromEnv:     a.fromEnv,
			secretMap:   secretMap,
		})
		if err != nil && errors.Is(err, prompt.ErrNotInteractive) {
			promptErr = err
		}
		return value, provided, err
	}

	if interactive && !a.scopeExplicit {
		return runSecretsSetupManifestTargets(context.Background(), a, discovered, valuer, out, &promptErr)
	}

	ghProvider, scopeLabel, err := buildSecretWriter(strings.ToLower(strings.TrimSpace(a.scope)), a.envName, a.org, a.visibility, a.tokenEnv, firstConfigPattern(a.configPatterns))
	if err != nil {
		return err
	}
	provider := secretsProviderAdapter{p: ghProvider}

	onlySet := make(map[string]bool, len(a.only))
	for _, name := range a.only {
		onlySet[name] = true
	}
	selector := func(ds []manifestDiscoveredSecret, statuses []SecretStatus) ([]manifestDiscoveredSecret, error) {
		if interactive && len(onlySet) == 0 {
			selectable := ds
			if a.skipExisting {
				selectable = selectManifestSecretsForSetup(ds, statuses, manifestSecretSelectionOptions{
					includeExisting: true,
					skipExisting:    true,
				})
			}
			if len(selectable) == 0 {
				return nil, nil
			}
			items := buildManifestMultiSelectItems(selectable, statuses, a.all)
			selectedIdx, err := prompt.MultiSelect(manifestMultiSelectTitle(scopeLabel, a.skipExisting), items)
			if err != nil {
				return nil, err
			}
			return manifestSecretsByIndexes(selectable, selectedIdx), nil
		}
		return selectManifestSecretsForSetup(ds, statuses, manifestSecretSelectionOptions{
			onlySet:         onlySet,
			includeExisting: true,
			skipExisting:    a.skipExisting,
		}), nil
	}
	auditFn := func(name, _ string) {
		_ = writeSecretsAuditRecord(name, "github:"+strings.ToLower(strings.TrimSpace(a.scope))) //nolint:errcheck // best-effort audit
	}

	fmt.Fprintf(out, "Setting up secrets from %s -> %s\n\n", a.manifestPath, scopeLabel)
	switch {
	case interactive && a.skipExisting:
		fmt.Fprintln(out, "Interactive mode: --skip-existing is set; existing secrets are hidden and unset secrets are selected by default.")
		fmt.Fprintln(out, "Leave a value empty to skip it.")
		fmt.Fprintln(out, "Use --from-env, --secret NAME=VALUE, or --non-interactive for scripted setup.")
	case interactive:
		fmt.Fprintln(out, "Interactive mode: unset secrets are selected by default; toggle existing secrets to update them.")
		fmt.Fprintln(out, "Leave a value empty to skip it.")
		fmt.Fprintln(out, "Use --from-env, --secret NAME=VALUE, or --non-interactive for scripted setup.")
	case a.fromEnv:
		fmt.Fprintln(out, "Non-interactive mode: reading values from matching environment variables; unset values will be skipped.")
	default:
		fmt.Fprintln(out, "Non-interactive mode: using --secret NAME=VALUE or piped KEY=VALUE values; missing values will fail.")
	}
	fmt.Fprintln(out)
	if !interactive {
		for _, secret := range discovered {
			fmt.Fprintf(out, "  %s (%s)\n", secret.Name, strings.Join(secret.Sources, ", "))
		}
		fmt.Fprintln(out)
	}

	report, err := runSetupEngine(context.Background(), discovered,
		func(secret manifestDiscoveredSecret) string { return secret.Name },
		provider, selector, valuer, auditFn, true)
	if promptErr != nil {
		return promptErr
	}
	if err != nil {
		return err
	}
	for _, n := range report.Set {
		fmt.Fprintf(out, "  %s: set\n", n)
	}
	for _, n := range report.Skipped {
		fmt.Fprintf(out, "  %s: skipped (no value provided)\n", n)
	}
	fmt.Fprintln(out, "\nAll done.")
	return nil
}

func runSecretsSetupManifestTargets(ctx context.Context, a *manifestSetupArgs, discovered []manifestDiscoveredSecret, valuer func(manifestDiscoveredSecret) (string, bool, error), out io.Writer, promptErr *error) error {
	providers, err := buildManifestSecretTargetProviders(a)
	if err != nil {
		return err
	}
	targets := queryManifestSecretTargets(ctx, discovered, providers)

	onlySet := make(map[string]bool, len(a.only))
	for _, name := range a.only {
		onlySet[name] = true
	}
	selectable := selectManifestSecretTargetsForSetup(targets, manifestSecretSelectionOptions{
		onlySet:         onlySet,
		includeExisting: true,
		skipExisting:    a.skipExisting,
	})
	if len(selectable) == 0 {
		fmt.Fprintln(out, "No unset secrets found in the selected provider targets.")
		return nil
	}

	fmt.Fprintf(out, "Setting up secrets from %s -> provider targets\n\n", a.manifestPath)
	if a.skipExisting {
		fmt.Fprintln(out, "Interactive mode: --skip-existing is set; existing secret targets are hidden.")
	} else {
		fmt.Fprintln(out, "Interactive mode: unset secret targets are selected by default; toggle set targets to update them.")
	}
	fmt.Fprintln(out, "Leave a value empty to skip every selected target for that secret.")
	fmt.Fprintln(out, "Use --scope to force a single GitHub target, or configure secretStores for provider-specific targets.")
	fmt.Fprintln(out)

	items := buildManifestSecretTargetItems(selectable, a.all)
	selectedIdx, err := prompt.MultiSelect(manifestMultiTargetSelectTitle(a.skipExisting), items)
	if err != nil {
		return err
	}
	selected := manifestSecretTargetsByIndexes(selectable, selectedIdx)
	if len(selected) == 0 {
		fmt.Fprintln(out, "No secret targets selected; nothing to do.")
		return nil
	}

	report, err := runManifestSecretTargetSetup(ctx, selected, valuer, func(name, store string) {
		_ = writeSecretsAuditRecord(name, store) //nolint:errcheck // best-effort audit
	}, true)
	if promptErr != nil && *promptErr != nil {
		return *promptErr
	}
	if err != nil {
		return err
	}
	for _, n := range report.Set {
		fmt.Fprintf(out, "  %s: set\n", n)
	}
	for _, n := range report.Skipped {
		fmt.Fprintf(out, "  %s: skipped (no value provided)\n", n)
	}
	fmt.Fprintln(out, "\nAll done.")
	return nil
}

func buildManifestSecretTargetProviders(a *manifestSetupArgs) ([]manifestSecretTargetProvider, error) {
	configPath := firstConfigPattern(a.configPatterns)
	providers := make([]manifestSecretTargetProvider, 0, 4)
	seen := map[string]bool{}
	add := func(store, label string, provider SecretsProvider) {
		if provider == nil || store == "" || seen[store] {
			return
		}
		if targetLabel := secretProviderTargetLabel(provider); targetLabel != "" {
			label = targetLabel
		}
		seen[store] = true
		providers = append(providers, manifestSecretTargetProvider{
			Store:    store,
			Label:    label,
			Provider: provider,
		})
	}

	repo := ""
	if p, label, err := buildSecretWriter("repo", a.envName, a.org, a.visibility, a.tokenEnv, configPath); err == nil {
		add("github-repo", label, secretsProviderAdapter{p: p})
		repo, _, _ = readGitHubRepoForSecretsSetup(configPath)
	}
	if a.envName != "" {
		if p, label, err := buildSecretWriter("env", a.envName, a.org, a.visibility, a.tokenEnv, configPath); err == nil {
			add("github-env:"+a.envName, label, secretsProviderAdapter{p: p})
		}
	}
	org := strings.TrimSpace(a.org)
	if org == "" {
		if repo == "" {
			repo, _, _ = readGitHubRepoForSecretsSetup(configPath)
		}
		org = githubOwnerFromRepo(repo)
	}
	if org != "" {
		if p, label, err := buildSecretWriter("org", a.envName, org, a.visibility, a.tokenEnv, configPath); err == nil {
			add("github-org:"+org, label, secretsProviderAdapter{p: p})
		}
	}

	if cfg, err := config.LoadFromFile(configPath); err == nil && len(cfg.SecretStores) > 0 {
		providers = providers[:0]
		clear(seen)
		for _, target := range buildConfiguredManifestSecretStoreTargets(cfg) {
			add(target.Store, target.Label, target.Provider)
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("no secret provider targets could be configured from %s; configure GitHub repo/org settings or secretStores", configPath)
	}
	return providers, nil
}

func buildConfiguredManifestSecretStoreTargets(cfg *config.WorkflowConfig) []manifestSecretTargetProvider {
	names := make([]string, 0, len(cfg.SecretStores))
	for name := range cfg.SecretStores {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]manifestSecretTargetProvider, 0, len(names))
	for _, name := range names {
		store := cfg.SecretStores[name]
		providers = append(providers, manifestSecretStoreTargets(name, store, cfg)...)
	}
	return providers
}

func manifestSecretStoreTargets(name string, store *config.SecretStoreConfig, cfg *config.WorkflowConfig) []manifestSecretTargetProvider {
	if store == nil {
		return nil
	}
	providerName := normalizedSecretStoreProvider(store.Provider)
	if providerName == "github" {
		return manifestGitHubSecretStoreTargets(name, store)
	}
	provider, err := getProviderForStore(name, cfg)
	if err != nil {
		return nil
	}
	label := secretProviderTargetLabel(provider)
	if label == "" {
		label = name
	}
	return []manifestSecretTargetProvider{{
		Store:    name,
		Label:    label + " (" + name + ")",
		Provider: provider,
	}}
}

func manifestGitHubSecretStoreTargets(name string, store *config.SecretStoreConfig) []manifestSecretTargetProvider {
	cfg := store.Config
	tokenEnv := stringConfigValue(cfg, "token_env")
	if tokenEnv == "" {
		tokenEnv = "GITHUB_TOKEN"
	}
	var targets []manifestSecretTargetProvider
	if repo := stringConfigValue(cfg, "repo"); repo != "" {
		if p, err := buildGitHubRepoSecretsTarget(repo, tokenEnv); err == nil {
			provider := secretsProviderAdapter{p: p}
			targets = append(targets, manifestSecretTargetProvider{
				Store:    name + ":repo",
				Label:    secretProviderTargetLabel(provider) + " (" + name + ")",
				Provider: provider,
			})
		}
		if env := stringConfigValue(cfg, "environment"); env != "" {
			if p, err := buildGitHubEnvSecretsTarget(repo, env, tokenEnv); err == nil {
				provider := secretsProviderAdapter{p: p}
				targets = append(targets, manifestSecretTargetProvider{
					Store:    name + ":env:" + env,
					Label:    secretProviderTargetLabel(provider) + " (" + name + ")",
					Provider: provider,
				})
			}
		}
	}
	if org := stringConfigValue(cfg, "org"); org != "" {
		visibility := stringConfigValue(cfg, "visibility")
		if visibility == "" {
			visibility = "all"
		}
		if p, err := buildGitHubOrgSecretsTarget(org, visibility, tokenEnv); err == nil {
			provider := secretsProviderAdapter{p: p}
			targets = append(targets, manifestSecretTargetProvider{
				Store:    name + ":org:" + org,
				Label:    fmt.Sprintf("%s (visibility=%s, %s)", secretProviderTargetLabel(provider), visibility, name),
				Provider: provider,
			})
		}
	}
	return targets
}

func buildGitHubRepoSecretsTarget(repo, tokenEnv string) (secrets.Provider, error) {
	return secrets.NewGitHubSecretsProvider(repo, tokenEnv)
}

func buildGitHubEnvSecretsTarget(repo, env, tokenEnv string) (secrets.Provider, error) {
	provider, err := secrets.NewGitHubSecretsProvider(repo, tokenEnv)
	if err != nil {
		return nil, err
	}
	provider.SetEnvironment(env)
	return provider, nil
}

func buildGitHubOrgSecretsTarget(org, visibility, tokenEnv string) (secrets.Provider, error) {
	vis, err := parseGitHubOrgVisibility(visibility)
	if err != nil {
		return nil, err
	}
	return secrets.NewGitHubOrgSecretsProvider(org, tokenEnv, vis, nil)
}

func normalizedSecretStoreProvider(provider string) string {
	switch strings.TrimSpace(provider) {
	case "github-actions":
		return "github"
	case "aws-secrets-manager":
		return "aws"
	default:
		return strings.TrimSpace(provider)
	}
}

func githubOwnerFromRepo(repo string) string {
	owner, _, ok := strings.Cut(strings.TrimSpace(repo), "/")
	if !ok {
		return ""
	}
	return owner
}

func secretProviderTargetLabel(provider SecretsProvider) string {
	describer, ok := provider.(interface{ SecretTarget() secrets.ProviderTarget })
	if !ok {
		return ""
	}
	return strings.TrimSpace(describer.SecretTarget().Label)
}

func queryManifestSecretTargets(ctx context.Context, secrets []manifestDiscoveredSecret, providers []manifestSecretTargetProvider) []manifestSecretTarget {
	targets := make([]manifestSecretTarget, 0, len(secrets)*len(providers))
	for _, secret := range secrets {
		for _, provider := range providers {
			if secret.StoreHint != "" && provider.Store != secret.StoreHint && !strings.HasPrefix(provider.Store, secret.StoreHint+":") {
				continue
			}
			state, err := provider.Provider.Check(ctx, secret.Name)
			status := SecretStatus{
				Name:  secret.Name,
				Store: provider.Store,
				State: state,
				IsSet: state == SecretSet,
			}
			if err != nil {
				status.Error = err.Error()
			}
			targets = append(targets, manifestSecretTarget{
				Secret:   secret,
				Store:    provider.Store,
				Label:    provider.Label,
				Provider: provider.Provider,
				Status:   status,
			})
		}
	}
	return targets
}

func runManifestSecretTargetSetup(ctx context.Context, targets []manifestSecretTarget, valuer func(manifestDiscoveredSecret) (string, bool, error), audit func(string, string), stopOnError bool) (setupReport, error) {
	var report setupReport
	type cachedValue struct {
		value    string
		provided bool
		err      error
	}
	values := make(map[string]cachedValue)
	for _, target := range targets {
		name := target.Secret.Name
		cached, ok := values[name]
		if !ok {
			value, provided, err := valuer(target.Secret)
			cached = cachedValue{value: value, provided: provided, err: err}
			values[name] = cached
		}
		displayName := manifestSecretTargetDisplayName(target)
		if cached.err != nil {
			report.Failed = append(report.Failed, displayName)
			if stopOnError {
				return report, cached.err
			}
			continue
		}
		if !cached.provided {
			report.Skipped = append(report.Skipped, displayName)
			continue
		}
		if err := target.Provider.Set(ctx, name, cached.value); err != nil {
			report.Failed = append(report.Failed, displayName)
			if stopOnError {
				return report, err
			}
			continue
		}
		report.Set = append(report.Set, displayName)
		if audit != nil {
			audit(name, target.Store)
		}
	}
	return report, nil
}

func manifestSecretTargetDisplayName(target manifestSecretTarget) string {
	label := target.Label
	if label == "" {
		label = target.Store
	}
	if label == "" {
		return target.Secret.Name
	}
	return target.Secret.Name + " [" + label + "]"
}

func discoverManifestSecrets(manifestPath, lockfilePath, pluginDir, configPatterns string) ([]manifestDiscoveredSecret, error) {
	plugins, err := discoverManifestPlugins(manifestPath, lockfilePath)
	if err != nil {
		return nil, err
	}
	secretsByName := map[string]*manifestDiscoveredSecret{}
	for _, pluginName := range plugins {
		manifest, err := loadPluginManifest(pluginName, pluginDir)
		if err != nil {
			return nil, err
		}
		sourceName := manifest.Name
		if sourceName == "" {
			sourceName = pluginName
		}
		for _, required := range manifest.RequiredSecrets {
			if strings.TrimSpace(required.Name) == "" {
				continue
			}
			addDiscoveredSecret(secretsByName, required, "plugin:"+sourceName)
		}
	}
	configFiles, err := expandConfigPatterns(configPatterns)
	if err != nil {
		return nil, err
	}
	for _, configFile := range configFiles {
		refs, err := discoverConfigEnvRefs(configFile)
		if err != nil {
			return nil, err
		}
		storeHints := discoverConfigSecretStoreHints(configFile)
		for _, ref := range refs {
			addDiscoveredSecretWithStoreHint(secretsByName, PluginRequiredSecret{
				Name:      ref,
				Sensitive: isSecretSensitive(ref),
			}, "config:"+filepath.Base(configFile), storeHints[ref])
		}
	}
	return sortedManifestSecrets(secretsByName), nil
}

type manifestSecretValueOptions struct {
	interactive bool
	fromEnv     bool
	secretMap   map[string]string
}

func manifestSecretValue(secret manifestDiscoveredSecret, opts manifestSecretValueOptions) (string, bool, error) {
	if opts.fromEnv {
		if v := os.Getenv(secret.Name); v != "" {
			return v, true, nil
		}
	}
	if v, ok := opts.secretMap[secret.Name]; ok {
		return v, true, nil
	}
	if opts.interactive {
		label := secret.Name
		if secret.Description != "" {
			label += " - " + secret.Description
		}
		value, err := prompt.Input(label, secret.Sensitive)
		if err != nil {
			return "", false, err
		}
		if value == "" {
			return "", false, nil
		}
		return value, true, nil
	}
	if opts.fromEnv {
		return "", false, nil
	}
	return "", false, fmt.Errorf("no value for secret %q: set $%s and pass --from-env, use --secret %s=VALUE, or run interactively from a terminal", secret.Name, secret.Name, secret.Name)
}

type manifestSecretSelectionOptions struct {
	onlySet         map[string]bool
	includeExisting bool
	skipExisting    bool
}

func selectManifestSecretsForSetup(secrets []manifestDiscoveredSecret, statuses []SecretStatus, opts manifestSecretSelectionOptions) []manifestDiscoveredSecret {
	statusByName := secretStatusByName(statuses)
	selected := make([]manifestDiscoveredSecret, 0, len(secrets))
	for _, secret := range secrets {
		if len(opts.onlySet) > 0 {
			if !opts.onlySet[secret.Name] {
				continue
			}
		} else if !opts.includeExisting && statusByName[secret.Name].IsSet {
			continue
		}
		if opts.skipExisting && statusByName[secret.Name].IsSet {
			continue
		}
		selected = append(selected, secret)
	}
	return selected
}

func buildManifestMultiSelectItems(secrets []manifestDiscoveredSecret, statuses []SecretStatus, includeExisting bool) []prompt.Item {
	statusByName := secretStatusByName(statuses)
	items := make([]prompt.Item, 0, len(secrets))
	for _, secret := range secrets {
		st := statusByName[secret.Name]
		label := formatStatusLabel(secret.Name, st)
		if len(secret.Sources) > 0 {
			label += " (" + strings.Join(secret.Sources, ", ") + ")"
		}
		items = append(items, prompt.Item{
			Label:       label,
			Preselected: includeExisting || !st.IsSet,
		})
	}
	return items
}

func buildManifestSecretTargetItems(targets []manifestSecretTarget, includeExisting bool) []prompt.Item {
	items := make([]prompt.Item, 0, len(targets))
	for _, target := range targets {
		label := formatStatusLabel(target.Secret.Name, target.Status)
		if target.Label != "" {
			label += " [" + target.Label + "]"
		} else if target.Store != "" {
			label += " [" + target.Store + "]"
		}
		if len(target.Secret.Sources) > 0 {
			label += " (" + strings.Join(target.Secret.Sources, ", ") + ")"
		}
		items = append(items, prompt.Item{
			Label:       label,
			Preselected: includeExisting || !target.Status.IsSet,
		})
	}
	return items
}

func selectManifestSecretTargetsForSetup(targets []manifestSecretTarget, opts manifestSecretSelectionOptions) []manifestSecretTarget {
	selected := make([]manifestSecretTarget, 0, len(targets))
	for _, target := range targets {
		if len(opts.onlySet) > 0 {
			if !opts.onlySet[target.Secret.Name] {
				continue
			}
		} else if !opts.includeExisting && target.Status.IsSet {
			continue
		}
		if opts.skipExisting && target.Status.IsSet {
			continue
		}
		selected = append(selected, target)
	}
	return selected
}

func manifestSecretTargetsByIndexes(targets []manifestSecretTarget, indexes []int) []manifestSecretTarget {
	selected := make([]manifestSecretTarget, 0, len(indexes))
	for _, i := range indexes {
		if i < 0 || i >= len(targets) {
			continue
		}
		selected = append(selected, targets[i])
	}
	return selected
}

func manifestMultiSelectTitle(scopeLabel string, skipExisting bool) string {
	if skipExisting {
		return fmt.Sprintf("Select unset secrets to set for %s (--skip-existing hides existing secrets)", scopeLabel)
	}
	return fmt.Sprintf("Select secrets to set for %s (unset selected by default; toggle set secrets to update)", scopeLabel)
}

func manifestMultiTargetSelectTitle(skipExisting bool) string {
	if skipExisting {
		return "Select unset secret provider targets (--skip-existing hides existing targets)"
	}
	return "Select secret provider targets (unset selected by default; toggle set targets to update)"
}

func secretStatusByName(statuses []SecretStatus) map[string]SecretStatus {
	statusByName := make(map[string]SecretStatus, len(statuses))
	for _, status := range statuses {
		statusByName[status.Name] = status
	}
	return statusByName
}

func manifestSecretsByIndexes(secrets []manifestDiscoveredSecret, indexes []int) []manifestDiscoveredSecret {
	selected := make([]manifestDiscoveredSecret, 0, len(indexes))
	for _, i := range indexes {
		if i < 0 || i >= len(secrets) {
			continue
		}
		selected = append(selected, secrets[i])
	}
	return selected
}

func discoverManifestPlugins(manifestPath, lockfilePath string) ([]string, error) {
	seen := map[string]bool{}
	var plugins []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		plugins = append(plugins, name)
	}
	manifest, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	for _, plugin := range manifest.Plugins {
		add(plugin.Name)
	}
	if lockfilePath != "" {
		lockfile, err := config.LoadWfctlLockfile(lockfilePath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		} else {
			for name := range lockfile.Plugins {
				add(name)
			}
		}
	}
	sort.Strings(plugins)
	return plugins, nil
}

func addDiscoveredSecret(secretsByName map[string]*manifestDiscoveredSecret, required PluginRequiredSecret, source string) {
	addDiscoveredSecretWithStoreHint(secretsByName, required, source, "")
}

func addDiscoveredSecretWithStoreHint(secretsByName map[string]*manifestDiscoveredSecret, required PluginRequiredSecret, source, storeHint string) {
	name := strings.TrimSpace(required.Name)
	if name == "" {
		return
	}
	required.Name = name
	secret, ok := secretsByName[name]
	if !ok {
		secret = &manifestDiscoveredSecret{PluginRequiredSecret: required}
		secretsByName[name] = secret
	}
	if required.Description != "" && secret.Description == "" {
		secret.Description = required.Description
	}
	if storeHint != "" && secret.StoreHint == "" {
		secret.StoreHint = storeHint
	}
	secret.Sensitive = secret.Sensitive || required.Sensitive || isSecretSensitive(name)
	for _, existing := range secret.Sources {
		if existing == source {
			return
		}
	}
	secret.Sources = append(secret.Sources, source)
	sort.Strings(secret.Sources)
}

func sortedManifestSecrets(secretsByName map[string]*manifestDiscoveredSecret) []manifestDiscoveredSecret {
	names := make([]string, 0, len(secretsByName))
	for name := range secretsByName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]manifestDiscoveredSecret, 0, len(names))
	for _, name := range names {
		out = append(out, *secretsByName[name])
	}
	return out
}

func expandConfigPatterns(patterns string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(patterns, ",") {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("expand config pattern %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			if _, err := os.Stat(pattern); err == nil {
				matches = []string{pattern}
			}
		}
		for _, match := range matches {
			if seen[match] {
				continue
			}
			seen[match] = true
			files = append(files, match)
		}
	}
	sort.Strings(files)
	return files, nil
}

func discoverConfigEnvRefs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	refs := map[string]bool{}
	collectEnvRefs(&doc, refs)
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out, nil
}

func discoverConfigSecretStoreHints(path string) map[string]string {
	cfg, err := config.LoadFromFile(path)
	if err != nil || cfg == nil || cfg.Secrets == nil {
		return nil
	}
	hints := make(map[string]string, len(cfg.Secrets.Entries))
	for _, entry := range cfg.Secrets.Entries {
		name := strings.TrimSpace(entry.Name)
		store := strings.TrimSpace(entry.Store)
		if name == "" || store == "" {
			continue
		}
		hints[name] = store
	}
	return hints
}

func collectEnvRefs(node *yaml.Node, refs map[string]bool) {
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode {
		for _, match := range manifestEnvRefPattern.FindAllStringSubmatch(node.Value, -1) {
			if len(match) > 1 {
				refs[match[1]] = true
			}
		}
	}
	for _, child := range node.Content {
		collectEnvRefs(child, refs)
	}
}

func buildSecretLiteralMap(literals []string) (map[string]string, error) {
	secretMap := make(map[string]string)
	for _, lit := range literals {
		k, v, found := strings.Cut(lit, "=")
		if !found {
			return nil, fmt.Errorf("--secret %q: expected NAME=VALUE format", lit)
		}
		secretMap[k] = v
	}
	return secretMap, nil
}

func readKVLines(r io.Reader) []string {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func firstConfigPattern(patterns string) string {
	fallback := ""
	for _, raw := range strings.Split(patterns, ",") {
		if pattern := strings.TrimSpace(raw); pattern != "" {
			if fallback == "" {
				fallback = pattern
			}
			if strings.ContainsAny(pattern, "*?[") {
				matches, err := filepath.Glob(pattern)
				if err != nil {
					continue
				}
				sort.Strings(matches)
				for _, match := range matches {
					if fileExists(match) {
						return match
					}
				}
				continue
			}
			if fileExists(pattern) {
				return pattern
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "app.yaml"
}

func parseManifestSetupFlags(args []string) (*manifestSetupArgs, error) {
	fs := flag.NewFlagSet("secrets setup --manifest", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "wfctl.yaml plugin manifest")
	lockfilePath := fs.String("lock-file", ".wfctl-lock.yaml", "wfctl plugin lockfile")
	pluginDir := fs.String("plugin-dir", "", "Plugin install dir (default: $WFCTL_PLUGIN_DIR or ./data/plugins)")
	configPatterns := fs.String("config", defaultManifestSetupConfigPatterns(), "Workflow config file or comma-separated glob list for env reference discovery")
	scope := fs.String("scope", "repo", "GitHub scope: repo | env | org")
	envName := fs.String("env", "", "Environment name (required with --scope=env)")
	org := fs.String("org", "", "Organization slug (required with --scope=org)")
	visibility := fs.String("visibility", "all", "Org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT")
	fromEnv := fs.Bool("from-env", false, "Read each secret value from $NAME")
	nonInteractive := fs.Bool("non-interactive", false, "Force non-interactive mode for manifest setup")
	onlyFlag := fs.String("only", "", "Comma-separated list of secret names to set")
	allFlag := fs.Bool("all", false, "Set all discovered secrets")
	skipExisting := fs.Bool("skip-existing", false, "Skip secrets that already have a value in the target scope")
	var secretFlag multiStringFlag
	fs.Var(&secretFlag, "secret", "NAME=VALUE literal. Repeatable.")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(*manifestPath) == "" {
		return nil, errors.New("--manifest <wfctl.yaml> is required")
	}
	only, err := parseSecretOnlyList(*onlyFlag)
	if err != nil {
		return nil, err
	}
	if *allFlag && len(only) > 0 {
		return nil, fmt.Errorf("--all and --only are mutually exclusive")
	}
	return &manifestSetupArgs{
		manifestPath:   *manifestPath,
		lockfilePath:   *lockfilePath,
		pluginDir:      *pluginDir,
		configPatterns: *configPatterns,
		scope:          *scope,
		scopeExplicit:  hasFlag(args, "scope"),
		envName:        *envName,
		org:            *org,
		visibility:     *visibility,
		tokenEnv:       *tokenEnv,
		fromEnv:        *fromEnv,
		nonInteractive: *nonInteractive,
		secretLiterals: []string(secretFlag),
		only:           only,
		all:            *allFlag,
		skipExisting:   *skipExisting,
	}, nil
}

func parseSecretOnlyList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
