package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestRunInfraPlan_FailsLoudOnPluginLoadFailure verifies T3.6b's BREAKING
// contract: when a config declares an iac.provider module and plugin load
// fails (binary missing, network down, etc.), wfctl infra plan exits
// non-zero with the literal error format documented in the v0.21.0
// CHANGELOG. There is no --no-provider escape hatch — wfctl validate covers
// the offline-config-validation use case.
func TestRunInfraPlan_FailsLoudOnPluginLoadFailure(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgYAML := `name: test-app
modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
      token: ignored
  - name: vpc
    type: infra.vpc
    config:
      provider: do
      region: nyc3
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	// Force the plan path's provider construction to fail with a caller-
	// distinguishable sentinel so the assertion can prove the error path
	// surfaces upstream rather than a coincidental match on a generic
	// substring.
	loadErr := errors.New("dial tcp: connection refused")
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return nil, nil, loadErr
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	err := runInfraPlan([]string{"--config", cfgPath})
	if err == nil {
		t.Fatal("expected error on plugin-load failure, got nil")
	}
	got := err.Error()
	want := `error: failed to load plugin "digitalocean": dial tcp: connection refused; wfctl infra plan now requires the plugin process to compute Diff (since v0.21.0)`
	if !strings.Contains(got, want) {
		t.Errorf("error message:\n  got:  %q\n  want: contains %q", got, want)
	}
}

// TestRunInfraPlan_PassesProviderToComputePlan verifies T3.6b's positive
// contract: when plugin load succeeds, the loaded provider is threaded
// into platform.ComputePlan so v2 Diff dispatch (T3.6e) operates against a
// real plugin process at plan time, not just at apply time.
func TestRunInfraPlan_PassesProviderToComputePlan(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfgYAML := `name: test-app
modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
      token: ignored
  - name: vpc
    type: infra.vpc
    config:
      provider: do
      region: nyc3
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	stub := &iactest.NoopProvider{}
	var loadCount atomic.Int64
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		loadCount.Add(1)
		return stub, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	if err := runInfraPlan([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runInfraPlan: %v", err)
	}
	if got := loadCount.Load(); got != 1 {
		t.Errorf("expected provider loaded once, got %d", got)
	}
}
