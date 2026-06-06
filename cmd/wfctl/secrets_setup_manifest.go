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
	"github.com/mattn/go-isatty"
	"gopkg.in/yaml.v3"
)

var manifestEnvRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type manifestDiscoveredSecret struct {
	PluginRequiredSecret
	Sources []string
}

type manifestSetupArgs struct {
	manifestPath   string
	lockfilePath   string
	pluginDir      string
	configPatterns string
	scope          string
	envName        string
	org            string
	visibility     string
	tokenEnv       string
	fromEnv        bool
	secretLiterals []string
	only           []string
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

	ghProvider, scopeLabel, err := buildSecretWriter(strings.ToLower(strings.TrimSpace(a.scope)), a.envName, a.org, a.visibility, a.tokenEnv, firstConfigPattern(a.configPatterns))
	if err != nil {
		return err
	}
	provider := secretsProviderAdapter{p: ghProvider}

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
	interactive := in == nil && isatty.IsTerminal(os.Stdin.Fd())

	onlySet := make(map[string]bool, len(a.only))
	for _, name := range a.only {
		onlySet[name] = true
	}
	selector := func(ds []manifestDiscoveredSecret, statuses []SecretStatus) ([]manifestDiscoveredSecret, error) {
		setMap := make(map[string]bool, len(statuses))
		for _, status := range statuses {
			if status.IsSet {
				setMap[status.Name] = true
			}
		}
		var selected []manifestDiscoveredSecret
		for _, secret := range ds {
			if len(onlySet) > 0 && !onlySet[secret.Name] {
				continue
			}
			if a.skipExisting && setMap[secret.Name] {
				continue
			}
			selected = append(selected, secret)
		}
		return selected, nil
	}
	var promptErr error
	valuer := func(secret manifestDiscoveredSecret) (string, bool, error) {
		if a.fromEnv {
			if v := os.Getenv(secret.Name); v != "" {
				return v, true, nil
			}
		}
		if v, ok := secretMap[secret.Name]; ok {
			return v, true, nil
		}
		if interactive {
			label := secret.Name
			if secret.Description != "" {
				label += " - " + secret.Description
			}
			value, err := prompt.Input(label, secret.Sensitive)
			if err != nil {
				if errors.Is(err, prompt.ErrNotInteractive) {
					promptErr = err
				}
				return "", false, err
			}
			if value == "" {
				return "", false, nil
			}
			return value, true, nil
		}
		return "", false, nil
	}
	auditFn := func(name, _ string) {
		_ = writeSecretsAuditRecord(name, "github:"+strings.ToLower(strings.TrimSpace(a.scope))) //nolint:errcheck // best-effort audit
	}

	fmt.Fprintf(out, "Setting up secrets from %s -> %s\n\n", a.manifestPath, scopeLabel)
	for _, secret := range discovered {
		fmt.Fprintf(out, "  %s (%s)\n", secret.Name, strings.Join(secret.Sources, ", "))
	}
	fmt.Fprintln(out)

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
		for _, ref := range refs {
			addDiscoveredSecret(secretsByName, PluginRequiredSecret{
				Name:      ref,
				Sensitive: isSecretSensitive(ref),
			}, "config:"+filepath.Base(configFile))
		}
	}
	return sortedManifestSecrets(secretsByName), nil
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
	for _, raw := range strings.Split(patterns, ",") {
		if pattern := strings.TrimSpace(raw); pattern != "" {
			return pattern
		}
	}
	return "app.yaml"
}

func parseManifestSetupFlags(args []string) (*manifestSetupArgs, error) {
	fs := flag.NewFlagSet("secrets setup --manifest", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "wfctl.yaml plugin manifest")
	lockfilePath := fs.String("lock-file", ".wfctl-lock.yaml", "wfctl plugin lockfile")
	pluginDir := fs.String("plugin-dir", "", "Plugin install dir (default: $WFCTL_PLUGIN_DIR or ./data/plugins)")
	configPatterns := fs.String("config", "app.yaml", "Workflow config file or comma-separated glob list for env reference discovery")
	scope := fs.String("scope", "repo", "GitHub scope: repo | env | org")
	envName := fs.String("env", "", "Environment name (required with --scope=env)")
	org := fs.String("org", "", "Organization slug (required with --scope=org)")
	visibility := fs.String("visibility", "all", "Org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT")
	fromEnv := fs.Bool("from-env", false, "Read each secret value from $NAME")
	nonInteractive := fs.Bool("non-interactive", false, "Accepted for parity with config setup; manifest setup auto-detects input mode")
	onlyFlag := fs.String("only", "", "Comma-separated list of secret names to set")
	allFlag := fs.Bool("all", false, "Set all discovered secrets")
	skipExisting := fs.Bool("skip-existing", false, "Skip secrets that already have a value in the target scope")
	var secretFlag multiStringFlag
	fs.Var(&secretFlag, "secret", "NAME=VALUE literal. Repeatable.")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	_ = nonInteractive
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
		envName:        *envName,
		org:            *org,
		visibility:     *visibility,
		tokenEnv:       *tokenEnv,
		fromEnv:        *fromEnv,
		secretLiterals: []string(secretFlag),
		only:           only,
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
