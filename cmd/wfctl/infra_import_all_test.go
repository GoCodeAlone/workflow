package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestRunInfraImportAll_requiresProvider pins the contract: import-all without
// --provider fails fast with a clear error pointing at the missing flag.
// Mirrors the sister guard in runInfraImport which requires --name. Catches
// the regression where the dispatch core silently defaults to the empty
// provider name and falls through to a generic "no module named \"\"" error
// that doesn't help operators.
func TestRunInfraImportAll_requiresProvider(t *testing.T) {
	err := runInfraImportAll([]string{})
	if err == nil {
		t.Fatal("expected error from runInfraImportAll with no flags; got nil")
	}
	if !strings.Contains(err.Error(), "--provider") {
		t.Fatalf("error %q should mention --provider; got %v", err.Error(), err)
	}
}

// TestRunInfraImportAll_requiresType pins the second-required-flag contract:
// after --provider passes, --type must also be set. Catches the regression
// where the implementation only validates --provider and lets a missing --type
// fall through to enumerator.EnumerateAll("") which surfaces as a generic
// "resource type not supported" error from the provider plugin instead of a
// clear CLI-level error.
func TestRunInfraImportAll_requiresType(t *testing.T) {
	err := runInfraImportAll([]string{"--provider", "digitalocean"})
	if err == nil {
		t.Fatal("expected error from runInfraImportAll with --provider but no --type; got nil")
	}
	if !strings.Contains(err.Error(), "--type") {
		t.Fatalf("error %q should mention --type; got %v", err.Error(), err)
	}
}

// ── stubImportAllProvider ─────────────────────────────────────────────────────

// stubImportAllProvider implements interfaces.IaCProvider + the optional
// interfaces.EnumeratorAll sub-interface so the dispatch core's runtime
// type-assertion succeeds. Test-local: only Import + EnumerateAll receive
// meaningful behavior; everything else returns zero-value to satisfy the
// full IaCProvider surface.
type stubImportAllProvider struct {
	mu              sync.Mutex
	enumerateAll    func(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error)
	importFn        func(ctx context.Context, cloudID, resourceType string) (*interfaces.ResourceState, error)
	importCalls     []importCall // captured for assertions
	enumerateCalls  []string     // resourceType values from each EnumerateAll call
	enumerateAllErr error        // when non-nil, EnumerateAll returns this regardless of enumerateAll fn
}

type importCall struct {
	CloudID      string
	ResourceType string
}

func (s *stubImportAllProvider) Name() string                                         { return "stub" }
func (s *stubImportAllProvider) Version() string                                      { return "0.0.0" }
func (s *stubImportAllProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (s *stubImportAllProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (s *stubImportAllProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (s *stubImportAllProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (s *stubImportAllProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (s *stubImportAllProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (s *stubImportAllProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (s *stubImportAllProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (s *stubImportAllProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, fmt.Errorf("stubImportAllProvider: ResourceDriver not implemented")
}
func (s *stubImportAllProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (s *stubImportAllProvider) Close() error                     { return nil }
func (s *stubImportAllProvider) SupportedCanonicalKeys() []string { return nil }

func (s *stubImportAllProvider) Import(ctx context.Context, cloudID, resourceType string) (*interfaces.ResourceState, error) {
	s.mu.Lock()
	s.importCalls = append(s.importCalls, importCall{CloudID: cloudID, ResourceType: resourceType})
	s.mu.Unlock()
	if s.importFn == nil {
		return nil, fmt.Errorf("stub: importFn not set")
	}
	return s.importFn(ctx, cloudID, resourceType)
}

// EnumerateAll satisfies interfaces.EnumeratorAll. The stub MUST implement
// it on the value-receiver path that matches the IaCProvider's pointer
// receiver, so the type assertion in runInfraImportAllWithDeps succeeds.
func (s *stubImportAllProvider) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	s.mu.Lock()
	s.enumerateCalls = append(s.enumerateCalls, resourceType)
	s.mu.Unlock()
	if s.enumerateAllErr != nil {
		return nil, s.enumerateAllErr
	}
	if s.enumerateAll == nil {
		return nil, nil
	}
	return s.enumerateAll(ctx, resourceType)
}

// Compile-time assertion: the stub really does satisfy both interfaces.
// If a future workflow SDK revision adds a method, this fails fast at
// compile time rather than at test-time via a nil-pointer panic.
var (
	_ interfaces.IaCProvider   = (*stubImportAllProvider)(nil)
	_ interfaces.EnumeratorAll = (*stubImportAllProvider)(nil)
	_ interfaces.IaCProvider   = (*providerImportOnly)(nil)
)

// providerImportOnly implements IaCProvider but NOT EnumeratorAll. The
// dispatch core's runtime type assertion against EnumeratorAll must fail
// cleanly on this type, returning (0, error) — not panic, not proceed with
// a nil enumerator. Drives TestRunInfraImportAllWithDeps_requiresEnumerator.
type providerImportOnly struct {
	importFn func(ctx context.Context, cloudID, resourceType string) (*interfaces.ResourceState, error)
}

func (p *providerImportOnly) Name() string                                         { return "import-only" }
func (p *providerImportOnly) Version() string                                      { return "0.0.0" }
func (p *providerImportOnly) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *providerImportOnly) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (p *providerImportOnly) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *providerImportOnly) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *providerImportOnly) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *providerImportOnly) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *providerImportOnly) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *providerImportOnly) Import(ctx context.Context, cloudID, resourceType string) (*interfaces.ResourceState, error) {
	if p.importFn == nil {
		return nil, nil
	}
	return p.importFn(ctx, cloudID, resourceType)
}
func (p *providerImportOnly) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *providerImportOnly) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, fmt.Errorf("not implemented")
}
func (p *providerImportOnly) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *providerImportOnly) Close() error                     { return nil }
func (p *providerImportOnly) SupportedCanonicalKeys() []string { return nil }

// ── dispatch-core tests ───────────────────────────────────────────────────────

// TestRunInfraImportAllWithDeps_dispatch is the happy-path contract: given
// a provider that implements EnumeratorAll and returns 2 zones, the
// dispatch core calls Import for each and persists 2 ResourceState rows via
// the state store. Mirrors plan §Task 16 Step 1.
func TestRunInfraImportAllWithDeps_dispatch(t *testing.T) {
	stub := &stubImportAllProvider{
		enumerateAll: func(_ context.Context, rt string) ([]*interfaces.ResourceOutput, error) {
			if rt != "infra.dns" {
				return nil, fmt.Errorf("unexpected resourceType %q", rt)
			}
			return []*interfaces.ResourceOutput{
				{ProviderID: "alpha.test", Type: "infra.dns", Outputs: map[string]any{"zone": "alpha.test", "ttl": 1800}},
				{ProviderID: "beta.test", Type: "infra.dns", Outputs: map[string]any{"zone": "beta.test", "ttl": 3600}},
			}, nil
		},
		importFn: func(_ context.Context, cloudID, rt string) (*interfaces.ResourceState, error) {
			// Mirror what a real provider returns: ProviderID populated, Name
			// reflects the cloud-side identifier so resourceStateFromImportedState
			// can hash + persist without re-reading.
			return &interfaces.ResourceState{
				ID:         cloudID,
				Name:       cloudID,
				Type:       rt,
				ProviderID: cloudID,
				Outputs:    map[string]any{"zone": cloudID},
			}, nil
		},
	}
	store := &fakeStateStore{}
	n, err := runInfraImportAllWithDeps(context.Background(), stub, "stub", store, "infra.dns", false)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if n != 2 {
		t.Errorf("imported = %d; want 2", n)
	}
	saved, _ := store.ListResources(context.Background())
	if len(saved) != 2 {
		t.Fatalf("store saved %d resources; want 2: %+v", len(saved), saved)
	}
	// Each ResourceState must carry the spec name (sanitized zone name) +
	// the provider type so the state-store row is identifiable by operators
	// later via `wfctl infra state list`.
	for _, s := range saved {
		if s.Provider != "stub" {
			t.Errorf("state.Provider = %q; want %q", s.Provider, "stub")
		}
		if s.Type != "infra.dns" {
			t.Errorf("state.Type = %q; want %q", s.Type, "infra.dns")
		}
		if s.ProviderID == "" {
			t.Errorf("state.ProviderID empty for %+v", s)
		}
	}
	if len(stub.importCalls) != 2 {
		t.Errorf("Import called %d times; want 2", len(stub.importCalls))
	}
	if len(stub.enumerateCalls) != 1 || stub.enumerateCalls[0] != "infra.dns" {
		t.Errorf("EnumerateAll calls = %+v; want [infra.dns]", stub.enumerateCalls)
	}
}

// TestRunInfraImportAllWithDeps_dryRun pins that --dry-run probes via
// Import but does NOT persist any state. Catches the regression where the
// dry-run branch silently calls SaveResource and writes through to the
// configured state backend.
func TestRunInfraImportAllWithDeps_dryRun(t *testing.T) {
	stub := &stubImportAllProvider{
		enumerateAll: func(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
			return []*interfaces.ResourceOutput{
				{ProviderID: "x.test", Outputs: map[string]any{"zone": "x.test"}},
			}, nil
		},
		importFn: func(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
			return &interfaces.ResourceState{ProviderID: "x.test", Type: "infra.dns", Name: "x-test"}, nil
		},
	}
	store := &fakeStateStore{}
	n, err := runInfraImportAllWithDeps(context.Background(), stub, "stub", store, "infra.dns", true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if n != 1 {
		t.Errorf("dry-run reported %d would-import; want 1", n)
	}
	saved, _ := store.ListResources(context.Background())
	if len(saved) != 0 {
		t.Errorf("dry-run wrote %d states to store; want 0 (dry-run must not persist)", len(saved))
	}
	if len(stub.importCalls) != 1 {
		t.Errorf("dry-run Import calls = %d; want 1 (probe for each zone)", len(stub.importCalls))
	}
}

// TestRunInfraImportAllWithDeps_perZoneFailureIsolation pins that a single
// zone's import failure does NOT abort subsequent zones. The successful
// imports are persisted; the failure surfaces in the returned error. Mirrors
// the design's "per-zone failure isolated" contract.
func TestRunInfraImportAllWithDeps_perZoneFailureIsolation(t *testing.T) {
	stub := &stubImportAllProvider{
		enumerateAll: func(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
			return []*interfaces.ResourceOutput{
				{ProviderID: "ok-1.test", Outputs: map[string]any{"zone": "ok-1.test"}},
				{ProviderID: "fail.test", Outputs: map[string]any{"zone": "fail.test"}},
				{ProviderID: "ok-2.test", Outputs: map[string]any{"zone": "ok-2.test"}},
			}, nil
		},
		importFn: func(_ context.Context, cloudID, rt string) (*interfaces.ResourceState, error) {
			if cloudID == "fail.test" {
				return nil, fmt.Errorf("simulated transient: %s", cloudID)
			}
			return &interfaces.ResourceState{ProviderID: cloudID, Type: rt, Name: cloudID}, nil
		},
	}
	store := &fakeStateStore{}
	n, err := runInfraImportAllWithDeps(context.Background(), stub, "stub", store, "infra.dns", false)
	if err == nil {
		t.Fatal("want error summarizing per-zone failures; got nil")
	}
	if !strings.Contains(err.Error(), "fail.test") {
		t.Errorf("error must mention failing zone; got %v", err)
	}
	if n != 2 {
		t.Errorf("successful imports = %d; want 2 (the two non-failing zones)", n)
	}
	saved, _ := store.ListResources(context.Background())
	if len(saved) != 2 {
		t.Errorf("store saved %d states; want 2 (the two non-failing zones)", len(saved))
	}
}

// TestRunInfraImportAllWithDeps_requiresEnumerator pins that providers
// which don't implement IaCProviderEnumerator surface a clear error rather
// than panicking. Operators see "provider does not support EnumerateAll"
// instead of a runtime crash when targeting a provider that hasn't shipped
// the optional sub-contract yet.
func TestRunInfraImportAllWithDeps_requiresEnumerator(t *testing.T) {
	p := &providerImportOnly{}
	store := &fakeStateStore{}
	n, err := runInfraImportAllWithDeps(context.Background(), p, "import-only", store, "infra.dns", false)
	if err == nil {
		t.Fatal("want error for provider missing EnumerateAll; got nil")
	}
	if !strings.Contains(err.Error(), "EnumerateAll") {
		t.Errorf("error must mention EnumerateAll; got %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d; want 0 (no work attempted)", n)
	}
}

// TestRunInfraImportAllWithDeps_enumerateError pins the early-abort path:
// when EnumerateAll itself errors (auth failure, network, etc.), the
// dispatch core returns (0, error) without calling Import or SaveResource.
func TestRunInfraImportAllWithDeps_enumerateError(t *testing.T) {
	sentinel := errors.New("auth: unauthorized")
	stub := &stubImportAllProvider{enumerateAllErr: sentinel}
	store := &fakeStateStore{}
	n, err := runInfraImportAllWithDeps(context.Background(), stub, "stub", store, "infra.dns", false)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("want wrapped sentinel error; got %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d; want 0 on enumerate failure", n)
	}
	if len(stub.importCalls) != 0 {
		t.Errorf("Import should not be called when enumerate fails; got %d calls", len(stub.importCalls))
	}
}

// TestDumpStateToFile pins the --output dump contract: state-store
// snapshot is written as JSON with a top-level "resources" array, file mode
// is 0o600 (sensitive — AppliedConfig + Outputs may contain creds).
func TestDumpStateToFile(t *testing.T) {
	store := &fakeStateStore{}
	_ = store.SaveResource(context.Background(), interfaces.ResourceState{
		Name: "alpha", Type: "infra.dns", ProviderID: "alpha.test",
	})
	dir := t.TempDir()
	out := filepath.Join(dir, "snapshot.json")
	if err := dumpStateToFile(context.Background(), store, out); err != nil {
		t.Fatalf("dump: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v; want 0o600 (state may contain secrets)", info.Mode().Perm())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), `"resources"`) {
		t.Errorf("dump missing 'resources' key: %s", data)
	}
	if !strings.Contains(string(data), `"alpha.test"`) {
		t.Errorf("dump missing alpha.test ProviderID: %s", data)
	}
}

// TestSanitizeImportedZoneName pins the zone-name → state-key mapping:
// dots in FQDNs become hyphens so the resulting name is safe for YAML keys
// and filesystem-backed state paths. Idempotent on already-sanitized input.
func TestSanitizeImportedZoneName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"alpha.test", "alpha-test"},
		{"a.b.example.com", "a-b-example-com"},
		{"already-clean", "already-clean"},
		{"under_score", "under_score"},
		{"123-digits", "123-digits"},
		{"", ""},
	}
	for _, c := range cases {
		got := sanitizeImportedZoneName(c.in)
		if got != c.want {
			t.Errorf("sanitizeImportedZoneName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
