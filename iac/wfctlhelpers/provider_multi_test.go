package wfctlhelpers_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// keysOf returns the sorted keys of a string-keyed map so test failure
// messages are deterministic.
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestLoadAllIaCProvidersFromConfig_Two exercises the design-cycle-4
// Important #6 fix: LoadIaCProviderFromConfig is first-match-only, but
// the handler library (T5/T6) needs ALL declared iac.provider modules
// keyed by module name so each Provider record in ListProviders carries
// the right module attribution. This test pins the minimum shape from
// plan §Task 3 — two providers, both keyed by their module name.
func TestLoadAllIaCProvidersFromConfig_Two(t *testing.T) {
	installFakeResolver(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "multi.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: stub-a
    type: iac.provider
    config:
      provider: stub
  - name: stub-b
    type: iac.provider
    config:
      provider: stub
`), 0o600); err != nil {
		t.Fatal(err)
	}

	providers, closers, err := wfctlhelpers.LoadAllIaCProvidersFromConfig(context.Background(), cfgPath)
	if err != nil {
		t.Fatalf("LoadAllIaCProvidersFromConfig: %v", err)
	}
	for _, c := range closers {
		defer c.Close()
	}
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d (keys: %v)", len(providers), keysOf(providers))
	}
	if _, ok := providers["stub-a"]; !ok {
		t.Errorf("missing stub-a (keys: %v)", keysOf(providers))
	}
	if _, ok := providers["stub-b"]; !ok {
		t.Errorf("missing stub-b (keys: %v)", keysOf(providers))
	}
	if len(closers) != 2 {
		t.Errorf("expected 2 closers (one per provider), got %d", len(closers))
	}
}

// TestLoadAllIaCProvidersFromConfig_EmptyConfig returns (empty map, nil
// closers, nil error) when no iac.provider modules are declared.
// Mirrors LoadIaCProviderFromConfig's permissive shape so callers don't
// need to special-case the missing-providers case.
func TestLoadAllIaCProvidersFromConfig_EmptyConfig(t *testing.T) {
	installFakeResolver(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "no-providers.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: web
    type: http.server
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	providers, closers, err := wfctlhelpers.LoadAllIaCProvidersFromConfig(context.Background(), cfgPath)
	if err != nil {
		t.Fatalf("LoadAllIaCProvidersFromConfig: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers, got %d (keys: %v)", len(providers), keysOf(providers))
	}
	if len(closers) != 0 {
		t.Errorf("expected 0 closers, got %d", len(closers))
	}
}

// TestLoadAllIaCProvidersFromConfig_SkipsMissingProviderField mirrors
// LoadIaCProviderFromConfig's behavior: iac.provider modules without a
// non-empty `provider:` string are silently skipped (not an error). The
// design assumes such a module is misconfigured and excludes it from
// the loaded set rather than failing the whole load — same shape as
// the single-provider path so callers can rely on consistent semantics.
func TestLoadAllIaCProvidersFromConfig_SkipsMissingProviderField(t *testing.T) {
	calls := installFakeResolver(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mixed.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: incomplete
    type: iac.provider
    config: {}
  - name: complete
    type: iac.provider
    config:
      provider: stub
`), 0o600); err != nil {
		t.Fatal(err)
	}

	providers, closers, err := wfctlhelpers.LoadAllIaCProvidersFromConfig(context.Background(), cfgPath)
	if err != nil {
		t.Fatalf("LoadAllIaCProvidersFromConfig: %v", err)
	}
	for _, c := range closers {
		defer c.Close()
	}
	if len(providers) != 1 || providers["complete"] == nil {
		t.Errorf("expected 1 provider keyed 'complete', got %d (keys: %v)", len(providers), keysOf(providers))
	}
	if len(*calls) != 1 {
		t.Errorf("Resolver called %d times, want exactly 1 (incomplete module is skipped pre-resolve)", len(*calls))
	}
}

// TestLoadAllIaCProvidersFromConfig_ResolverErrorRollsBack ensures that
// when the Nth resolve fails, the prior N-1 closers are released
// before returning the error. Otherwise an error from provider #3
// leaks the subprocesses + plugin managers of providers #1 and #2.
func TestLoadAllIaCProvidersFromConfig_ResolverErrorRollsBack(t *testing.T) {
	orig := wfctlhelpers.Resolver
	t.Cleanup(func() { wfctlhelpers.Resolver = orig })

	var closedTracker []bool // index by module-name order
	wfctlhelpers.Resolver = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType == "broken" {
			return nil, nil, errors.New("simulated resolver failure")
		}
		idx := len(closedTracker)
		closedTracker = append(closedTracker, false)
		myIdx := idx
		closer := closerFuncT(func() error { closedTracker[myIdx] = true; return nil })
		return &stubProvider{name: providerType}, closer, nil
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "fail.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: ok-a
    type: iac.provider
    config:
      provider: stub
  - name: ok-b
    type: iac.provider
    config:
      provider: stub
  - name: bad
    type: iac.provider
    config:
      provider: broken
`), 0o600); err != nil {
		t.Fatal(err)
	}

	providers, closers, err := wfctlhelpers.LoadAllIaCProvidersFromConfig(context.Background(), cfgPath)
	if err == nil {
		t.Fatal("expected resolver-failure error, got nil")
	}
	if providers != nil {
		t.Errorf("providers = %v on error, want nil", providers)
	}
	if closers != nil {
		t.Errorf("closers = %v on error, want nil — caller has no handle to release them", closers)
	}
	// All previously-opened closers must have been called by the helper
	// before returning the error.
	for i, closed := range closedTracker {
		if !closed {
			t.Errorf("closer #%d (ok-a/ok-b) was not closed on resolver-failure rollback", i)
		}
	}
}

// closerFuncT adapts a func() error to io.Closer for tests in this
// package. (state_plugin_internal_test.go already declares one in
// `package wfctlhelpers` — different package, no collision.)
type closerFuncT func() error

func (f closerFuncT) Close() error { return f() }
