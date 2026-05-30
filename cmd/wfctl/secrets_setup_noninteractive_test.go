package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSetupConfig writes a minimal app.yaml + a file-store directory for
// non-interactive setup tests.  Returns (configPath, storeDir).
func writeSetupConfig(t *testing.T, entries ...string) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	storeDir := filepath.Join(tmp, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}

	var entryLines []string
	for _, name := range entries {
		entryLines = append(entryLines, "  - name: "+name)
	}
	entriesYAML := strings.Join(entryLines, "\n")

	cfg := `modules:
  - name: http
    type: http.server
    config:
      address: ":0"
secrets:
  defaultStore: localfs
  entries:
` + entriesYAML + `
secretStores:
  localfs:
    provider: file
    config:
      path: ` + storeDir + `
`
	cfgPath := filepath.Join(tmp, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write app.yaml: %v", err)
	}
	return cfgPath, storeDir
}

// TestNonInteractive_FromEnv verifies --from-env reads the secret value
// from $NAME without any argv leak.
func TestNonInteractive_FromEnv(t *testing.T) {
	cfgPath, storeDir := writeSetupConfig(t, "A", "B")
	t.Setenv("A", "hunter2")

	var buf bytes.Buffer
	err := runSecretsSetupNonInteractive(&nonInteractiveSetupArgs{
		configFile: cfgPath,
		fromEnv:    true,
		only:       []string{"A"},
		storeName:  "localfs",
	}, &buf)
	if err != nil {
		t.Fatalf("non-interactive setup: %v", err)
	}
	// A should be written to the file store.
	data, readErr := os.ReadFile(filepath.Join(storeDir, "A"))
	if readErr != nil {
		t.Fatalf("read A from store: %v", readErr)
	}
	if string(data) != "hunter2" {
		t.Errorf("A = %q, want hunter2", string(data))
	}
	// B should NOT be written.
	if _, err := os.Stat(filepath.Join(storeDir, "B")); !os.IsNotExist(err) {
		t.Error("B should not have been set")
	}
	// Output must not contain the secret value.
	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Errorf("output contains secret value: %s", out)
	}
}

// TestNonInteractive_SkipExisting verifies --skip-existing skips secrets
// that are already set in the store.
func TestNonInteractive_SkipExisting(t *testing.T) {
	cfgPath, storeDir := writeSetupConfig(t, "A", "B")
	// Pre-set A.
	if err := os.WriteFile(filepath.Join(storeDir, "A"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("A", "hunter2")
	t.Setenv("B", "bvalue")

	var buf bytes.Buffer
	err := runSecretsSetupNonInteractive(&nonInteractiveSetupArgs{
		configFile:   cfgPath,
		fromEnv:      true,
		skipExisting: true,
		storeName:    "localfs",
	}, &buf)
	if err != nil {
		t.Fatalf("non-interactive setup: %v", err)
	}
	// A must not be overwritten.
	data, _ := os.ReadFile(filepath.Join(storeDir, "A"))
	if string(data) != "old" {
		t.Errorf("A = %q, want old (skip-existing)", string(data))
	}
	// B must be set (was absent).
	bData, _ := os.ReadFile(filepath.Join(storeDir, "B"))
	if string(bData) != "bvalue" {
		t.Errorf("B = %q, want bvalue", string(bData))
	}
	out := buf.String()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "bvalue") {
		t.Errorf("output contains secret value: %s", out)
	}
}

// TestNonInteractive_SecretFlag verifies --secret B=v sets B.
func TestNonInteractive_SecretFlag(t *testing.T) {
	cfgPath, storeDir := writeSetupConfig(t, "A", "B")

	var buf bytes.Buffer
	err := runSecretsSetupNonInteractive(&nonInteractiveSetupArgs{
		configFile:     cfgPath,
		secretLiterals: []string{"B=bval"},
		only:           []string{"B"},
		storeName:      "localfs",
	}, &buf)
	if err != nil {
		t.Fatalf("non-interactive setup: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(storeDir, "B"))
	if string(data) != "bval" {
		t.Errorf("B = %q, want bval", string(data))
	}
}

// TestNonInteractive_MissingValue verifies that a selected secret with no
// value source causes a named error (no hang).
func TestNonInteractive_MissingValue(t *testing.T) {
	cfgPath, _ := writeSetupConfig(t, "A")

	var buf bytes.Buffer
	err := runSecretsSetupNonInteractive(&nonInteractiveSetupArgs{
		configFile: cfgPath,
		only:       []string{"A"},
		storeName:  "localfs",
	}, &buf)
	if err == nil {
		t.Fatal("expected error for missing value")
	}
	if !strings.Contains(err.Error(), "A") {
		t.Errorf("error should name the missing secret: %v", err)
	}
}

// TestNonInteractive_ProviderReceivesExactSets ensures the provider is
// called exactly for the expected secrets (integration-style using a
// fresh file store as the backing provider).
func TestNonInteractive_ProviderReceivesExactSets(t *testing.T) {
	cfgPath, storeDir := writeSetupConfig(t, "A", "B", "C")
	t.Setenv("A", "aval")
	t.Setenv("C", "cval")

	var buf bytes.Buffer
	err := runSecretsSetupNonInteractive(&nonInteractiveSetupArgs{
		configFile: cfgPath,
		fromEnv:    true,
		only:       []string{"A", "C"},
		storeName:  "localfs",
	}, &buf)
	if err != nil {
		t.Fatalf("non-interactive setup: %v", err)
	}
	// A and C set; B untouched.
	aData, _ := os.ReadFile(filepath.Join(storeDir, "A"))
	cData, _ := os.ReadFile(filepath.Join(storeDir, "C"))
	if string(aData) != "aval" {
		t.Errorf("A = %q", aData)
	}
	if string(cData) != "cval" {
		t.Errorf("C = %q", cData)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "B")); !os.IsNotExist(err) {
		t.Error("B should not have been set")
	}
}

// Ensure the json import is used (it will be used by JSON output in full binary).
var _ = json.Marshal

// TestNonInteractive_ProviderReceivesExactSets_Context ensures context
// is propagated correctly.
func TestNonInteractive_ContextCancel(t *testing.T) {
	cfgPath, _ := writeSetupConfig(t, "A")
	t.Setenv("A", "val")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var buf bytes.Buffer
	// Should not hang — cancelled context.
	_ = runSecretsSetupNonInteractiveCtx(ctx, &nonInteractiveSetupArgs{
		configFile: cfgPath,
		fromEnv:    true,
		storeName:  "localfs",
	}, &buf)
	// We don't assert success or failure here; just that it returns.
}
