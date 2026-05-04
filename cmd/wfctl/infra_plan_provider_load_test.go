package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	stub := &planNoopProvider{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		stub.loadCount++
		return stub, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	if err := runInfraPlan([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runInfraPlan: %v", err)
	}
	if stub.loadCount != 1 {
		t.Errorf("expected provider loaded once, got %d", stub.loadCount)
	}
}

// planNoopProvider is a minimal interfaces.IaCProvider for plan-path tests
// that only need to confirm the provider was constructed. ComputePlan in
// T3.6a still uses the legacy ConfigHash compare and ignores the provider;
// in T3.6e it dispatches Diff via ResourceDriver, which this stub returns
// as nil so ComputePlan must guard the nil driver per the contract.
type planNoopProvider struct {
	loadCount int
}

func (p *planNoopProvider) Name() string                                         { return "stub" }
func (p *planNoopProvider) Version() string                                      { return "0.0.0" }
func (p *planNoopProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *planNoopProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (p *planNoopProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *planNoopProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *planNoopProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *planNoopProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *planNoopProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *planNoopProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *planNoopProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *planNoopProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *planNoopProvider) SupportedCanonicalKeys() []string { return nil }
func (p *planNoopProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *planNoopProvider) Close() error { return nil }
