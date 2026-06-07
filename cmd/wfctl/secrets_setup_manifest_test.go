package main

import (
	"os"
	"path/filepath"
	"reflect"
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
