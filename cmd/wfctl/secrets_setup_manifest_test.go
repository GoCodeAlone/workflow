package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

func TestDiscoverManifestSecrets(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")
	configPath := filepath.Join(dir, "infra.yaml")

	if err := os.WriteFile(manifestPath, []byte(`version: 1
plugins:
  - name: workflow-plugin-cloudflare
    version: v1.2.3
`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(`version: 1
generated_at: 2026-06-06T00:00:00Z
plugins:
  namecheap:
    version: v0.4.5
    source: github.com/GoCodeAlone/workflow-plugin-namecheap
`), 0o600); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
	writePluginManifestFile(t, pluginDir, "cloudflare", `{
		"name": "workflow-plugin-cloudflare",
		"required_secrets": [
			{"name": "CLOUDFLARE_API_TOKEN", "sensitive": true, "description": "Cloudflare API token"}
		]
	}`)
	writePluginManifestFile(t, pluginDir, "workflow-plugin-namecheap", `{
		"name": "workflow-plugin-namecheap",
		"required_secrets": [
			{"name": "NAMECHEAP_API_KEY", "sensitive": true}
		],
		"required_config": [
			{"name": "NAMECHEAP_CLIENT_IP", "description": "allowlisted client IP"}
		]
	}`)
	if err := os.WriteFile(configPath, []byte(`providers:
  cloudflare:
    api_token: ${CLOUDFLARE_API_TOKEN}
  extra:
    webhook: ${CONFIG_ONLY_TOKEN}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	secrets, err := discoverManifestSecrets(manifestPath, lockPath, pluginDir, configPath)
	if err != nil {
		t.Fatalf("discoverManifestSecrets: %v", err)
	}
	got := make([]string, 0, len(secrets))
	sources := map[string][]string{}
	for _, secret := range secrets {
		got = append(got, secret.Name)
		sources[secret.Name] = secret.Sources
	}
	want := []string{"CLOUDFLARE_API_TOKEN", "CONFIG_ONLY_TOKEN", "NAMECHEAP_API_KEY", "NAMECHEAP_CLIENT_IP"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("secrets = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(sources["CLOUDFLARE_API_TOKEN"], []string{"config:infra.yaml", "plugin:workflow-plugin-cloudflare"}) {
		t.Fatalf("cloudflare sources = %v", sources["CLOUDFLARE_API_TOKEN"])
	}
	if !reflect.DeepEqual(sources["CONFIG_ONLY_TOKEN"], []string{"config:infra.yaml"}) {
		t.Fatalf("config-only sources = %v", sources["CONFIG_ONLY_TOKEN"])
	}
	kinds := map[string]envSetupInputKind{}
	for _, input := range secrets {
		kinds[input.Name] = input.Kind
	}
	if kinds["NAMECHEAP_API_KEY"] != envSetupInputSecret || kinds["NAMECHEAP_CLIENT_IP"] != envSetupInputVar {
		t.Fatalf("kinds = %+v, want namecheap key secret and client ip var", kinds)
	}
}

func TestDiscoverManifestSecretsPreservesConfiguredStoreHint(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")
	configPath := filepath.Join(dir, "app.yaml")

	if err := os.WriteFile(manifestPath, []byte("version: 1\nplugins: []\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`secretStores:
  aws-prod:
    provider: aws-secrets-manager
    config:
      region: us-east-1
secrets:
  entries:
    - name: AWS_ACCESS_KEY_ID
      store: aws-prod
providers:
  aws:
    access_key_id: ${AWS_ACCESS_KEY_ID}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	secrets, err := discoverManifestSecrets(manifestPath, lockPath, pluginDir, configPath)
	if err != nil {
		t.Fatalf("discoverManifestSecrets: %v", err)
	}
	if len(secrets) != 1 {
		t.Fatalf("secrets = %+v, want one AWS secret", secrets)
	}
	if secrets[0].StoreHint != "aws-prod" {
		t.Fatalf("StoreHint = %q, want aws-prod", secrets[0].StoreHint)
	}
}

func TestParseManifestSetupFlagsAcceptsSetupSelectors(t *testing.T) {
	args, err := parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
		"--non-interactive",
		"--only", "B,A,A",
		"--skip-existing",
		"--from-env",
	})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags: %v", err)
	}
	if !reflect.DeepEqual(args.only, []string{"A", "B"}) {
		t.Fatalf("only = %v", args.only)
	}
	if !args.skipExisting || !args.fromEnv {
		t.Fatalf("flags not preserved: %+v", args)
	}
	if !args.nonInteractive {
		t.Fatalf("--non-interactive was not preserved: %+v", args)
	}
}

func TestParseManifestSetupFlagsPreservesAll(t *testing.T) {
	args, err := parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
		"--all",
		"--verbose",
	})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags: %v", err)
	}
	if !args.all {
		t.Fatalf("--all was not preserved: %+v", args)
	}
	if !args.verbose {
		t.Fatalf("--verbose was not preserved: %+v", args)
	}
}

func TestParseManifestSetupFlagsTracksExplicitScope(t *testing.T) {
	args, err := parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
	})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags: %v", err)
	}
	if args.scopeExplicit {
		t.Fatalf("default scope should not be treated as explicit: %+v", args)
	}

	args, err = parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
		"--scope", "org",
	})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags: %v", err)
	}
	if !args.scopeExplicit {
		t.Fatalf("--scope flag was not tracked as explicit: %+v", args)
	}
}

func TestParseManifestSetupFlagsAcceptsKind(t *testing.T) {
	args, err := parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
		"--kind", "var",
	})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags: %v", err)
	}
	if args.kind != envSetupInputVar {
		t.Fatalf("kind = %q, want var", args.kind)
	}

	args, err = parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
		"--kind", "all",
	})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags all: %v", err)
	}
	if args.kind != "" {
		t.Fatalf("kind = %q, want all/empty", args.kind)
	}
}

func TestParseManifestSetupFlagsRejectsInvalidKind(t *testing.T) {
	_, err := parseManifestSetupFlags([]string{
		"--manifest", "wfctl.yaml",
		"--kind", "token",
	})
	if err == nil {
		t.Fatal("expected invalid kind error")
	}
	if !strings.Contains(err.Error(), "--kind") || !strings.Contains(err.Error(), "secret|var|all") {
		t.Fatalf("error = %q, want kind guidance", err)
	}
}

func TestManifestSecretValueNonInteractiveRequiresValueSource(t *testing.T) {
	secret := manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"}}
	got, provided, err := manifestSecretValue(secret, manifestSecretValueOptions{
		interactive: false,
		fromEnv:     false,
		secretMap:   map[string]string{},
	})
	if err == nil {
		t.Fatalf("expected missing value error, got value=%q provided=%v", got, provided)
	}
	msg := err.Error()
	for _, want := range []string{"DIGITALOCEAN_TOKEN", "--from-env", "--secret DIGITALOCEAN_TOKEN=VALUE"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
}

func TestManifestSecretValueNonInteractiveMentionsMappedLookupFallback(t *testing.T) {
	secret := manifestDiscoveredSecret{
		PluginRequiredSecret: PluginRequiredSecret{Name: "NAMECHEAP_API_KEY"},
		StorageName:          "GCA_NC_API_KEY",
	}
	got, provided, err := manifestSecretValue(secret, manifestSecretValueOptions{
		interactive: false,
		fromEnv:     false,
		secretMap:   map[string]string{},
	})
	if err == nil {
		t.Fatalf("expected missing value error, got value=%q provided=%v", got, provided)
	}
	msg := err.Error()
	for _, want := range []string{"NAMECHEAP_API_KEY", "GCA_NC_API_KEY", "--secret GCA_NC_API_KEY=VALUE", "--secret NAMECHEAP_API_KEY=VALUE"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
}

func TestManifestSecretValueFromEnvMissingSkips(t *testing.T) {
	secret := manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"}}
	got, provided, err := manifestSecretValue(secret, manifestSecretValueOptions{
		interactive: false,
		fromEnv:     true,
		secretMap:   map[string]string{},
	})
	if err != nil {
		t.Fatalf("manifestSecretValue: %v", err)
	}
	if provided || got != "" {
		t.Fatalf("got value=%q provided=%v, want skipped empty value", got, provided)
	}
}

func TestDiscoverManifestSecretsFailsOnConfigVariableCollectionError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")
	configPath := filepath.Join(dir, "app.yaml")

	if err := os.WriteFile(manifestPath, []byte("version: 1\nplugins: []\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("version: 1\nplugins: {}\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`modules:
  - name: app-config
    type: config.provider
    config:
      sources:
        - type: env
      schema:
        password: not-a-map
providers:
  app:
    password: ${PASSWORD}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := discoverManifestSecrets(manifestPath, lockPath, pluginDir, configPath)
	if err == nil {
		t.Fatal("expected config variable collection error")
	}
	if !strings.Contains(err.Error(), "collect config variables") {
		t.Fatalf("error = %q, want config collection context", err)
	}
}

func TestBuildManifestMultiSelectItemsShowsStatusSourcesAndDefaults(t *testing.T) {
	secrets := []manifestDiscoveredSecret{
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"},
			Sources:              []string{"config:digitalocean.wfctl.yaml", "plugin:workflow-plugin-digitalocean"},
		},
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "HOVER_PASSWORD"},
			Sources:              []string{"config:hover.wfctl.yaml", "plugin:workflow-plugin-hover"},
		},
	}
	statuses := []SecretStatus{
		{Name: "DIGITALOCEAN_TOKEN", State: SecretSet, IsSet: true},
		{Name: "HOVER_PASSWORD", State: SecretNotSet, IsSet: false},
	}

	items := buildManifestMultiSelectItems(secrets, statuses, false)
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if items[0].Preselected {
		t.Fatalf("set secret was preselected: %+v", items[0])
	}
	if !items[1].Preselected {
		t.Fatalf("unset secret was not preselected: %+v", items[1])
	}
	for _, want := range []string{"DIGITALOCEAN_TOKEN", "✓ set", "config:digitalocean.wfctl.yaml", "plugin:workflow-plugin-digitalocean"} {
		if !strings.Contains(items[0].Label, want) {
			t.Fatalf("set label %q does not contain %q", items[0].Label, want)
		}
	}
	for _, want := range []string{"HOVER_PASSWORD", "✗ unset", "config:hover.wfctl.yaml", "plugin:workflow-plugin-hover"} {
		if !strings.Contains(items[1].Label, want) {
			t.Fatalf("unset label %q does not contain %q", items[1].Label, want)
		}
	}
}

func TestBuildManifestSecretTargetItemsShowProviderScopes(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"},
				Sources:              []string{"config:digitalocean.wfctl.yaml"},
			},
			Store: "github-repo",
			Label: "github repo GoCodeAlone/gocodealone-dns",
			Status: SecretStatus{
				Name:  "DIGITALOCEAN_TOKEN",
				Store: "github-repo",
				State: SecretNotSet,
				IsSet: false,
			},
		},
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"},
				Sources:              []string{"config:digitalocean.wfctl.yaml"},
			},
			Store: "github-org",
			Label: "github org GoCodeAlone",
			Status: SecretStatus{
				Name:  "DIGITALOCEAN_TOKEN",
				Store: "github-org",
				State: SecretSet,
				IsSet: true,
			},
		},
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "AWS_ACCESS_KEY_ID"},
				Sources:              []string{"config:aws.wfctl.yaml"},
			},
			Store: "aws-prod",
			Label: "aws secrets-manager aws-prod",
			Status: SecretStatus{
				Name:  "AWS_ACCESS_KEY_ID",
				Store: "aws-prod",
				State: SecretNotSet,
				IsSet: false,
			},
		},
	}

	items := buildManifestSecretTargetItems(targets, false)
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	if !items[0].Preselected {
		t.Fatalf("unset repo target should be preselected: %+v", items[0])
	}
	if items[1].Preselected {
		t.Fatalf("set org target should not be preselected: %+v", items[1])
	}
	if !items[2].Preselected {
		t.Fatalf("unset AWS target should be preselected: %+v", items[2])
	}
	for _, want := range []string{"DIGITALOCEAN_TOKEN", "github repo GoCodeAlone/gocodealone-dns", "✗ unset"} {
		if !strings.Contains(items[0].Label, want) {
			t.Fatalf("repo target label %q does not contain %q", items[0].Label, want)
		}
	}
	for _, want := range []string{"DIGITALOCEAN_TOKEN", "github org GoCodeAlone", "✓ set"} {
		if !strings.Contains(items[1].Label, want) {
			t.Fatalf("org target label %q does not contain %q", items[1].Label, want)
		}
	}
	for _, want := range []string{"AWS_ACCESS_KEY_ID", "aws secrets-manager aws-prod", "✗ unset"} {
		if !strings.Contains(items[2].Label, want) {
			t.Fatalf("AWS target label %q does not contain %q", items[2].Label, want)
		}
	}
}

func TestManifestSecretTargetScopeLabelUsesProviderDomain(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "GITHUB_TOKEN"}},
			Store:  "github-repo",
			Label:  "github repo GoCodeAlone/example",
		},
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "GITHUB_TOKEN"}},
			Store:  "github-env:production",
			Label:  "github env production in GoCodeAlone/example",
		},
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "DOTENV_TOKEN"}},
			Store:  "dotenv",
			Label:  "file .env",
		},
	}

	got := []string{
		manifestSecretTargetScopeLabel(targets[0]),
		manifestSecretTargetScopeLabel(targets[1]),
		manifestSecretTargetScopeLabel(targets[2]),
	}
	want := []string{"github:repo", "github:env", "file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scope labels = %v, want %v", got, want)
	}
	for _, label := range got[:2] {
		if strings.Contains(label, "local") {
			t.Fatalf("github scope label leaked local semantics: %q", label)
		}
	}
}

func TestBuildManifestSecretTargetProvidersUsesYAMLDeclaredEnvironments(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	t.Setenv("GITHUB_TOKEN", "stub")
	configPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(configPath, []byte(`modules: []
secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow
environments:
  staging: {}
ci:
  deploy:
    environments:
      production: {}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	providers, err := buildManifestSecretTargetProviders(&manifestSetupArgs{
		configPatterns: configPath,
		envName:        "local",
		envExplicit:    false,
		visibility:     "all",
		tokenEnv:       "GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("buildManifestSecretTargetProviders: %v", err)
	}

	stores := manifestTargetProviderStores(providers)
	want := []string{"github-env:production", "github-env:staging", "github-org:GoCodeAlone", "github-repo"}
	if !reflect.DeepEqual(stores, want) {
		t.Fatalf("stores = %v, want %v", stores, want)
	}
}

func TestBuildManifestSecretTargetProvidersDoesNotUseDefaultLocalEnvironment(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	t.Setenv("GITHUB_TOKEN", "stub")
	configPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(configPath, []byte(`modules: []
secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	providers, err := buildManifestSecretTargetProviders(&manifestSetupArgs{
		configPatterns: configPath,
		envName:        "local",
		envExplicit:    false,
		visibility:     "all",
		tokenEnv:       "GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("buildManifestSecretTargetProviders: %v", err)
	}

	stores := manifestTargetProviderStores(providers)
	want := []string{"github-org:GoCodeAlone", "github-repo"}
	if !reflect.DeepEqual(stores, want) {
		t.Fatalf("stores = %v, want %v", stores, want)
	}
}

func TestBuildManifestSecretTargetProvidersUsesExplicitEnvironment(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	t.Setenv("GITHUB_TOKEN", "stub")
	configPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(configPath, []byte(`modules: []
secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	providers, err := buildManifestSecretTargetProviders(&manifestSetupArgs{
		configPatterns: configPath,
		envName:        "preview",
		envExplicit:    true,
		visibility:     "all",
		tokenEnv:       "GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("buildManifestSecretTargetProviders: %v", err)
	}

	stores := manifestTargetProviderStores(providers)
	want := []string{"github-env:preview", "github-org:GoCodeAlone", "github-repo"}
	if !reflect.DeepEqual(stores, want) {
		t.Fatalf("stores = %v, want %v", stores, want)
	}
}

func TestManifestSetupEnvNameRequiresExplicitEnvFlag(t *testing.T) {
	tests := []struct {
		name     string
		envName  string
		explicit bool
		want     string
	}{
		{name: "default local ignored", envName: "local", explicit: false, want: ""},
		{name: "explicit local kept", envName: "local", explicit: true, want: "local"},
		{name: "explicit preview kept", envName: "preview", explicit: true, want: "preview"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := manifestSetupEnvName(tt.envName, tt.explicit); got != tt.want {
				t.Fatalf("manifestSetupEnvName(%q, %v) = %q, want %q", tt.envName, tt.explicit, got, tt.want)
			}
		})
	}
}

func TestBuildManifestSecretTargetProvidersSkipsRuntimeEnvironmentPlaceholder(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	t.Setenv("GITHUB_TOKEN", "stub")
	configPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(configPath, []byte(`modules: []
secretStores:
  github:
    provider: github
    config:
      repo: GoCodeAlone/workflow
      environment: ${WORKFLOW_ENV}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	providers, err := buildManifestSecretTargetProviders(&manifestSetupArgs{
		configPatterns: configPath,
		visibility:     "all",
		tokenEnv:       "GITHUB_TOKEN",
	})
	if err != nil {
		t.Fatalf("buildManifestSecretTargetProviders: %v", err)
	}

	stores := manifestTargetProviderStores(providers)
	want := []string{"github:repo"}
	if !reflect.DeepEqual(stores, want) {
		t.Fatalf("stores = %v, want %v", stores, want)
	}
}

func manifestTargetProviderStores(providers []manifestSecretTargetProvider) []string {
	stores := make([]string, 0, len(providers))
	for _, provider := range providers {
		stores = append(stores, provider.Store)
	}
	sort.Strings(stores)
	return stores
}

func TestPreflightManifestSecretTargetEnvironmentsCreatesMissingInteractiveOnce(t *testing.T) {
	provider := &manifestEnvironmentTestProvider{env: "staging"}
	targets := []manifestSecretTarget{
		{
			Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN_A"}},
			Store:    "github-env:staging",
			Provider: provider,
		},
		{
			Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN_B"}},
			Store:    "github-env:staging",
			Provider: provider,
		},
	}
	var confirms int

	err := preflightManifestSecretTargetEnvironments(context.Background(), targets, manifestEnvironmentPreflightOptions{
		Interactive: true,
		Confirm: func(question string, def bool) (bool, error) {
			confirms++
			if !strings.Contains(question, "staging") {
				t.Fatalf("confirm question = %q, want environment name", question)
			}
			if !def {
				t.Fatalf("confirm default = false, want true")
			}
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if confirms != 1 || provider.ensureCalls != 1 {
		t.Fatalf("confirms=%d ensureCalls=%d, want one each", confirms, provider.ensureCalls)
	}
}

func TestPreflightManifestSecretTargetEnvironmentsFailsMissingNonInteractive(t *testing.T) {
	provider := &manifestEnvironmentTestProvider{env: "production"}
	targets := []manifestSecretTarget{{
		Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN_A"}},
		Store:    "github-env:production",
		Provider: provider,
	}}

	err := preflightManifestSecretTargetEnvironments(context.Background(), targets, manifestEnvironmentPreflightOptions{})
	if err == nil {
		t.Fatal("expected missing environment error")
	}
	if !strings.Contains(err.Error(), "production") || !strings.Contains(err.Error(), "non-interactive") {
		t.Fatalf("error = %q, want environment and non-interactive guidance", err)
	}
	if provider.ensureCalls != 0 {
		t.Fatalf("ensureCalls = %d, want 0", provider.ensureCalls)
	}
}

type manifestEnvironmentTestProvider struct {
	env           string
	exists        bool
	validateCalls int
	ensureCalls   int
}

func (p *manifestEnvironmentTestProvider) Get(context.Context, string) (string, error) {
	return "", secrets.ErrUnsupported
}

func (p *manifestEnvironmentTestProvider) Set(context.Context, string, string) error {
	return nil
}

func (p *manifestEnvironmentTestProvider) Check(context.Context, string) (SecretState, error) {
	return SecretNotSet, nil
}

func (p *manifestEnvironmentTestProvider) List(context.Context) ([]SecretStatus, error) {
	return nil, nil
}

func (p *manifestEnvironmentTestProvider) Delete(context.Context, string) error {
	return nil
}

func (p *manifestEnvironmentTestProvider) SecretTarget() secrets.ProviderTarget {
	subject := p.env + " on GoCodeAlone/workflow"
	return secrets.ProviderTarget{
		Provider: "github",
		Scope:    "env",
		Subject:  subject,
		Label:    "github env " + subject,
	}
}

func (p *manifestEnvironmentTestProvider) Environment() string {
	return p.env
}

func (p *manifestEnvironmentTestProvider) ListEnvironments(context.Context) ([]secrets.ProviderEnvironment, error) {
	return nil, nil
}

func (p *manifestEnvironmentTestProvider) ValidateEnvironment(context.Context, string) (secrets.ProviderEnvironment, error) {
	p.validateCalls++
	if !p.exists {
		return secrets.ProviderEnvironment{}, secrets.ErrNotFound
	}
	return secrets.ProviderEnvironment{Provider: "github", Name: p.env, Exists: true}, nil
}

func (p *manifestEnvironmentTestProvider) EnsureEnvironment(context.Context, string) (secrets.ProviderEnvironment, error) {
	p.ensureCalls++
	p.exists = true
	return secrets.ProviderEnvironment{Provider: "github", Name: p.env, Exists: true}, nil
}

func TestBuildManifestSecretMatrixRowsSummarizesTargets(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"},
				Sources:              []string{"config:digitalocean.wfctl.yaml", "plugin:workflow-plugin-digitalocean"},
			},
			Store: "github-repo",
			Label: "github repo GoCodeAlone/example",
			Status: SecretStatus{
				Name:  "DIGITALOCEAN_TOKEN",
				Store: "github-repo",
				State: SecretNotSet,
			},
		},
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"},
				Sources:              []string{"config:digitalocean.wfctl.yaml", "plugin:workflow-plugin-digitalocean"},
			},
			Store: "github-org:GoCodeAlone",
			Label: "github org GoCodeAlone",
			Status: SecretStatus{
				Name:  "DIGITALOCEAN_TOKEN",
				Store: "github-org:GoCodeAlone",
				State: SecretSet,
				IsSet: true,
			},
		},
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "HOVER_PASSWORD"},
				Sources:              []string{"config:hover.wfctl.yaml", "plugin:workflow-plugin-hover"},
			},
			Store: "github-repo",
			Label: "github repo GoCodeAlone/example",
			Status: SecretStatus{
				Name:  "HOVER_PASSWORD",
				Store: "github-repo",
				State: SecretSet,
				IsSet: true,
			},
		},
		{
			Secret: manifestDiscoveredSecret{
				PluginRequiredSecret: PluginRequiredSecret{Name: "HOVER_PASSWORD"},
				Sources:              []string{"config:hover.wfctl.yaml", "plugin:workflow-plugin-hover"},
			},
			Store: "github-org:GoCodeAlone",
			Label: "github org GoCodeAlone",
			Status: SecretStatus{
				Name:  "HOVER_PASSWORD",
				Store: "github-org:GoCodeAlone",
				State: SecretSet,
				IsSet: true,
			},
		},
	}

	cols, rows, grouped := buildManifestSecretMatrixRows(targets, false, false)
	if len(cols) != 4 {
		t.Fatalf("cols = %+v, want input + kind + 2 scopes", cols)
	}
	if cols[1].Title != "Kind" || cols[2].Title != "github:repo" || cols[3].Title != "github:org" {
		t.Fatalf("cols = %+v, want github scope headers", cols)
	}
	if len(rows) != 2 || len(grouped) != 2 {
		t.Fatalf("rows=%d grouped=%d, want 2", len(rows), len(grouped))
	}
	if rows[0].Cells[0] != "DIGITALOCEAN_TOKEN" || rows[0].Cells[1] != "secret" || rows[0].Cells[2] != "○" || rows[0].Cells[3] != "✓" {
		t.Fatalf("first row = %+v", rows[0].Cells)
	}
	if !rows[0].Preselected {
		t.Fatalf("unset row should be preselected: %+v", rows[0])
	}
	if rows[1].Preselected {
		t.Fatalf("all-set row should not be preselected: %+v", rows[1])
	}
	for _, cell := range rows[0].Cells {
		if strings.Contains(cell, "config:") || strings.Contains(cell, "plugin:") || strings.Contains(cell, "GoCodeAlone/example") {
			t.Fatalf("default matrix row is too verbose: %+v", rows[0].Cells)
		}
	}

	_, verboseRows, _ := buildManifestSecretMatrixRows(targets, false, true)
	if !strings.Contains(strings.Join(verboseRows[0].Cells, " "), "config:digitalocean.wfctl.yaml") {
		t.Fatalf("verbose row lacks source detail: %+v", verboseRows[0].Cells)
	}
}

func TestBuildManifestSecretMatrixRowsDisambiguatesDuplicateScopes(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:  "gh-a:repo",
			Label:  "github repo GoCodeAlone/app-a",
			Status: SecretStatus{
				State: SecretNotSet,
			},
		},
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:  "gh-b:repo",
			Label:  "github repo GoCodeAlone/app-b",
			Status: SecretStatus{
				State: SecretSet,
				IsSet: true,
			},
		},
	}

	cols, rows, _ := buildManifestSecretMatrixRows(targets, false, false)
	if len(cols) != 4 {
		t.Fatalf("cols = %+v, want input + kind + two repo targets", cols)
	}
	if cols[2].Title == cols[3].Title {
		t.Fatalf("duplicate compact scopes were not disambiguated: %+v", cols)
	}
	if !strings.HasPrefix(cols[2].Title, "github:repo:") || !strings.HasPrefix(cols[3].Title, "github:repo:") {
		t.Fatalf("cols = %+v, want compact disambiguated github repo columns", cols)
	}
	if rows[0].Cells[1] != "secret" || rows[0].Cells[2] != "○" || rows[0].Cells[3] != "✓" {
		t.Fatalf("row = %+v, want per-target status preserved", rows[0].Cells)
	}
}

func TestBuildManifestSecretMatrixRowsKeepsTruncatedSubjectsUnique(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:  "gh-a:repo",
			Label:  "github repo GoCodeAlone/repository-with-shared-prefix-alpha",
			Status: SecretStatus{
				State: SecretNotSet,
			},
		},
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:  "gh-b:repo",
			Label:  "github repo GoCodeAlone/repository-with-shared-prefix-beta",
			Status: SecretStatus{
				State: SecretSet,
				IsSet: true,
			},
		},
	}

	cols, rows, _ := buildManifestSecretMatrixRows(targets, false, false)
	if len(cols) != 4 {
		t.Fatalf("cols = %+v, want input + kind + two disambiguated repo targets", cols)
	}
	if cols[2].Title == cols[3].Title {
		t.Fatalf("truncated duplicate subjects collapsed into one matrix column: %+v", cols)
	}
	if rows[0].Cells[1] != "secret" || rows[0].Cells[2] != "○" || rows[0].Cells[3] != "✓" {
		t.Fatalf("row = %+v, want per-target statuses retained", rows[0].Cells)
	}
}

func TestBuildManifestTargetItemsAreCompactByDefault(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Store: "github-repo",
			Label: "github repo GoCodeAlone/gocodealone-dns (inferred from git remote.origin.url)",
			Status: SecretStatus{
				State: SecretNotSet,
			},
		},
		{
			Store: "github-org:GoCodeAlone",
			Label: "github org GoCodeAlone",
			Status: SecretStatus{
				State: SecretSet,
				IsSet: true,
			},
		},
	}

	items := buildManifestTargetItems(targets, false, false)
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if items[0].Label != "github:repo  unset" {
		t.Fatalf("compact label = %q", items[0].Label)
	}
	if items[1].Label != "github:org   set" {
		t.Fatalf("compact label = %q", items[1].Label)
	}
	if strings.Contains(items[0].Label, "inferred") || strings.Contains(items[0].Label, "gocodealone-dns") {
		t.Fatalf("compact target label is too verbose: %q", items[0].Label)
	}

	verbose := buildManifestTargetItems(targets, false, true)
	if !strings.Contains(verbose[0].Label, "inferred from git remote.origin.url") {
		t.Fatalf("verbose target label lacks detail: %q", verbose[0].Label)
	}
}

func TestRunManifestSecretTargetSetupWithValuesSupportsPerTargetValues(t *testing.T) {
	repoProvider := newEngineTestProvider(nil)
	orgProvider := newEngineTestProvider(nil)
	targets := []manifestSecretTarget{
		{
			Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:    "github-repo",
			Label:    "github repo GoCodeAlone/example",
			Provider: repoProvider,
		},
		{
			Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:    "github-org:GoCodeAlone",
			Label:    "github org GoCodeAlone",
			Provider: orgProvider,
		},
	}
	values := map[string]manifestProvidedSecretValue{
		manifestSecretTargetKey(targets[0]): {Value: "repo-value", Provided: true},
		manifestSecretTargetKey(targets[1]): {Value: "org-value", Provided: true},
	}

	report, err := runManifestSecretTargetSetupWithValues(context.Background(), targets, values, nil, true)
	if err != nil {
		t.Fatalf("runManifestSecretTargetSetupWithValues: %v", err)
	}
	if repoProvider.data["TOKEN"] != "repo-value" {
		t.Fatalf("repo value = %q", repoProvider.data["TOKEN"])
	}
	if orgProvider.data["TOKEN"] != "org-value" {
		t.Fatalf("org value = %q", orgProvider.data["TOKEN"])
	}
	if len(report.Set) != 2 {
		t.Fatalf("report.Set = %v", report.Set)
	}
}

func TestCollectManifestSecretTargetValuesAllowsDifferentPerTargetValue(t *testing.T) {
	targets := []manifestSecretTarget{
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN", Sensitive: true}},
			Store:  "github-repo",
			Label:  "github repo GoCodeAlone/example",
		},
		{
			Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN", Sensitive: true}},
			Store:  "github-org:GoCodeAlone",
			Label:  "github org GoCodeAlone",
		},
	}
	inputs := []string{"repo-value", "org-value"}
	confirmCalls := 0
	values, err := collectManifestSecretTargetValues(targets,
		func(manifestDiscoveredSecret) (string, bool, error) { return "", false, nil },
		manifestTargetValuePrompt{
			input: func(label string, masked bool) (string, error) {
				if !masked {
					t.Fatalf("secret prompt was not masked for %q", label)
				}
				if !strings.Contains(label, "TOKEN for github:") {
					t.Fatalf("input label = %q, want target-specific scope", label)
				}
				v := inputs[0]
				inputs = inputs[1:]
				return v, nil
			},
			confirm: func(question string, def bool) (bool, error) {
				confirmCalls++
				if !strings.Contains(question, "github:org") || !def {
					t.Fatalf("confirm question=%q def=%v", question, def)
				}
				return false, nil
			},
		})
	if err != nil {
		t.Fatalf("collectManifestSecretTargetValues: %v", err)
	}
	if confirmCalls != 1 {
		t.Fatalf("confirm calls = %d, want 1", confirmCalls)
	}
	if got := values[manifestSecretTargetKey(targets[0])].Value; got != "repo-value" {
		t.Fatalf("repo value = %q", got)
	}
	if got := values[manifestSecretTargetKey(targets[1])].Value; got != "org-value" {
		t.Fatalf("org value = %q", got)
	}
}

func TestCollectManifestSecretTargetValuesUsesPreprovidedValueForAllTargets(t *testing.T) {
	targets := []manifestSecretTarget{
		{Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}}, Store: "github-repo"},
		{Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}}, Store: "github-org:GoCodeAlone"},
	}
	values, err := collectManifestSecretTargetValues(targets,
		func(manifestDiscoveredSecret) (string, bool, error) { return "env-value", true, nil },
		manifestTargetValuePrompt{
			input: func(string, bool) (string, error) {
				t.Fatal("input should not be called when value is preprovided")
				return "", nil
			},
			confirm: func(string, bool) (bool, error) {
				t.Fatal("confirm should not be called when value is preprovided")
				return false, nil
			},
		})
	if err != nil {
		t.Fatalf("collectManifestSecretTargetValues: %v", err)
	}
	for _, target := range targets {
		got := values[manifestSecretTargetKey(target)]
		if !got.Provided || got.Value != "env-value" {
			t.Fatalf("value for %+v = %+v", target, got)
		}
	}
}

func TestSelectManifestSecretTargetsSkipExistingPerTarget(t *testing.T) {
	targets := []manifestSecretTarget{
		{Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}}, Store: "repo", Status: SecretStatus{Name: "TOKEN", Store: "repo", IsSet: false}},
		{Secret: manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}}, Store: "org", Status: SecretStatus{Name: "TOKEN", Store: "org", IsSet: true}},
	}

	selected := selectManifestSecretTargetsForSetup(targets, manifestSecretSelectionOptions{
		includeExisting: true,
		skipExisting:    true,
	})
	if len(selected) != 1 || selected[0].Store != "repo" {
		t.Fatalf("selected targets = %+v, want only unset repo target", selected)
	}

	selected = selectManifestSecretTargetsForSetup(targets, manifestSecretSelectionOptions{
		includeExisting: true,
	})
	if len(selected) != 2 {
		t.Fatalf("selected targets len = %d, want both targets", len(selected))
	}
}

func TestQueryManifestSecretTargetsUsesProviderStatusPerTarget(t *testing.T) {
	secrets := []manifestDiscoveredSecret{
		{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
	}
	providers := []manifestSecretTargetProvider{
		{
			Store:    "github-repo",
			Label:    "github repo GoCodeAlone/example",
			Provider: newEngineTestProvider(nil),
		},
		{
			Store:    "github-org",
			Label:    "github org GoCodeAlone",
			Provider: newEngineTestProvider(map[string]string{"TOKEN": "set"}),
		},
	}

	targets := queryManifestSecretTargets(context.Background(), secrets, providers)
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(targets))
	}
	if targets[0].Status.IsSet {
		t.Fatalf("repo target status = %+v, want unset", targets[0].Status)
	}
	if !targets[1].Status.IsSet {
		t.Fatalf("org target status = %+v, want set", targets[1].Status)
	}
}

func TestQueryManifestSecretTargetsHonorsSecretStoreHint(t *testing.T) {
	secrets := []manifestDiscoveredSecret{
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "AWS_ACCESS_KEY_ID"},
			StoreHint:            "aws-prod",
		},
	}
	providers := []manifestSecretTargetProvider{
		{
			Store:    "github-repo",
			Label:    "github repo GoCodeAlone/example",
			Provider: newEngineTestProvider(nil),
		},
		{
			Store:    "aws-prod",
			Label:    "aws-secrets-manager aws-prod",
			Provider: newEngineTestProvider(nil),
		},
	}

	targets := queryManifestSecretTargets(context.Background(), secrets, providers)
	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want only configured AWS target", targets)
	}
	if targets[0].Store != "aws-prod" {
		t.Fatalf("target store = %q, want aws-prod", targets[0].Store)
	}
	if strings.Contains(targets[0].Label, "repo") || strings.Contains(targets[0].Label, "org") {
		t.Fatalf("AWS target leaked GitHub label semantics: %q", targets[0].Label)
	}
}

func TestQueryManifestSecretTargetsFiltersByPluginTargetCapabilities(t *testing.T) {
	discovered := []manifestDiscoveredSecret{
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "GITHUB_APP_PRIVATE_KEY"},
			SecretTargets: []PluginSecretTarget{
				{Provider: "github", Scopes: []string{"repo", "env"}},
			},
		},
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "VAULT_TOKEN"},
			SecretTargets: []PluginSecretTarget{
				{Provider: "vault", Scopes: []string{"mount"}},
			},
		},
	}
	providers := []manifestSecretTargetProvider{
		{
			Store:    "github-repo",
			Label:    "github repo GoCodeAlone/workflow",
			Provider: targetEngineTestProvider{engineTestProvider: newEngineTestProvider(nil), target: secrets.ProviderTarget{Provider: "github", Scope: "repo"}},
		},
		{
			Store:    "github-org",
			Label:    "github org GoCodeAlone",
			Provider: targetEngineTestProvider{engineTestProvider: newEngineTestProvider(nil), target: secrets.ProviderTarget{Provider: "github", Scope: "org"}},
		},
		{
			Store:    "vault-prod",
			Label:    "vault prod",
			Provider: targetEngineTestProvider{engineTestProvider: newEngineTestProvider(nil), target: secrets.ProviderTarget{Provider: "vault", Scope: "mount"}},
		},
	}

	targets := queryManifestSecretTargets(context.Background(), discovered, providers)
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want github repo + vault only", targets)
	}
	if targets[0].Secret.Name != "GITHUB_APP_PRIVATE_KEY" || targets[0].Store != "github-repo" {
		t.Fatalf("first target = %+v, want GitHub repo", targets[0])
	}
	if targets[1].Secret.Name != "VAULT_TOKEN" || targets[1].Store != "vault-prod" {
		t.Fatalf("second target = %+v, want Vault", targets[1])
	}
}

func TestQueryManifestSecretTargetsStoreHintOverridesPluginTargets(t *testing.T) {
	discovered := []manifestDiscoveredSecret{
		{
			PluginRequiredSecret: PluginRequiredSecret{Name: "SHARED_TOKEN"},
			StoreHint:            "vault-prod",
			SecretTargets: []PluginSecretTarget{
				{Provider: "github", Scopes: []string{"repo"}},
			},
		},
	}
	providers := []manifestSecretTargetProvider{
		{
			Store:    "github-repo",
			Label:    "github repo GoCodeAlone/workflow",
			Provider: targetEngineTestProvider{engineTestProvider: newEngineTestProvider(nil), target: secrets.ProviderTarget{Provider: "github", Scope: "repo"}},
		},
		{
			Store:    "vault-prod",
			Label:    "vault prod",
			Provider: targetEngineTestProvider{engineTestProvider: newEngineTestProvider(nil), target: secrets.ProviderTarget{Provider: "vault", Scope: "mount"}},
		},
	}

	targets := queryManifestSecretTargets(context.Background(), discovered, providers)
	if len(targets) != 1 || targets[0].Store != "vault-prod" {
		t.Fatalf("targets = %+v, want explicit vault store hint", targets)
	}
}

type targetEngineTestProvider struct {
	*engineTestProvider
	target secrets.ProviderTarget
}

func (p targetEngineTestProvider) SecretTarget() secrets.ProviderTarget {
	return p.target
}

func TestRunManifestSecretTargetSetupWritesEachSelectedProvider(t *testing.T) {
	repoProvider := newEngineTestProvider(nil)
	orgProvider := newEngineTestProvider(nil)
	targets := []manifestSecretTarget{
		{
			Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:    "github-repo",
			Label:    "github repo GoCodeAlone/example",
			Provider: repoProvider,
		},
		{
			Secret:   manifestDiscoveredSecret{PluginRequiredSecret: PluginRequiredSecret{Name: "TOKEN"}},
			Store:    "github-org",
			Label:    "github org GoCodeAlone",
			Provider: orgProvider,
		},
	}
	valueCalls := 0
	var audited []string

	report, err := runManifestSecretTargetSetup(context.Background(), targets,
		func(secret manifestDiscoveredSecret) (string, bool, error) {
			valueCalls++
			return "value", true, nil
		},
		func(name, store string) {
			audited = append(audited, name+"@"+store)
		},
		true,
	)
	if err != nil {
		t.Fatalf("runManifestSecretTargetSetup: %v", err)
	}
	if valueCalls != 1 {
		t.Fatalf("valuer calls = %d, want one prompt reused for both targets", valueCalls)
	}
	if repoProvider.data["TOKEN"] != "value" || orgProvider.data["TOKEN"] != "value" {
		t.Fatalf("providers not both set: repo=%q org=%q", repoProvider.data["TOKEN"], orgProvider.data["TOKEN"])
	}
	if !reflect.DeepEqual(report.Set, []string{"TOKEN [github repo GoCodeAlone/example]", "TOKEN [github org GoCodeAlone]"}) {
		t.Fatalf("report.Set = %v", report.Set)
	}
	if !reflect.DeepEqual(audited, []string{"TOKEN@github-repo", "TOKEN@github-org"}) {
		t.Fatalf("audited = %v", audited)
	}
}

func TestManifestMultiSelectTitleRespectsSkipExisting(t *testing.T) {
	normal := manifestMultiSelectTitle("github repo GoCodeAlone/example", false)
	if !strings.Contains(normal, "toggle set secrets to update") {
		t.Fatalf("normal title = %q, want update hint", normal)
	}
	skipExisting := manifestMultiSelectTitle("github repo GoCodeAlone/example", true)
	if strings.Contains(skipExisting, "toggle set secrets") {
		t.Fatalf("skip-existing title = %q, must not offer toggling hidden set secrets", skipExisting)
	}
	for _, want := range []string{"--skip-existing", "hides existing secrets"} {
		if !strings.Contains(skipExisting, want) {
			t.Fatalf("skip-existing title = %q, want %q", skipExisting, want)
		}
	}
}

func TestSelectManifestSecretsForSetupDefaultsToUnsetButCanUpdateAll(t *testing.T) {
	secrets := []manifestDiscoveredSecret{
		{PluginRequiredSecret: PluginRequiredSecret{Name: "DIGITALOCEAN_TOKEN"}, Kind: envSetupInputSecret},
		{PluginRequiredSecret: PluginRequiredSecret{Name: "HOVER_PASSWORD"}, Kind: envSetupInputSecret},
	}
	statuses := []SecretStatus{
		{Name: "DIGITALOCEAN_TOKEN", State: SecretSet, IsSet: true},
		{Name: "HOVER_PASSWORD", State: SecretNotSet, IsSet: false},
	}

	selected := selectManifestSecretsForSetup(secrets, statuses, manifestSecretSelectionOptions{})
	if got := manifestSecretNames(selected); !reflect.DeepEqual(got, []string{"HOVER_PASSWORD"}) {
		t.Fatalf("default selected = %v, want only unset secret", got)
	}

	selected = selectManifestSecretsForSetup(secrets, statuses, manifestSecretSelectionOptions{includeExisting: true})
	if got := manifestSecretNames(selected); !reflect.DeepEqual(got, []string{"DIGITALOCEAN_TOKEN", "HOVER_PASSWORD"}) {
		t.Fatalf("includeExisting selected = %v, want all secrets", got)
	}

	selected = selectManifestSecretsForSetup(secrets, statuses, manifestSecretSelectionOptions{
		includeExisting: true,
		skipExisting:    true,
	})
	if got := manifestSecretNames(selected); !reflect.DeepEqual(got, []string{"HOVER_PASSWORD"}) {
		t.Fatalf("skipExisting selected = %v, want only unset secret", got)
	}

	selected = selectManifestSecretsForSetup(secrets, statuses, manifestSecretSelectionOptions{
		onlySet: map[string]bool{"DIGITALOCEAN_TOKEN": true},
	})
	if got := manifestSecretNames(selected); !reflect.DeepEqual(got, []string{"DIGITALOCEAN_TOKEN"}) {
		t.Fatalf("explicit only selected = %v, want selected set secret", got)
	}
}

func TestSelectManifestSecretsForSetupFiltersByKind(t *testing.T) {
	inputs := []manifestDiscoveredSecret{
		{PluginRequiredSecret: PluginRequiredSecret{Name: "API_TOKEN"}, Kind: envSetupInputSecret},
		{PluginRequiredSecret: PluginRequiredSecret{Name: "ACCOUNT_ID"}, Kind: envSetupInputVar},
		{PluginRequiredSecret: PluginRequiredSecret{Name: "LEGACY_SECRET"}},
	}
	statuses := []SecretStatus{
		{Name: "API_TOKEN", State: SecretNotSet},
		{Name: "ACCOUNT_ID", State: SecretNotSet},
		{Name: "LEGACY_SECRET", State: SecretNotSet},
	}

	selected := selectManifestSecretsForSetup(inputs, statuses, manifestSecretSelectionOptions{
		kind:            envSetupInputSecret,
		includeExisting: true,
	})
	if got := manifestSecretNames(selected); !reflect.DeepEqual(got, []string{"API_TOKEN", "LEGACY_SECRET"}) {
		t.Fatalf("secret kind selected = %v, want API_TOKEN and legacy secret default", got)
	}

	selected = selectManifestSecretsForSetup(inputs, statuses, manifestSecretSelectionOptions{
		kind:            envSetupInputVar,
		includeExisting: true,
	})
	if got := manifestSecretNames(selected); !reflect.DeepEqual(got, []string{"ACCOUNT_ID"}) {
		t.Fatalf("var kind selected = %v, want ACCOUNT_ID", got)
	}
}

func manifestSecretNames(secrets []manifestDiscoveredSecret) []string {
	names := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		names = append(names, secret.Name)
	}
	return names
}

func TestParseManifestSetupFlagsDefaultsConfigPatterns(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "infra"), 0o755); err != nil {
		t.Fatalf("mkdir infra: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "infra", "dns.wfctl.yaml"), []byte("resources: []\n"), 0o644); err != nil {
		t.Fatalf("write infra config: %v", err)
	}

	args, err := parseManifestSetupFlags([]string{"--manifest", "wfctl.yaml"})
	if err != nil {
		t.Fatalf("parseManifestSetupFlags: %v", err)
	}
	if args.configPatterns != "infra/*.wfctl.yaml" {
		t.Fatalf("configPatterns = %q, want infra/*.wfctl.yaml", args.configPatterns)
	}
}

func TestRunSecretsSetupRejectsAutoGenKeysForManifestTarget(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "wfctl.yaml"), []byte("version: 1\nplugins: []\n"), 0o644); err != nil {
		t.Fatalf("write wfctl.yaml: %v", err)
	}

	err := runSecretsSetup([]string{"--auto-gen-keys"})
	if err == nil {
		t.Fatal("expected --auto-gen-keys manifest error")
	}
	msg := err.Error()
	for _, want := range []string{"--auto-gen-keys", "wfctl.yaml", "--from-env"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
}

func TestFirstConfigPatternResolvesGlobToExistingFile(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "infra"), 0o755); err != nil {
		t.Fatalf("mkdir infra: %v", err)
	}
	for _, name := range []string{"b.wfctl.yaml", "a.wfctl.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, "infra", name), []byte("resources: []\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got := firstConfigPattern("missing.yaml,infra/*.wfctl.yaml")
	if got != filepath.Join("infra", "a.wfctl.yaml") {
		t.Fatalf("firstConfigPattern = %q, want infra/a.wfctl.yaml", got)
	}
}

func TestBuildSecretWriterRepoScopeFallsBackToGitRemote(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	runGitForTest(t, dir, "init")
	runGitForTest(t, dir, "remote", "add", "origin", "git@github.com:GoCodeAlone/gocodealone-dns.git")
	t.Setenv("GITHUB_TOKEN", "stub")

	if err := os.MkdirAll(filepath.Join(dir, "infra"), 0o755); err != nil {
		t.Fatalf("mkdir infra: %v", err)
	}
	configPath := filepath.Join(dir, "infra", "digitalocean.wfctl.yaml")
	if err := os.WriteFile(configPath, []byte(`modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, label, err := buildSecretWriter("repo", "", "", "all", "GITHUB_TOKEN", configPath)
	if err != nil {
		t.Fatalf("buildSecretWriter: %v", err)
	}
	if !strings.Contains(label, "GoCodeAlone/gocodealone-dns") {
		t.Fatalf("label = %q, want git remote repo", label)
	}
	if !strings.Contains(label, "inferred from git remote.origin.url") {
		t.Fatalf("label = %q, want inference source", label)
	}
}

func TestBuildSecretWriterRepoScopeErrorIncludesInferredRepo(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	runGitForTest(t, dir, "init")
	runGitForTest(t, dir, "remote", "add", "origin", "https://github.com/GoCodeAlone/gocodealone-dns.git")
	t.Setenv("GITHUB_TOKEN", "")

	if err := os.MkdirAll(filepath.Join(dir, "infra"), 0o755); err != nil {
		t.Fatalf("mkdir infra: %v", err)
	}
	configPath := filepath.Join(dir, "infra", "digitalocean.wfctl.yaml")
	if err := os.WriteFile(configPath, []byte(`modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := buildSecretWriter("repo", "", "", "all", "GITHUB_TOKEN", configPath)
	if err == nil {
		t.Fatal("expected missing token error")
	}
	msg := err.Error()
	for _, want := range []string{
		"GoCodeAlone/gocodealone-dns",
		"inferred from git remote.origin.url",
		`env var "GITHUB_TOKEN"`,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
}

func TestReadGitHubRepoFromConfigUsesSecretStores(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(configPath, []byte(`modules: []
secrets:
  defaultStore: github
  entries:
    - name: TOKEN
secretStores:
  github:
    provider: github
    config:
      repo: GoCodeAlone/workflow
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := readGitHubRepoFromAppYAML(configPath)
	if err != nil {
		t.Fatalf("readGitHubRepoFromAppYAML: %v", err)
	}
	if got != "GoCodeAlone/workflow" {
		t.Fatalf("repo = %q, want GoCodeAlone/workflow", got)
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
