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

// bootstrapDispatchFake is a minimal IaCProvider stub that records the cfg
// passed to BootstrapStateBackend and returns a pre-configured result.
type bootstrapDispatchFake struct {
	gotCfg map[string]any
	result *interfaces.BootstrapResult
	err    error
	called bool
}

func (p *bootstrapDispatchFake) Name() string    { return "dispatch-test-fake" }
func (p *bootstrapDispatchFake) Version() string { return "0.0.0" }
func (p *bootstrapDispatchFake) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *bootstrapDispatchFake) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (p *bootstrapDispatchFake) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *bootstrapDispatchFake) SupportedCanonicalKeys() []string { return nil }
func (p *bootstrapDispatchFake) BootstrapStateBackend(_ context.Context, cfg map[string]any) (*interfaces.BootstrapResult, error) {
	p.called = true
	p.gotCfg = cfg
	return p.result, p.err
}
func (p *bootstrapDispatchFake) Close() error { return nil }

// TestBootstrapStateBackend_DispatchesToProvider verifies that for a remote
// backend (e.g. "spaces"), bootstrapStateBackend:
//  1. Loads the iac.provider declared in the config via resolveIaCProvider.
//  2. Calls provider.BootstrapStateBackend with the expanded iac.state cfg.
//  3. Prints each entry in result.EnvVars as `export KEY=VALUE` (sorted).
//  4. Writes result.Bucket back to the on-disk config.
func TestBootstrapStateBackend_DispatchesToProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: test-token

  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: test-bucket
      region: nyc3
      accessKey: ak
      secretKey: sk
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fake := &bootstrapDispatchFake{
		result: &interfaces.BootstrapResult{
			Bucket: "test-bucket",
			EnvVars: map[string]string{
				"WFCTL_STATE_BUCKET": "test-bucket",
				"SPACES_BUCKET":      "test-bucket",
			},
		},
	}

	var resolvedProvType string
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, pt string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		resolvedProvType = pt
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	// Capture stdout for export line verification.
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

	// Provider was dispatched with the declared provider type.
	if resolvedProvType != "digitalocean" {
		t.Errorf("resolveIaCProvider called with provider type %q, want %q", resolvedProvType, "digitalocean")
	}

	// BootstrapStateBackend was called with the expanded iac.state cfg.
	if !fake.called {
		t.Fatal("expected BootstrapStateBackend to be called on the provider")
	}
	if got, _ := fake.gotCfg["backend"].(string); got != "spaces" {
		t.Errorf("BootstrapStateBackend cfg[backend]: got %q, want %q", got, "spaces")
	}
	if got, _ := fake.gotCfg["bucket"].(string); got != "test-bucket" {
		t.Errorf("BootstrapStateBackend cfg[bucket]: got %q, want %q", got, "test-bucket")
	}

	// Both env vars are printed as export lines (stable sort order).
	if !strings.Contains(output, "export SPACES_BUCKET=test-bucket") {
		t.Errorf("expected 'export SPACES_BUCKET=test-bucket' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "export WFCTL_STATE_BUCKET=test-bucket") {
		t.Errorf("expected 'export WFCTL_STATE_BUCKET=test-bucket' in output, got:\n%s", output)
	}

	// Bucket name was written back to the on-disk config.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "bucket: test-bucket") {
		t.Errorf("expected 'bucket: test-bucket' in config after write-back, got:\n%s", string(data))
	}
}

// TestBootstrapStateBackend_SelfContainedBackendsSkipped verifies that
// self-contained backends (filesystem, memory, postgres, "") are no-ops:
// bootstrapStateBackend returns nil without calling any provider.
func TestBootstrapStateBackend_SelfContainedBackendsSkipped(t *testing.T) {
	for _, backend := range []string{"filesystem", "memory", "postgres", ""} {
		backend := backend
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "infra.yaml")
			backendVal := backend
			if backendVal == "" {
				backendVal = `""`
			} else {
				backendVal = `"` + backendVal + `"`
			}
			yaml := `modules:
  - name: tf-state
    type: iac.state
    config:
      backend: ` + backendVal + `
      bucket: should-not-matter
`
			if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			called := false
			orig := resolveIaCProvider
			resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
				called = true
				return nil, nil, nil
			}
			defer func() { resolveIaCProvider = orig }()

			if err := bootstrapStateBackend(context.Background(), cfgPath); err != nil {
				t.Fatalf("bootstrapStateBackend(%q): unexpected error: %v", backend, err)
			}
			if called {
				t.Errorf("backend=%q: resolveIaCProvider was called, expected no-op", backend)
			}
		})
	}
}

// TestBootstrapStateBackend_NoProviderModuleErrors verifies that a remote
// backend without an iac.provider module in the config returns a clear error.
func TestBootstrapStateBackend_NoProviderModuleErrors(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: tf-state
    type: iac.state
    config:
      backend: spaces
      bucket: my-bucket
      region: nyc3
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := bootstrapStateBackend(context.Background(), cfgPath)
	if err == nil {
		t.Fatal("expected error when no iac.provider module is declared")
	}
	if !strings.Contains(err.Error(), "iac.provider") {
		t.Errorf("expected 'iac.provider' in error message, got: %v", err)
	}
}
