package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

type importCaptureProvider struct {
	stateReturningProvider
	driver        interfaces.ResourceDriver
	importCloudID string
	importType    string
	importState   *interfaces.ResourceState
	importCalled  bool
}

func (p *importCaptureProvider) Import(_ context.Context, cloudID string, resourceType string) (*interfaces.ResourceState, error) {
	p.importCalled = true
	p.importCloudID = cloudID
	p.importType = resourceType
	return p.importState, nil
}

func (p *importCaptureProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

func captureInfraImportStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	runErr := fn()
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String(), runErr
}

func writeImportConfig(t *testing.T, dir string) (string, string) {
	t.Helper()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`

  - name: site-dns
    type: infra.dns
    config:
      provider: do-provider
      domain: example.com
      ttl: 300
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath, stateDir
}

func writeImportConfigWithoutState(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: site-dns
    type: infra.dns
    config:
      provider: do-provider
      domain: example.com
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func TestInfraImport_RequiresRealStateBackend(t *testing.T) {
	cfgPath := writeImportConfigWithoutState(t, t.TempDir())
	fake := &importCaptureProvider{
		importState: &interfaces.ResourceState{ProviderID: "provider-domain-id"},
	}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns", "--id", "provider-domain-id"})
	})
	if err == nil {
		t.Fatal("expected import to fail without a real iac.state backend")
	}
	if !strings.Contains(err.Error(), "iac.state") {
		t.Fatalf("error = %v, want message about iac.state backend", err)
	}
	if fake.importCalled {
		t.Fatal("provider.Import should not be called before validating state backend")
	}
}

func TestInfraImport_ConfigAwareWithIDUsesProviderImport(t *testing.T) {
	cfgPath, _ := writeImportConfig(t, t.TempDir())
	desiredConfig := map[string]any{"provider": "do-provider", "domain": "example.com", "ttl": 300}
	fake := &importCaptureProvider{
		importState: &interfaces.ResourceState{
			ID:            "provider-domain-id",
			Name:          "provider-domain-id",
			Type:          "infra.dns",
			ProviderID:    "provider-domain-id",
			ConfigHash:    configHashMap(map[string]any{"domain": "old.example.com", "ttl": 300}),
			AppliedConfig: map[string]any{"domain": "old.example.com", "ttl": 300},
			Outputs:       map[string]any{"domain": "old.example.com"},
		},
	}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType != "fake-cloud" {
			t.Fatalf("providerType = %q, want fake-cloud", providerType)
		}
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	out, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns", "--id", "provider-domain-id"})
	})
	if err != nil {
		t.Fatalf("runInfraImport: %v", err)
	}
	if !fake.importCalled {
		t.Fatal("provider.Import was not called")
	}
	if fake.importCloudID != "provider-domain-id" || fake.importType != "infra.dns" {
		t.Fatalf("Import args = (%q, %q), want (provider-domain-id, infra.dns)", fake.importCloudID, fake.importType)
	}
	if !strings.Contains(out, `Imported "site-dns"`) || !strings.Contains(out, "provider-domain-id") {
		t.Fatalf("stdout = %q, want concise import success", out)
	}

	states, err := loadCurrentState(cfgPath, "")
	if err != nil {
		t.Fatalf("loadCurrentState: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	got := states[0]
	if got.Name != "site-dns" || got.ID != "site-dns" || got.Type != "infra.dns" {
		t.Fatalf("saved state identity = id:%q name:%q type:%q", got.ID, got.Name, got.Type)
	}
	if got.Provider != "fake-cloud" {
		t.Fatalf("saved provider = %q, want fake-cloud", got.Provider)
	}
	if got.ProviderID != "provider-domain-id" {
		t.Fatalf("saved ProviderID = %q, want provider-domain-id", got.ProviderID)
	}
	if got.AppliedConfig["domain"] != desiredConfig["domain"] || fmt.Sprint(got.AppliedConfig["ttl"]) != fmt.Sprint(desiredConfig["ttl"]) {
		t.Fatalf("saved AppliedConfig = %#v, want desired config", got.AppliedConfig)
	}
	if got.ConfigHash == configHashMap(desiredConfig) {
		t.Fatalf("saved ConfigHash matched desired hash; next apply would not reconcile imported live drift")
	}
}

func TestInfraImport_ConfigAwareWithoutIDReadsByDesiredDomainProviderID(t *testing.T) {
	cfgPath, _ := writeImportConfig(t, t.TempDir())
	driver := &readDriver{
		expectedProviderID: "example.com",
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "domain-by-name",
			Outputs: map[string]any{
				"domain": "example.com",
				"config": map[string]any{"provider": "do-provider", "domain": "example.com", "ttl": 300},
			},
		},
	}
	fake := &importCaptureProvider{driver: driver}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"-c", cfgPath, "--name", "site-dns"})
	})
	if err != nil {
		t.Fatalf("runInfraImport: %v", err)
	}
	if fake.importCalled {
		t.Fatal("provider.Import should not be called without --id")
	}
	if len(driver.reads) != 1 {
		t.Fatalf("driver.Read calls = %d, want 1", len(driver.reads))
	}
	if driver.reads[0].Name != "site-dns" || driver.reads[0].Type != "infra.dns" || driver.reads[0].ProviderID != "example.com" {
		t.Fatalf("driver.Read ref = %+v, want desired name/type with ProviderID example.com", driver.reads[0])
	}

	states, err := loadCurrentState(cfgPath, "")
	if err != nil {
		t.Fatalf("loadCurrentState: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	if states[0].ProviderID != "domain-by-name" {
		t.Fatalf("saved ProviderID = %q, want domain-by-name", states[0].ProviderID)
	}
}

func TestInfraImport_WithoutIDFallsBackToNameProviderIDWhenDomainOmitted(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`

  - name: site-dns
    type: infra.dns
    config:
      provider: do-provider
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	driver := &readDriver{
		expectedProviderID: "site-dns",
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "site-dns",
			Outputs:    map[string]any{"domain": "site-dns"},
		},
	}
	fake := &importCaptureProvider{driver: driver}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns"})
	})
	if err != nil {
		t.Fatalf("runInfraImport: %v", err)
	}
	if len(driver.reads) != 1 {
		t.Fatalf("driver.Read calls = %d, want 1", len(driver.reads))
	}
	if driver.reads[0].ProviderID != "site-dns" {
		t.Fatalf("driver.Read ProviderID = %q, want fallback name site-dns", driver.reads[0].ProviderID)
	}
}

func TestInfraImport_RejectsMalformedImportedProviderID(t *testing.T) {
	cfgPath, _ := writeImportConfig(t, t.TempDir())
	fake := &importCaptureProvider{
		driver: &readDriver{format: interfaces.IDFormatDomainName},
		importState: &interfaces.ResourceState{
			ID:         "not a domain",
			Name:       "not a domain",
			Type:       "infra.dns",
			ProviderID: "not a domain",
			Outputs:    map[string]any{"domain": "example.com"},
		},
	}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns", "--id", "not a domain"})
	})
	if err == nil {
		t.Fatal("expected malformed ProviderID error")
	}
	if !strings.Contains(err.Error(), "malformed ProviderID") || !strings.Contains(err.Error(), "state not persisted") {
		t.Fatalf("error = %v, want strict ProviderID validation failure", err)
	}
	states, loadErr := loadCurrentState(cfgPath, "")
	if loadErr != nil {
		t.Fatalf("loadCurrentState: %v", loadErr)
	}
	if len(states) != 0 {
		t.Fatalf("states = %d, want none saved for malformed ProviderID", len(states))
	}
}

func TestInfraImport_RejectsMalformedReadProviderID(t *testing.T) {
	cfgPath, _ := writeImportConfig(t, t.TempDir())
	driver := &readDriver{
		expectedProviderID: "example.com",
		format:             interfaces.IDFormatDomainName,
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "not a domain",
			Outputs:    map[string]any{"domain": "example.com"},
		},
	}
	fake := &importCaptureProvider{driver: driver}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns"})
	})
	if err == nil {
		t.Fatal("expected malformed ProviderID error")
	}
	if !strings.Contains(err.Error(), "malformed ProviderID") || !strings.Contains(err.Error(), "state not persisted") {
		t.Fatalf("error = %v, want strict ProviderID validation failure", err)
	}
}

func TestInfraImport_ReadNilOutputFails(t *testing.T) {
	cfgPath, _ := writeImportConfig(t, t.TempDir())
	driver := &readDriver{readOut: nil, readErr: nil}
	fake := &importCaptureProvider{driver: driver}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns"})
	})
	if err == nil {
		t.Fatal("expected error when ResourceDriver.Read returns nil output")
	}
	if !strings.Contains(err.Error(), "returned no state") {
		t.Fatalf("error = %v, want nil live state message", err)
	}
}

func TestInfraImport_ReadEmptyProviderIDFails(t *testing.T) {
	cfgPath, _ := writeImportConfig(t, t.TempDir())
	driver := &readDriver{
		readOut: &interfaces.ResourceOutput{
			Name:    "site-dns",
			Type:    "infra.dns",
			Outputs: map[string]any{"domain": "example.com"},
		},
	}
	fake := &importCaptureProvider{driver: driver}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	_, err := captureInfraImportStdout(t, func() error {
		return runInfraImport([]string{"--config", cfgPath, "--name", "site-dns"})
	})
	if err == nil {
		t.Fatal("expected error when imported live output has empty ProviderID")
	}
	if !strings.Contains(err.Error(), "ProviderID") {
		t.Fatalf("error = %v, want ProviderID message", err)
	}
}
