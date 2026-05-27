package wfctlhelpers_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// stubProvider is a minimal interfaces.IaCProvider implementation used by
// the provider-lift tests so they don't need to spawn a real plugin
// subprocess. Only Name() is exercised; the rest exist to satisfy the
// interface and return zero values.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string                                         { return s.name }
func (s *stubProvider) Version() string                                      { return "test" }
func (s *stubProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (s *stubProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (s *stubProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, errors.New("stub: Plan not implemented")
}
func (s *stubProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, errors.New("stub: Destroy not implemented")
}
func (s *stubProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, errors.New("stub: Status not implemented")
}
func (s *stubProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, errors.New("stub: DetectDrift not implemented")
}
func (s *stubProvider) Import(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
	return nil, errors.New("stub: Import not implemented")
}
func (s *stubProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, errors.New("stub: ResolveSizing not implemented")
}
func (s *stubProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, errors.New("stub: ResourceDriver not implemented")
}
func (s *stubProvider) SupportedCanonicalKeys() []string { return nil }
func (s *stubProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (s *stubProvider) Close() error { return nil }

type nopCloser struct{ closed bool }

func (n *nopCloser) Close() error { n.closed = true; return nil }

// installFakeResolver swaps wfctlhelpers.Resolver to a fake for the
// duration of the test, restoring the previous resolver on cleanup. The
// fake returns a stubProvider whose Name reflects the providerType
// argument so the test can assert which iac.provider module won.
func installFakeResolver(t *testing.T) (recorded *[]string) {
	t.Helper()
	calls := []string{}
	orig := wfctlhelpers.Resolver
	wfctlhelpers.Resolver = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		calls = append(calls, providerType)
		return &stubProvider{name: providerType}, &nopCloser{}, nil
	}
	t.Cleanup(func() { wfctlhelpers.Resolver = orig })
	return &calls
}

func TestLoadIaCProviderFromConfig_StubProvider(t *testing.T) {
	calls := installFakeResolver(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "stub.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: stub-provider
    type: iac.provider
    config:
      provider: stub
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, closer, err := wfctlhelpers.LoadIaCProviderFromConfig(context.Background(), cfgPath)
	if err != nil {
		t.Fatalf("LoadIaCProviderFromConfig: %v", err)
	}
	if provider == nil {
		t.Fatal("provider is nil with nil error")
	}
	if closer == nil {
		t.Fatal("closer is nil; expected the fake's nopCloser")
	}
	defer closer.Close()
	if provider.Name() != "stub" {
		t.Errorf("provider.Name() = %q, want %q", provider.Name(), "stub")
	}
	if len(*calls) != 1 || (*calls)[0] != "stub" {
		t.Errorf("resolver invocations = %v, want [stub]", *calls)
	}
}

// TestLoadIaCProviderFromConfig_NoProviderModule returns (nil, nil, nil)
// when the config has no iac.provider module — the caller treats this
// as "no provider available" rather than an error. Mirrors the
// wfctl-internal behavior.
func TestLoadIaCProviderFromConfig_NoProviderModule(t *testing.T) {
	installFakeResolver(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "no-provider.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: web
    type: http.server
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, closer, err := wfctlhelpers.LoadIaCProviderFromConfig(context.Background(), cfgPath)
	if err != nil {
		t.Fatalf("LoadIaCProviderFromConfig: %v", err)
	}
	if provider != nil {
		t.Errorf("provider = %v, want nil", provider)
	}
	if closer != nil {
		t.Errorf("closer = %v, want nil", closer)
	}
}

// TestLoadIaCProviderFromConfig_FirstMatchWins documents the
// first-match-only invariant the design doc cycle-4 reviewer flagged
// (Important #6 → resolved by adding LoadAllIaCProvidersFromConfig in
// Task 3). Pinning the behavior here prevents accidental reordering of
// the loop or change in tie-break semantics.
func TestLoadIaCProviderFromConfig_FirstMatchWins(t *testing.T) {
	calls := installFakeResolver(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "multi.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: first
    type: iac.provider
    config:
      provider: alpha
  - name: second
    type: iac.provider
    config:
      provider: beta
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, closer, err := wfctlhelpers.LoadIaCProviderFromConfig(context.Background(), cfgPath)
	if err != nil {
		t.Fatalf("LoadIaCProviderFromConfig: %v", err)
	}
	defer closer.Close()
	if provider.Name() != "alpha" {
		t.Errorf("first-match-wins: got %q, want %q", provider.Name(), "alpha")
	}
	if len(*calls) != 1 {
		t.Errorf("resolver called %d times, want exactly 1 (first match short-circuits)", len(*calls))
	}
}

// TestLoadIaCProviderFromConfig_LoadError surfaces config-load errors
// with context so the caller can diagnose missing/malformed configs.
func TestLoadIaCProviderFromConfig_LoadError(t *testing.T) {
	installFakeResolver(t)
	_, _, err := wfctlhelpers.LoadIaCProviderFromConfig(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

// TestLoadIaCProviderFromConfig_NoResolverRegistered guards the default
// resolver returns a clear error when no init() has registered a real
// loader. Without this, an empty Resolver field would panic with a
// nil-func-call, which is far less actionable than the wfctlhelpers:
// prefix error.
func TestLoadIaCProviderFromConfig_NoResolverRegistered(t *testing.T) {
	orig := wfctlhelpers.Resolver
	wfctlhelpers.Resolver = wfctlhelpers.UnregisteredResolver
	t.Cleanup(func() { wfctlhelpers.Resolver = orig })

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "stub.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: stub-provider
    type: iac.provider
    config:
      provider: stub
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := wfctlhelpers.LoadIaCProviderFromConfig(context.Background(), cfgPath)
	if err == nil {
		t.Fatal("expected error from unregistered resolver, got nil")
	}
}
