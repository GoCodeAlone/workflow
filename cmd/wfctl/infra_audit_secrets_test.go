package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInfraAuditSecrets_TwoEntryAntiPattern(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "infra.yaml")
	if err := os.WriteFile(cfg, []byte(`secrets:
  generate:
    - key: SPACES_access_key
      type: provider_credential
      source: digitalocean.spaces
      name: test-key
    - key: SPACES_secret_key
      type: provider_credential
      source: digitalocean.spaces
      name: test-key
`), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	exitCode := runInfraAuditSecrets([]string{"-c", cfg}, &out)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit on anti-pattern; got 0\nout=%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("two-entry provider_credential")) {
		t.Errorf("expected 'two-entry provider_credential' in output; got: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("SPACES_access_key")) {
		t.Errorf("expected offending key 'SPACES_access_key' in output; got: %s", out.String())
	}
}

func TestInfraAuditSecrets_CanonicalShape_Passes(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "infra.yaml")
	if err := os.WriteFile(cfg, []byte(`secrets:
  generate:
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: test-key
`), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	exitCode := runInfraAuditSecrets([]string{"-c", cfg}, &out)
	if exitCode != 0 {
		t.Fatalf("expected zero exit on canonical shape; got %d\nout=%s", exitCode, out.String())
	}
}
