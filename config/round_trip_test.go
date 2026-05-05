package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/GoCodeAlone/workflow/config"
)

// TestWorkflowConfig_RoundTrip_PreservesSecretsGenerateAndInfra is a regression
// test that verifies secrets.generate[] and infra.auto_bootstrap survive a full
// round-trip through config.WorkflowConfig (load → marshal → reload).
//
// Previously, these fields were only declared on cmd/wfctl-local structs and not
// reflected in config.WorkflowConfig, so writeEnvResolvedConfig (which marshals
// via WorkflowConfig) silently dropped them. Moving SecretGen/InfraConfig into
// the config package fixes the round-trip.
func TestWorkflowConfig_RoundTrip_PreservesSecretsGenerateAndInfra(t *testing.T) {
	const src = `
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: my-state

secrets:
  provider: github
  generate:
    - key: JWT_SECRET
      type: random_hex
      length: 32
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces

infra:
  auto_bootstrap: true
`
	// Write the source YAML to a temp file.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	// Load through config.LoadFromFile (the same path used by writeEnvResolvedConfig).
	cfg, err := config.LoadFromFile(srcPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	// Assert fields are present in the loaded struct.
	if cfg.Secrets == nil {
		t.Fatal("cfg.Secrets is nil after load")
	}
	if len(cfg.Secrets.Generate) != 2 {
		t.Fatalf("cfg.Secrets.Generate: want 2 entries, got %d", len(cfg.Secrets.Generate))
	}
	if cfg.Secrets.Generate[0].Key != "JWT_SECRET" {
		t.Errorf("Generate[0].Key: want %q, got %q", "JWT_SECRET", cfg.Secrets.Generate[0].Key)
	}
	if cfg.Secrets.Generate[1].Source != "digitalocean.spaces" {
		t.Errorf("Generate[1].Source: want %q, got %q", "digitalocean.spaces", cfg.Secrets.Generate[1].Source)
	}
	if cfg.Infra == nil {
		t.Fatal("cfg.Infra is nil after load")
	}
	if cfg.Infra.AutoBootstrap == nil || !*cfg.Infra.AutoBootstrap {
		t.Error("cfg.Infra.AutoBootstrap: want true, got nil or false")
	}

	// Marshal back to YAML — this is what writeEnvResolvedConfig does.
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Write marshalled YAML to a new temp file and reload.
	roundTripPath := filepath.Join(dir, "roundtrip.yaml")
	if err := os.WriteFile(roundTripPath, out, 0o600); err != nil {
		t.Fatalf("write roundtrip: %v", err)
	}
	cfg2, err := config.LoadFromFile(roundTripPath)
	if err != nil {
		t.Fatalf("LoadFromFile (roundtrip): %v", err)
	}

	// Assert both fields survived the round-trip.
	if cfg2.Secrets == nil {
		t.Fatal("cfg2.Secrets is nil after round-trip")
	}
	if len(cfg2.Secrets.Generate) != 2 {
		t.Errorf("after round-trip: Generate: want 2 entries, got %d (generate[] was dropped)", len(cfg2.Secrets.Generate))
	}
	if cfg2.Infra == nil {
		t.Fatal("cfg2.Infra is nil after round-trip (infra section was dropped)")
	}
	if cfg2.Infra.AutoBootstrap == nil || !*cfg2.Infra.AutoBootstrap {
		t.Error("after round-trip: Infra.AutoBootstrap: want true, got nil or false")
	}
}
