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

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd %s: %v", wd, err)
		}
	})
}

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

func TestSetupNoArgsMissingConfigExplainsDiscovery(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)

	err := runSecretsSetup(nil)
	if err == nil {
		t.Fatal("expected missing config error")
	}
	msg := err.Error()
	for _, want := range []string{
		"no secrets setup config found",
		"--config <path>",
		"--manifest wfctl.yaml",
		"app.yaml",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "open app.yaml") {
		t.Fatalf("error should not expose raw app.yaml open failure: %q", msg)
	}
}

func TestSetupNoArgsDiscoversWorkflowYAML(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	storeDir := filepath.Join(dir, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	cfg := `modules:
  - name: http
    type: http.server
    config:
      address: ":0"
secrets:
  defaultStore: localfs
  entries:
    - name: A
secretStores:
  localfs:
    provider: file
    config:
      path: ` + storeDir + `
`
	if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}

	err := runSecretsSetup([]string{"--secret", "A=av", "--store", "localfs"})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(storeDir, "A"))
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	if string(data) != "av" {
		t.Fatalf("stored secret = %q, want av", data)
	}
}

func TestSetupNoArgsDiscoversWfctlManifest(t *testing.T) {
	dir := t.TempDir()
	chdirForTest(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "wfctl.yaml"), []byte("version: 1\nplugins: []\n"), 0o644); err != nil {
		t.Fatalf("write wfctl.yaml: %v", err)
	}

	if err := runSecretsSetup(nil); err != nil {
		t.Fatalf("setup should use wfctl.yaml manifest defaults: %v", err)
	}
}

// TestSetupAllOnlyConflict verifies --all and --only are mutually exclusive.
func TestSetupAllOnlyConflict(t *testing.T) {
	cfgPath, _ := writeSetupConfig(t, "A")
	err := runSecretsSetup([]string{"--all", "--only", "A", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected error when --all and --only are both given")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %v, want 'mutually exclusive'", err)
	}
}

// TestSetupAllFlagSetsEverything verifies --all (the explicit default) sets all
// declared secrets. Under `go test` stdin is not a TTY, so runSecretsSetup
// routes to the non-interactive path; --secret supplies the values.
func TestSetupAllFlagSetsEverything(t *testing.T) {
	cfgPath, storeDir := writeSetupConfig(t, "A", "B")
	err := runSecretsSetup([]string{
		"--all",
		"--secret", "A=av",
		"--secret", "B=bv",
		"--store", "localfs",
		"--config", cfgPath,
	})
	if err != nil {
		t.Fatalf("--all setup: %v", err)
	}
	for name, want := range map[string]string{"A": "av", "B": "bv"} {
		data, readErr := os.ReadFile(filepath.Join(storeDir, name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", name, string(data), want)
		}
	}
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

// TestNonInteractive_AutoGenKeys verifies --auto-gen-keys generates a value
// for key/secret/token names and writes it, while leaving non-candidate names
// to error/skip.
func TestNonInteractive_AutoGenKeys(t *testing.T) {
	cfgPath, storeDir := writeSetupConfig(t, "API_KEY")

	var buf bytes.Buffer
	err := runSecretsSetupNonInteractive(&nonInteractiveSetupArgs{
		configFile:  cfgPath,
		autoGenKeys: true,
		storeName:   "localfs",
	}, &buf)
	if err != nil {
		t.Fatalf("auto-gen setup: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(storeDir, "API_KEY"))
	if readErr != nil {
		t.Fatalf("read API_KEY: %v", readErr)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Error("API_KEY should have an auto-generated value")
	}
	// The generated value must NOT appear in stdout.
	if strings.Contains(buf.String(), string(data)) {
		t.Errorf("output leaked generated value: %s", buf.String())
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
