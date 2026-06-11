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
	orgVisibility := fs.String("visibility", "all", "Org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT")
	configFile := fs.String("config", "app.yaml", "app.yaml/wfctl.yaml used to resolve the github repo when --scope=repo|env")
	fromEnv := fs.Bool("from-env", false, "Read each variable value from $NAME")
	nonInteractive := fs.Bool("non-interactive", false, "Do not prompt; skip entries without --from-env, --var, or piped KEY=VALUE values")
	var varFlag multiStringFlag
	fs.Var(&varFlag, "var", "NAME=VALUE literal. Repeatable.")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl vars setup [--plugin <name>] [options]

Set non-secret variables declared by either:
  - a plugin's plugin.json required_config[] block when --plugin is supplied, or
  - config.provider schema entries with sensitive:false when --plugin is omitted.

Sensitive values belong in required_secrets[] or secrets setup paths and must be
configured with wfctl secrets setup instead.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pluginName == "" {
		values, err := valuesFromFlagsAndReader(varFlag, in)
		if err != nil {
			return err
		}
		return runVarsSetupConfig(*configFile, strings.ToLower(strings.TrimSpace(*scope)), *envName, *org, *orgVisibility, *tokenEnv, values, *fromEnv, *nonInteractive, out)
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

	values, err := buildVariableLiteralMap(varFlag)
	if err != nil {
		return err
	}
	if in != nil {
		for _, kv := range readKVLines(in) {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				values[k] = v
			}
		}
	}

	interactive := in == nil && !*nonInteractive && prompt.CanPrompt()
	fmt.Fprintf(out, "Setting up variables for plugin %q -> %s\n\n", manifest.Name, scopeLabel)

	for _, cfg := range manifest.RequiredConfig {
		value, provided, err := pluginConfigValue(cfg, values, *fromEnv, interactive)
		if err != nil {
			return err
		}
		if !provided {
			fmt.Fprintf(out, "  %s: skipped (no value provided)\n", cfg.Name)
			continue
		}
		if err := provider.SetVariable(context.Background(), cfg.Name, value); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s: set\n", cfg.Name)
	}
	fmt.Fprintln(out, "\nAll done.")
	return nil
}

func runVarsSetupConfig(configFile, scopeStr, envName, org, orgVisibility, tokenEnv string, values map[string]string, fromEnv, nonInteractive bool, out io.Writer) error {
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
		value, provided, err := configVariableValue(entry, values, fromEnv, interactive)
		if err != nil {
			return err
		}
		if !provided {
			fmt.Fprintf(out, "  %s: skipped (no value provided)\n", entry.Name)
			continue
		}
		if err := provider.SetVariable(context.Background(), entry.Name, value); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s: set\n", entry.Name)
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
			if ok {
				values[k] = v
			}
		}
	}
	return values, nil
}

func pluginConfigValue(cfg PluginRequiredConfig, literals map[string]string, fromEnv, interactive bool) (string, bool, error) {
	if fromEnv {
		if v := os.Getenv(cfg.Name); v != "" {
			return v, true, nil
		}
	}
	if v, ok := literals[cfg.Name]; ok {
		return v, true, nil
	}
	if !interactive {
		return "", false, nil
	}
	label := cfg.Prompt
	if label == "" {
		label = cfg.Name
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

func configVariableValue(entry configVariableEntry, literals map[string]string, fromEnv, interactive bool) (string, bool, error) {
	if fromEnv {
		if v := os.Getenv(entry.Name); v != "" {
			return v, true, nil
		}
	}
	if v, ok := literals[entry.Name]; ok {
		return v, true, nil
	}
	if !interactive {
		return "", false, nil
	}
	label := entry.Name
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
