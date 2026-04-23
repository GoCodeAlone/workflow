package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// urlBootstrapProvider is a minimal IaCProvider stub for URL-export tests.
type urlBootstrapProvider struct {
	result *interfaces.BootstrapResult
	err    error
}

func (p *urlBootstrapProvider) Name() string    { return "url-test-fake" }
func (p *urlBootstrapProvider) Version() string { return "0.0.0" }
func (p *urlBootstrapProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *urlBootstrapProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (p *urlBootstrapProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *urlBootstrapProvider) SupportedCanonicalKeys() []string { return nil }
func (p *urlBootstrapProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return p.result, p.err
}
func (p *urlBootstrapProvider) Close() error { return nil }

// withURLBootstrapFake overrides resolveIaCProvider for the duration of the test.
func withURLBootstrapFake(t *testing.T, result *interfaces.BootstrapResult) {
	t.Helper()
	fake := &urlBootstrapProvider{result: result}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })
}

// TestBootstrapStateBackend_PrintsExportLine verifies that after a successful
// bootstrap, bootstrapStateBackend prints both `export WFCTL_STATE_BUCKET=<name>`
// and `export SPACES_BUCKET=<name>` to stdout for CI capture.
func TestBootstrapStateBackend_PrintsExportLine(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean

  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: bmw-iac-state-test
      region: nyc3
      accessKey: key123
      secretKey: secret456
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	withURLBootstrapFake(t, &interfaces.BootstrapResult{
		Bucket: "bmw-iac-state-test",
		EnvVars: map[string]string{
			"WFCTL_STATE_BUCKET": "bmw-iac-state-test",
			"SPACES_BUCKET":      "bmw-iac-state-test",
		},
	})

	// Capture stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := bootstrapStateBackend(context.Background(), cfgPath)

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	output := buf.String()

	if err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	if !strings.Contains(output, "export WFCTL_STATE_BUCKET=bmw-iac-state-test") {
		t.Errorf("expected 'export WFCTL_STATE_BUCKET=bmw-iac-state-test' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "export SPACES_BUCKET=bmw-iac-state-test") {
		t.Errorf("expected 'export SPACES_BUCKET=bmw-iac-state-test' in output, got:\n%s", output)
	}
}

// TestBootstrapStateBackend_WritesBucketBackToConfig verifies that after a
// successful bootstrap, the resolved bucket name returned by the provider is
// written back to the on-disk config so downstream commands can load it
// without the env var being set again.
func TestBootstrapStateBackend_WritesBucketBackToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	initialYAML := `modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean

  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: original-bucket
      region: nyc3
      accessKey: key
      secretKey: sec
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Provider returns a resolved bucket name (may differ from config value).
	withURLBootstrapFake(t, &interfaces.BootstrapResult{
		Bucket: "resolved-bucket",
		EnvVars: map[string]string{
			"WFCTL_STATE_BUCKET": "resolved-bucket",
		},
	})

	// Suppress stdout output.
	oldStdout := os.Stdout
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	defer func() { os.Stdout = oldStdout; devNull.Close() }()

	if err := bootstrapStateBackend(context.Background(), cfgPath); err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "bucket: resolved-bucket") {
		t.Errorf("expected 'bucket: resolved-bucket' written back to config, got:\n%s", string(data))
	}
}

// TestWriteBucketBackToConfig_ReplacesField verifies that writeBucketBackToConfig
// replaces the bucket: field inside an iac.state module block without altering
// other fields or YAML structure.
func TestWriteBucketBackToConfig_ReplacesField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	yaml := `modules:
  - name: other-module
    type: infra.database
    config:
      bucket: not-this-one

  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: ${OLD_BUCKET}
      region: nyc3
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := writeBucketBackToConfig(cfgPath, "new-bucket"); err != nil {
		t.Fatalf("writeBucketBackToConfig: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	result := string(data)

	// The iac.state bucket field must be updated.
	if !strings.Contains(result, "bucket: new-bucket") {
		t.Errorf("expected 'bucket: new-bucket' in result, got:\n%s", result)
	}
	// The infra.database bucket field must be untouched.
	if !strings.Contains(result, "bucket: not-this-one") {
		t.Errorf("expected 'bucket: not-this-one' (other module) to remain, got:\n%s", result)
	}
}
