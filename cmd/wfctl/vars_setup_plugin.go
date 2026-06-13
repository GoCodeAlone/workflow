package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
	"github.com/GoCodeAlone/workflow/secrets"
)

func runVarsSetupPlugin(args []string) error {
	return runVarsSetupPluginWithIO(args, nil, os.Stdout)
}

func runVarsSetupPluginWithIO(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("vars setup", flag.ContinueOnError)
	pluginName := fs.String("plugin", "", "Plugin name (must match a directory under --plugin-dir / $WFCTL_PLUGIN_DIR). When omitted, --config is scanned for config.provider env vars.")
	pluginDir := fs.String("plugin-dir", "", "Plugin install dir (default: $WFCTL_PLUGIN_DIR or ./data/plugins)")
	scope := fs.String("scope", "repo", "GitHub variable scope: repo | env | org")
	envName := fs.String("env", "", "Environment name (required with --scope=env)")
	org := fs.String("org", "", "Organization slug (required with --scope=org)")
	orgVisibility := fs.String("visibility", "private", "Org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT")
	configFile := fs.String("config", "app.yaml", "app.yaml/wfctl.yaml used to resolve the github repo when --scope=repo|env")
	fromEnv := fs.Bool("from-env", false, "Read each variable value from $NAME")
	nonInteractive := fs.Bool("non-interactive", false, "Do not prompt; skip entries without --from-env, --var, or piped KEY=VALUE values")
	var varFlag multiStringFlag
	var nameMapFlag multiStringFlag
	fs.Var(&varFlag, "var", "NAME=VALUE literal. Repeatable.")
	fs.Var(&nameMapFlag, "name-map", "LOGICAL=STORED provider variable name mapping. Repeatable.")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl vars setup [--plugin <name>] [options]

Set non-secret variables declared by either:
  - a plugin's plugin.json required_config[] block when --plugin is supplied, or
  - config vars.entries/variables.entries declarations and config.provider
    schema entries with sensitive:false when --plugin is omitted.

For mixed secret and variable environment input setup, prefer:
  wfctl env setup --manifest wfctl.yaml

Sensitive values belong in required_secrets[] or secrets setup paths and must be
configured with wfctl secrets setup instead.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	nameMappings, err := parseNameMappings(nameMapFlag)
	if err != nil {
		return err
	}
	if *pluginName == "" {
		values, err := valuesFromFlagsAndReader(varFlag, in)
		if err != nil {
			return err
		}
		return runVarsSetupConfig(*configFile, strings.ToLower(strings.TrimSpace(*scope)), *envName, *org, *orgVisibility, *tokenEnv, values, nameMappings, *fromEnv, *nonInteractive || in != nil, out)
	}

	manifest, err := loadPluginManifest(*pluginName, *pluginDir)
	if err != nil {
		return err
	}
	if len(manifest.RequiredConfig) == 0 {
		fmt.Fprintf(out, "plugin %q declares no required_config[]; nothing to do\n", manifest.Name)
		return nil
	}
	for _, cfg := range manifest.RequiredConfig {
		if cfg.Sensitive {
			return fmt.Errorf("required_config %q is marked sensitive; it belongs in required_secrets[]", cfg.Name)
		}
	}

	scopeStr := strings.ToLower(strings.TrimSpace(*scope))
	provider, scopeLabel, err := buildVariableWriter(scopeStr, *envName, *org, *orgVisibility, *tokenEnv, *configFile)
	if err != nil {
		return err
	}
	if !pluginConfigTargetAllowed(manifest.ConfigTargets, provider) {
		desc := variableProviderTarget(provider)
		return fmt.Errorf("plugin %q does not declare config target %s:%s", manifest.Name, desc.Provider, desc.Scope)
	}

	values, err := valuesFromFlagsAndReader(varFlag, in)
	if err != nil {
		return err
	}

	interactive := in == nil && !*nonInteractive && prompt.CanPrompt()
	fmt.Fprintf(out, "Setting up variables for plugin %q -> %s\n\n", manifest.Name, scopeLabel)

	for _, cfg := range manifest.RequiredConfig {
		storedName := mappedSetupName(cfg.Name, nameMappings)
		value, provided, err := pluginConfigValue(cfg, storedName, values, *fromEnv, interactive)
		if err != nil {
			return err
		}
		if !provided {
			fmt.Fprintf(out, "  %s: skipped (no value provided)\n", setupNameDisplay(cfg.Name, storedName))
			continue
		}
		if err := provider.SetVariable(context.Background(), storedName, value); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s: set\n", setupNameDisplay(cfg.Name, storedName))
	}
	fmt.Fprintln(out, "\nAll done.")
	return nil
}

func runVarsSetupConfig(configFile, scopeStr, envName, org, orgVisibility, tokenEnv string, values map[string]string, nameMappings map[string]string, fromEnv, nonInteractive bool, out io.Writer) error {
	entries, skippedSensitive, err := collectConfigVariablesFromFile(configFile)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintf(out, "config %q declares no non-secret config variables; nothing to do\n", configFile)
		if len(skippedSensitive) > 0 {
			fmt.Fprintf(out, "Skipped sensitive config entries; use wfctl secrets setup for: %s\n", strings.Join(skippedSensitive, ", "))
		}
		return nil
	}

	provider, scopeLabel, err := buildVariableWriter(scopeStr, envName, org, orgVisibility, tokenEnv, configFile)
	if err != nil {
		return err
	}

	interactive := !nonInteractive && prompt.CanPrompt()
	fmt.Fprintf(out, "Setting up variables from config %q -> %s\n\n", configFile, scopeLabel)
	if len(skippedSensitive) > 0 {
		fmt.Fprintf(out, "Sensitive config entries skipped; use wfctl secrets setup for: %s\n\n", strings.Join(skippedSensitive, ", "))
	}
	for _, entry := range entries {
		storedName := mappedSetupName(entry.Name, nameMappings)
		value, provided, err := configVariableValue(entry, storedName, values, fromEnv, interactive)
		if err != nil {
			return err
		}
		if !provided {
			fmt.Fprintf(out, "  %s: skipped (no value provided)\n", setupNameDisplay(entry.Name, storedName))
			continue
		}
		if err := provider.SetVariable(context.Background(), storedName, value); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s: set\n", setupNameDisplay(entry.Name, storedName))
	}
	fmt.Fprintln(out, "\nAll done.")
	return nil
}

func buildVariableWriter(scope, envName, org, visibility, tokenEnv, configFile string) (secrets.VariableProvider, string, error) {
	p, label, err := buildSecretWriter(scope, envName, org, visibility, tokenEnv, configFile)
	if err != nil {
		return nil, "", err
	}
	vars, ok := p.(secrets.VariableProvider)
	if !ok {
		return nil, "", fmt.Errorf("provider %q does not support variables", p.Name())
	}
	return vars, strings.Replace(label, "secrets", "variables", 1), nil
}

func buildVariableLiteralMap(literals []string) (map[string]string, error) {
	values := make(map[string]string, len(literals))
	for _, lit := range literals {
		k, v, found := strings.Cut(lit, "=")
		if !found {
			return nil, fmt.Errorf("--var %q: expected NAME=VALUE format", lit)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("--var %q: variable name is required", lit)
		}
		values[k] = v
	}
	return values, nil
}

func valuesFromFlagsAndReader(literals []string, in io.Reader) (map[string]string, error) {
	values, err := buildVariableLiteralMap(literals)
	if err != nil {
		return nil, err
	}
	if in != nil {
		for _, kv := range readKVLines(in) {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			values[k] = strings.TrimSpace(v)
		}
	}
	return values, nil
}

func pluginConfigValue(cfg PluginRequiredConfig, storedName string, literals map[string]string, fromEnv, interactive bool) (string, bool, error) {
	names := setupLookupNames(cfg.Name, storedName)
	if fromEnv {
		for _, name := range names {
			if v := os.Getenv(name); v != "" {
				return v, true, nil
			}
		}
	}
	for _, name := range names {
		if v, ok := literals[name]; ok {
			return v, true, nil
		}
	}
	if !interactive {
		return "", false, nil
	}
	label := cfg.Prompt
	if label == "" {
		label = setupNameDisplay(cfg.Name, storedName)
	}
	if cfg.Description != "" {
		label += " - " + cfg.Description
	}
	value, err := prompt.Input(label, false)
	if err != nil {
		return "", false, err
	}
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func configVariableValue(entry configVariableEntry, storedName string, literals map[string]string, fromEnv, interactive bool) (string, bool, error) {
	names := setupLookupNames(entry.Name, storedName)
	if fromEnv {
		for _, name := range names {
			if v := os.Getenv(name); v != "" {
				return v, true, nil
			}
		}
	}
	for _, name := range names {
		if v, ok := literals[name]; ok {
			return v, true, nil
		}
	}
	if !interactive {
		return "", false, nil
	}
	label := setupNameDisplay(entry.Name, storedName)
	if entry.Description != "" {
		label += " - " + entry.Description
	}
	value, err := prompt.Input(label, false)
	if err != nil {
		return "", false, err
	}
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func mappedSetupName(logical string, mappings map[string]string) string {
	if stored := strings.TrimSpace(mappings[logical]); stored != "" {
		return stored
	}
	return strings.TrimSpace(logical)
}

func setupNameDisplay(logical, stored string) string {
	if strings.TrimSpace(stored) == "" || stored == logical {
		return logical
	}
	return logical + " -> " + stored
}

func setupLookupNames(logical, stored string) []string {
	input := manifestDiscoveredSecret{
		PluginRequiredSecret: PluginRequiredSecret{Name: logical},
		StorageName:          stored,
	}
	return manifestInputValueLookupNames(input)
}

func pluginConfigTargetAllowed(allowed []PluginConfigTarget, provider secrets.VariableProvider) bool {
	if len(allowed) == 0 {
		return true
	}
	target := variableProviderTarget(provider)
	providerName := normalizedSecretTargetProvider(target.Provider)
	scope := strings.ToLower(strings.TrimSpace(target.Scope))
	for _, candidate := range allowed {
		if normalizedSecretTargetProvider(candidate.Provider) != providerName {
			continue
		}
		scopes := normalizedSecretTargetScopes(candidate.Scopes)
		if len(scopes) == 0 {
			return true
		}
		for _, s := range scopes {
			if s == scope {
				return true
			}
		}
	}
	return false
}

func variableProviderTarget(provider secrets.VariableProvider) secrets.ProviderTarget {
	if targeter, ok := provider.(interface{ SecretTarget() secrets.ProviderTarget }); ok {
		return targeter.SecretTarget()
	}
	return secrets.ProviderTarget{Provider: provider.Name(), Scope: "default"}
}
