package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	want := []string{"CLOUDFLARE_API_TOKEN", "CONFIG_ONLY_TOKEN", "NAMECHEAP_API_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("secrets = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(sources["CLOUDFLARE_API_TOKEN"], []string{"config:infra.yaml", "plugin:workflow-plugin-cloudflare"}) {
		t.Fatalf("cloudflare sources = %v", sources["CLOUDFLARE_API_TOKEN"])
	}
	if !reflect.DeepEqual(sources["CONFIG_ONLY_TOKEN"], []string{"config:infra.yaml"}) {
		t.Fatalf("config-only sources = %v", sources["CONFIG_ONLY_TOKEN"])
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
