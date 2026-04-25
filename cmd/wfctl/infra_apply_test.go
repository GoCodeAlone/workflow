package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── fakes ──────────────────────────────────────────────────────────────────────

// applyCapture is a minimal IaCProvider that records Plan and Apply calls.
type applyCapture struct {
	mu          sync.Mutex
	planCalled  bool
	applyCalled bool
	planSpecs   []interfaces.ResourceSpec
	appliedPlan *interfaces.IaCPlan
}

func (f *applyCapture) Name() string                                         { return "fake" }
func (f *applyCapture) Version() string                                      { return "0.0.0" }
func (f *applyCapture) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *applyCapture) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *applyCapture) Plan(_ context.Context, desired []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planCalled = true
	f.planSpecs = append(f.planSpecs, desired...)
	return nil, nil
}
func (f *applyCapture) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCalled = true
	f.appliedPlan = plan
	return &interfaces.ApplyResult{}, nil
}
func (f *applyCapture) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *applyCapture) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *applyCapture) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *applyCapture) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *applyCapture) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *applyCapture) ResourceDriver(_ string) (interfaces.ResourceDriver, error) { return nil, nil }
func (f *applyCapture) SupportedCanonicalKeys() []string                           { return nil }
func (f *applyCapture) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *applyCapture) Close() error { return nil }

type readDriver struct {
	readOut            *interfaces.ResourceOutput
	readErr            error
	reads              []interfaces.ResourceRef
	expectedProviderID string
	format             interfaces.ProviderIDFormat
}

func (d *readDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type}, nil
}

func (d *readDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	d.reads = append(d.reads, ref)
	if d.expectedProviderID != "" && ref.ProviderID != d.expectedProviderID {
		return nil, interfaces.ErrResourceNotFound
	}
	return d.readOut, d.readErr
}

func (d *readDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}

func (d *readDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }

func (d *readDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}

func (d *readDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}

func (d *readDriver) Scale(_ context.Context, ref interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}

func (d *readDriver) SensitiveKeys() []string { return nil }

func (d *readDriver) ProviderIDFormat() interfaces.ProviderIDFormat {
	return d.format
}

type readBackedProvider struct {
	applyCapture
	driver interfaces.ResourceDriver
}

func (p *readBackedProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

type readBackedFailingApplyProvider struct {
	readBackedProvider
	applyErr error
}

func (p *readBackedFailingApplyProvider) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	p.mu.Lock()
	p.applyCalled = true
	p.appliedPlan = plan
	p.mu.Unlock()
	return nil, p.applyErr
}

// ── TestApplyInfraModules_DirectPath ───────────────────────────────────────────

// TestApplyInfraModules_DirectPath verifies that applyInfraModules:
//  1. Loads the IaCProvider for the declared iac.provider module.
//  2. Computes a plan (via platform.ComputePlan — no current state → all creates).
//  3. Calls provider.Apply with the computed plan.
//  4. Correctly handles two infra.* modules that reference the same provider.
func TestApplyInfraModules_DirectPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud
      token: "test-token"

  - name: bmw-db
    type: infra.database
    config:
      provider: do-provider
      engine: postgres
      size: s

  - name: bmw-app
    type: infra.container_service
    config:
      provider: do-provider
      image: registry.example.com/bmw:latest
      http_port: 8080
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fake := &applyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType != "fake-cloud" {
			t.Errorf("unexpected provider type %q", providerType)
		}
		return fake, nil, nil
	}
	defer func() { resolveIaCProvider = orig }()

	if err := applyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	// Apply must have been called.
	if !fake.applyCalled {
		t.Fatal("provider.Apply was not called")
	}

	// Plan must contain exactly 2 create actions (no current state → all creates).
	if fake.appliedPlan == nil {
		t.Fatal("appliedPlan is nil")
	}
	if got := len(fake.appliedPlan.Actions); got != 2 {
		t.Errorf("plan actions: want 2, got %d", got)
	}
	actions := map[string]string{}
	for _, a := range fake.appliedPlan.Actions {
		actions[a.Resource.Name] = a.Action
	}
	for _, name := range []string{"bmw-db", "bmw-app"} {
		if actions[name] != "create" {
			t.Errorf("action for %q: want create, got %q", name, actions[name])
		}
	}
}

func TestApplyInfraModules_ScopesCurrentStatePerProvider(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud-a

  - name: aws-provider
    type: iac.provider
    config:
      provider: fake-cloud-b

  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`

  - name: do-vpc
    type: infra.vpc
    config:
      provider: do-provider
      region: nyc3

  - name: aws-vpc
    type: infra.vpc
    config:
      provider: aws-provider
      region: us-east-1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := &fsWfctlStateStore{dir: stateDir}
	for _, st := range []interfaces.ResourceState{
		{
			ID:            "do-vpc",
			Name:          "do-vpc",
			Type:          "infra.vpc",
			Provider:      "fake-cloud-a",
			ProviderID:    "do-vpc-id",
			ConfigHash:    configHashMap(map[string]any{"provider": "do-provider", "region": "nyc3"}),
			AppliedConfig: map[string]any{"provider": "do-provider", "region": "nyc3"},
		},
		{
			ID:            "aws-vpc",
			Name:          "aws-vpc",
			Type:          "infra.vpc",
			Provider:      "fake-cloud-b",
			ProviderID:    "aws-vpc-id",
			ConfigHash:    configHashMap(map[string]any{"provider": "aws-provider", "region": "us-east-1"}),
			AppliedConfig: map[string]any{"provider": "aws-provider", "region": "us-east-1"},
		},
	} {
		if err := store.SaveResource(context.Background(), st); err != nil {
			t.Fatalf("seed state %s: %v", st.Name, err)
		}
	}

	providers := map[string]*applyCapture{
		"fake-cloud-a": {},
		"fake-cloud-b": {},
	}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		p, ok := providers[providerType]
		if !ok {
			t.Fatalf("unexpected provider type %q", providerType)
		}
		return p, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	if err := applyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	for providerType, provider := range providers {
		provider.mu.Lock()
		applyCalled := provider.applyCalled
		appliedPlan := provider.appliedPlan
		provider.mu.Unlock()
		if applyCalled {
			t.Fatalf("%s Apply was called with plan %+v; provider-scoped current state should produce no changes", providerType, appliedPlan)
		}
	}
}

func TestApplyInfraModules_SameProviderTypeDoesNotDeleteOtherProviderInstanceState(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: east-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: west-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`

  - name: west-vpc
    type: infra.vpc
    config:
      provider: west-provider
      region: sfo3
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := &fsWfctlStateStore{dir: stateDir}
	eastState := interfaces.ResourceState{
		ID:            "east-vpc",
		Name:          "east-vpc",
		Type:          "infra.vpc",
		Provider:      "fake-cloud",
		ProviderID:    "east-vpc-id",
		ConfigHash:    configHashMap(map[string]any{"provider": "east-provider", "region": "nyc3"}),
		AppliedConfig: map[string]any{"provider": "east-provider", "region": "nyc3"},
	}
	if err := store.SaveResource(context.Background(), eastState); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	fake := &applyCapture{}
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType != "fake-cloud" {
			t.Fatalf("providerType = %q, want fake-cloud", providerType)
		}
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	if err := applyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	fake.mu.Lock()
	appliedPlan := fake.appliedPlan
	fake.mu.Unlock()
	if appliedPlan == nil {
		t.Fatal("expected apply plan for west-vpc create")
	}
	for _, action := range appliedPlan.Actions {
		if action.Action == "delete" && action.Resource.Name == "east-vpc" {
			t.Fatalf("west-provider plan included east-provider delete: %+v", appliedPlan.Actions)
		}
	}
}

// ── fakeStateStore ─────────────────────────────────────────────────────────────

// fakeStateStore captures SaveResource and DeleteResource calls for use in tests.
type fakeStateStore struct {
	mu      sync.Mutex
	saved   []interfaces.ResourceState
	deleted []string
	saveErr error // if non-nil, SaveResource returns this error
}

func (f *fakeStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]interfaces.ResourceState(nil), f.saved...), nil
}
func (f *fakeStateStore) SaveResource(_ context.Context, s interfaces.ResourceState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, s)
	return nil
}
func (f *fakeStateStore) DeleteResource(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, name)
	return nil
}

// ── TestApplyWithProvider_NoChanges ────────────────────────────────────────────

// TestApplyWithProvider_NoChanges verifies that when the current state already
// matches the desired spec (identical config hash), Apply is NOT called.
// It exercises the no-op branch of applyWithProviderAndStore directly.
func TestApplyWithProvider_NoChanges(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "my-db",
		Type:   "infra.database",
		Config: map[string]any{"engine": "postgres"},
	}

	// Reproduce the hash that platform.ComputePlan computes via configHash
	// (sorted kv-pair encoding):
	cfgHash := configHashMap(spec.Config)

	current := []interfaces.ResourceState{{
		Name:       spec.Name,
		Type:       spec.Type,
		ConfigHash: cfgHash,
	}}

	fake := &applyCapture{}
	if err := applyWithProviderAndStore(context.Background(), fake, "fake-cloud", []interfaces.ResourceSpec{spec}, current, nil, io.Discard, ""); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.applyCalled {
		t.Error("provider.Apply should NOT be called when current state matches desired spec")
	}
}

func TestApplyWithProvider_AdoptsExistingDNSBeforeComputePlan(t *testing.T) {
	desiredConfig := map[string]any{
		"provider": "do-provider",
		"domain":   "example.com",
		"ttl":      300,
	}
	liveConfig := map[string]any{
		"provider": "do-provider",
		"domain":   "old.example.com",
		"ttl":      300,
	}
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: desiredConfig,
	}
	driver := &readDriver{
		expectedProviderID: "example.com",
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "do-domain-123",
			Outputs: map[string]any{
				"domain": "old.example.com",
				"config": liveConfig,
			},
		},
	}
	provider := &readBackedProvider{driver: driver}
	store := &fakeStateStore{}

	if err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, store, io.Discard, ""); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}

	if len(driver.reads) != 1 {
		t.Fatalf("driver.Read calls = %d, want 1", len(driver.reads))
	}
	if driver.reads[0].Name != "site-dns" || driver.reads[0].Type != "infra.dns" || driver.reads[0].ProviderID != "example.com" {
		t.Fatalf("driver.Read ref = %+v, want site-dns/infra.dns ProviderID example.com", driver.reads[0])
	}

	provider.mu.Lock()
	appliedPlan := provider.appliedPlan
	provider.mu.Unlock()
	if appliedPlan == nil {
		t.Fatal("expected Apply to receive an update plan")
	}
	if len(appliedPlan.Actions) != 1 {
		t.Fatalf("plan actions = %d, want 1", len(appliedPlan.Actions))
	}
	if appliedPlan.Actions[0].Action != "update" {
		t.Fatalf("action = %q, want update", appliedPlan.Actions[0].Action)
	}
	if appliedPlan.Actions[0].Current == nil || appliedPlan.Actions[0].Current.ProviderID != "do-domain-123" {
		t.Fatalf("plan current = %+v, want adopted ProviderID do-domain-123", appliedPlan.Actions[0].Current)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 {
		t.Fatalf("saved states = %d, want 1 adopted state", len(store.saved))
	}
	adopted := store.saved[0]
	if adopted.Name != "site-dns" || adopted.ID != "site-dns" || adopted.Type != "infra.dns" {
		t.Fatalf("adopted identity = id:%q name:%q type:%q, want site-dns/site-dns/infra.dns", adopted.ID, adopted.Name, adopted.Type)
	}
	if adopted.Provider != "digitalocean" {
		t.Fatalf("adopted provider = %q, want digitalocean", adopted.Provider)
	}
	if adopted.ProviderID != "do-domain-123" {
		t.Fatalf("adopted ProviderID = %q, want do-domain-123", adopted.ProviderID)
	}
	if adopted.Outputs["domain"] != "old.example.com" {
		t.Fatalf("adopted Outputs = %#v, want live outputs", adopted.Outputs)
	}
	if adopted.AppliedConfig["domain"] != "old.example.com" {
		t.Fatalf("adopted AppliedConfig = %#v, want live config", adopted.AppliedConfig)
	}
	if adopted.ConfigHash != configHashMap(liveConfig) {
		t.Fatalf("adopted ConfigHash = %q, want live config hash %q", adopted.ConfigHash, configHashMap(liveConfig))
	}
	if adopted.ConfigHash == configHashMap(desiredConfig) {
		t.Fatalf("adopted ConfigHash matched desired hash; ComputePlan would skip required update")
	}
}

func TestApplyWithProvider_DNSAdoptionFailedUpdateKeepsLiveAppliedConfig(t *testing.T) {
	desiredConfig := map[string]any{
		"provider": "do-provider",
		"domain":   "example.com",
		"ttl":      300,
	}
	liveConfig := map[string]any{
		"provider": "do-provider",
		"domain":   "old.example.com",
		"ttl":      100,
	}
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: desiredConfig,
	}
	driver := &readDriver{
		expectedProviderID: "example.com",
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "do-domain-123",
			Outputs: map[string]any{
				"domain": "old.example.com",
				"config": liveConfig,
			},
		},
	}
	provider := &readBackedFailingApplyProvider{
		readBackedProvider: readBackedProvider{driver: driver},
		applyErr:           errors.New("provider update failed"),
	}
	store := &fakeStateStore{}

	err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, store, io.Discard, "")
	if err == nil {
		t.Fatal("expected apply failure, got nil")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 {
		t.Fatalf("saved states = %d, want only the adopted live state", len(store.saved))
	}
	adopted := store.saved[0]
	if adopted.AppliedConfig["domain"] != "old.example.com" || fmt.Sprint(adopted.AppliedConfig["ttl"]) != "100" {
		t.Fatalf("adopted AppliedConfig after failed update = %#v, want live config", adopted.AppliedConfig)
	}
	if adopted.ConfigHash != configHashMap(liveConfig) {
		t.Fatalf("adopted ConfigHash = %q, want live hash %q", adopted.ConfigHash, configHashMap(liveConfig))
	}
	if adopted.ConfigHash == configHashMap(desiredConfig) {
		t.Fatalf("adopted ConfigHash matched desired hash after failed update")
	}
}

func TestApplyWithProvider_DNSAdoptionFallsBackToNameWhenDomainOmitted(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"provider": "do-provider"},
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
	provider := &readBackedProvider{driver: driver}

	if err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, &fakeStateStore{}, io.Discard, ""); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	if len(driver.reads) != 1 {
		t.Fatalf("driver.Read calls = %d, want 1", len(driver.reads))
	}
	if driver.reads[0].ProviderID != "site-dns" {
		t.Fatalf("driver.Read ProviderID = %q, want fallback name site-dns", driver.reads[0].ProviderID)
	}
}

func TestApplyWithProvider_DNSAdoptionSaveFailureFailsBeforeApply(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"domain": "example.com"},
	}
	driver := &readDriver{
		expectedProviderID: "example.com",
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "example.com",
			Outputs:    map[string]any{"domain": "example.com"},
		},
	}
	provider := &readBackedProvider{driver: driver}
	store := &fakeStateStore{saveErr: errors.New("disk full")}

	err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, store, io.Discard, "")
	if err == nil {
		t.Fatal("expected adoption save failure, got nil")
	}
	if !strings.Contains(err.Error(), "persist adopted state") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want adopted state persistence failure", err)
	}
	provider.mu.Lock()
	applyCalled := provider.applyCalled
	provider.mu.Unlock()
	if applyCalled {
		t.Fatal("Apply should not be called after adopted state persistence fails")
	}
}

func TestApplyWithProvider_DNSAdoptionRequiresWritableStateStore(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"domain": "example.com"},
	}
	driver := &readDriver{
		expectedProviderID: "example.com",
		readOut: &interfaces.ResourceOutput{
			Name:       "site-dns",
			Type:       "infra.dns",
			ProviderID: "example.com",
			Outputs:    map[string]any{"domain": "example.com"},
		},
	}
	provider := &readBackedProvider{driver: driver}

	err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, nil, io.Discard, "")
	if err == nil {
		t.Fatal("expected adoption to fail with noop state store")
	}
	if !strings.Contains(err.Error(), "writable iac.state") {
		t.Fatalf("error = %v, want writable iac.state message", err)
	}
	provider.mu.Lock()
	applyCalled := provider.applyCalled
	provider.mu.Unlock()
	if applyCalled {
		t.Fatal("Apply should not be called when DNS adoption cannot be persisted")
	}
}

func TestApplyWithProvider_DNSAdoptionRejectsMalformedProviderID(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"domain": "example.com"},
	}
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
	provider := &readBackedProvider{driver: driver}
	store := &fakeStateStore{}

	err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, store, io.Discard, "")
	if err == nil {
		t.Fatal("expected malformed ProviderID error")
	}
	if !strings.Contains(err.Error(), "malformed ProviderID") || !strings.Contains(err.Error(), "state not persisted") {
		t.Fatalf("error = %v, want strict ProviderID validation failure", err)
	}
	provider.mu.Lock()
	applyCalled := provider.applyCalled
	provider.mu.Unlock()
	if applyCalled {
		t.Fatal("Apply should not be called after malformed adopted ProviderID")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 0 {
		t.Fatalf("saved states = %d, want none for malformed ProviderID", len(store.saved))
	}
}

func TestApplyWithProvider_DNSReadNotFoundKeepsCreateBehavior(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"domain": "example.com"},
	}
	driver := &readDriver{readErr: interfaces.ErrResourceNotFound}
	provider := &readBackedProvider{driver: driver}
	store := &fakeStateStore{}

	if err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, store, io.Discard, ""); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}

	provider.mu.Lock()
	appliedPlan := provider.appliedPlan
	provider.mu.Unlock()
	if appliedPlan == nil || len(appliedPlan.Actions) != 1 {
		t.Fatalf("plan = %+v, want one create action", appliedPlan)
	}
	if appliedPlan.Actions[0].Action != "create" {
		t.Fatalf("action = %q, want create", appliedPlan.Actions[0].Action)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 0 {
		t.Fatalf("saved states = %d, want none before create result", len(store.saved))
	}
}

func TestApplyWithProvider_DNSReadErrorFailsBeforeApply(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"domain": "example.com"},
	}
	driver := &readDriver{readErr: errors.New("provider API unavailable")}
	provider := &readBackedProvider{driver: driver}

	err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, nil, io.Discard, "")
	if err == nil {
		t.Fatal("expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "provider API unavailable") {
		t.Fatalf("error = %v, want read failure", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.applyCalled {
		t.Fatal("Apply should not be called after adoption read failure")
	}
}

func TestApplyWithProvider_DNSReadEmptyProviderIDFailsBeforeApply(t *testing.T) {
	spec := interfaces.ResourceSpec{
		Name:   "site-dns",
		Type:   "infra.dns",
		Config: map[string]any{"domain": "example.com"},
	}
	driver := &readDriver{
		readOut: &interfaces.ResourceOutput{
			Name:    "site-dns",
			Type:    "infra.dns",
			Outputs: map[string]any{"domain": "example.com"},
		},
	}
	provider := &readBackedProvider{driver: driver}
	store := &fakeStateStore{}

	err := applyWithProviderAndStore(t.Context(), provider, "digitalocean", []interfaces.ResourceSpec{spec}, nil, store, io.Discard, "")
	if err == nil {
		t.Fatal("expected error when adopted live output has empty ProviderID")
	}
	if !strings.Contains(err.Error(), "ProviderID") {
		t.Fatalf("error = %v, want ProviderID message", err)
	}
	provider.mu.Lock()
	applyCalled := provider.applyCalled
	provider.mu.Unlock()
	if applyCalled {
		t.Fatal("Apply should not be called after empty ProviderID adoption failure")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 0 {
		t.Fatalf("saved states = %d, want none for invalid adoption", len(store.saved))
	}
}

// TestApplyWithProvider_DeletesRemovedResource verifies that a resource present
// in current state but absent from the desired specs generates a delete action.
// This guards the fix to the type-scoped current-state filter: the old
// name-only filter silently dropped orphaned state entries, preventing deletes.
func TestApplyWithProvider_DeletesRemovedResource(t *testing.T) {
	// Desired: only bmw-app remains.
	specs := []interfaces.ResourceSpec{
		{Name: "bmw-app", Type: "infra.container_service", Config: map[string]any{"image": "registry/app:latest"}},
	}
	// Current: bmw-app + old-db (removed from config, should be deleted).
	appHash := configHashMap(specs[0].Config)
	current := []interfaces.ResourceState{
		{Name: "bmw-app", Type: "infra.container_service", ConfigHash: appHash},
		{Name: "old-db", Type: "infra.database", ConfigHash: "oldhash"},
	}

	fake := &applyCapture{}
	store := &fakeStateStore{}
	if err := applyWithProviderAndStore(context.Background(), fake, "fake-cloud", specs, current, store, io.Discard, ""); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.applyCalled {
		t.Fatal("provider.Apply should have been called for the delete action")
	}
	if fake.appliedPlan == nil {
		t.Fatal("appliedPlan is nil")
	}
	actions := map[string]string{}
	for _, a := range fake.appliedPlan.Actions {
		actions[a.Resource.Name] = a.Action
	}
	if actions["old-db"] != "delete" {
		t.Errorf("expected delete action for old-db, got %q", actions["old-db"])
	}
	if a := actions["bmw-app"]; a != "" {
		t.Errorf("expected no action for bmw-app (hash matches), got %q", a)
	}

	// The delete action for old-db must remove it from the store.
	store.mu.Lock()
	defer store.mu.Unlock()
	found := false
	for _, d := range store.deleted {
		if d == "old-db" {
			found = true
		}
	}
	if !found {
		t.Errorf("store.DeleteResource not called for old-db; deleted=%v", store.deleted)
	}
}

// ── TestApplyWithProvider_SavesState* ──────────────────────────────────────────

// stateReturningProvider is a minimal IaCProvider whose Apply method returns
// a configurable result, used for state-persistence tests.
type stateReturningProvider struct {
	applyResult *interfaces.ApplyResult
	applyErr    error
}

func (p *stateReturningProvider) Name() string    { return "fake" }
func (p *stateReturningProvider) Version() string { return "0.0.0" }
func (p *stateReturningProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *stateReturningProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (p *stateReturningProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *stateReturningProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return p.applyResult, p.applyErr
}
func (p *stateReturningProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *stateReturningProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *stateReturningProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *stateReturningProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *stateReturningProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *stateReturningProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *stateReturningProvider) SupportedCanonicalKeys() []string { return nil }
func (p *stateReturningProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *stateReturningProvider) Close() error { return nil }

// TestApplyWithProvider_SavesStateForSuccessfulResources asserts that
// applyWithProviderAndStore calls store.SaveResource for each resource in
// the Apply result.
func TestApplyWithProvider_SavesStateForSuccessfulResources(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "r1", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
		{Name: "r2", Type: "infra.database", Config: map[string]any{"engine": "postgres"}},
	}
	fake := &stateReturningProvider{
		applyResult: &interfaces.ApplyResult{
			Resources: []interfaces.ResourceOutput{
				{Name: "r1", Type: "infra.vpc", ProviderID: "vpc-1", Outputs: map[string]any{"id": "vpc-1"}},
				{Name: "r2", Type: "infra.database", ProviderID: "db-1", Outputs: map[string]any{"uri": "postgres://..."}},
			},
		},
	}
	store := &fakeStateStore{}

	if err := applyWithProviderAndStore(t.Context(), fake, "fake-cloud", specs, nil, store, io.Discard, ""); err != nil {
		t.Fatalf("apply: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 2 {
		t.Errorf("saved = %d, want 2", len(store.saved))
	}
	found := map[string]string{}
	for _, s := range store.saved {
		found[s.Name] = s.ProviderID
	}
	if found["r1"] != "vpc-1" {
		t.Errorf("r1 ProviderID = %q, want vpc-1", found["r1"])
	}
	if found["r2"] != "db-1" {
		t.Errorf("r2 ProviderID = %q, want db-1", found["r2"])
	}
}

// TestApplyWithProvider_SavesStateOnPartialFailure asserts that when Apply
// returns partial success (some resources + some errors), states are saved for
// the successful resources, and an error is returned for the failures.
func TestApplyWithProvider_SavesStateOnPartialFailure(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "r1", Type: "infra.vpc", Config: nil},
		{Name: "r2", Type: "infra.database", Config: nil},
		{Name: "r3", Type: "infra.container_service", Config: nil},
	}
	fake := &stateReturningProvider{
		applyResult: &interfaces.ApplyResult{
			Resources: []interfaces.ResourceOutput{
				{Name: "r1", ProviderID: "id-1"},
				{Name: "r2", ProviderID: "id-2"},
			},
			Errors: []interfaces.ActionError{
				{Resource: "r3", Action: "create", Error: "boom"},
			},
		},
	}
	store := &fakeStateStore{}

	err := applyWithProviderAndStore(t.Context(), fake, "fake-cloud", specs, nil, store, io.Discard, "")
	if err == nil {
		t.Fatal("expected error on partial failure, got nil")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 2 {
		t.Errorf("saved = %d, want 2 (partial success should still persist successful resources)", len(store.saved))
	}
}

// TestApplyWithProvider_StoreSaveFailureFails asserts that a SaveResource error
// after a successful provider mutation fails the command so callers cannot miss
// out-of-sync state.
func TestApplyWithProvider_StoreSaveFailureFails(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "r1", Type: "infra.vpc", Config: nil},
	}
	fake := &stateReturningProvider{
		applyResult: &interfaces.ApplyResult{
			Resources: []interfaces.ResourceOutput{{Name: "r1", ProviderID: "vpc-1"}},
		},
	}
	store := &fakeStateStore{saveErr: fmt.Errorf("disk full")}

	err := applyWithProviderAndStore(t.Context(), fake, "fake-cloud", specs, nil, store, io.Discard, "")
	if err == nil {
		t.Fatal("expected save failure after apply, got nil")
	}
	if !strings.Contains(err.Error(), "persist state") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v, want hard state persistence failure", err)
	}
}

// TestApplyInfraModules_DisabledProviderError verifies that when an infra.*
// module references a provider that is explicitly disabled for the requested
// environment (environments[envName]: null), the error message says "disabled
// for environment" rather than "not declared".
func TestApplyInfraModules_DisabledProviderError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: fake-cloud
    environments:
      staging: null

  - name: my-db
    type: infra.database
    config:
      provider: do-provider
      engine: postgres
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := applyInfraModules(context.Background(), cfgPath, "staging")
	if err == nil {
		t.Fatal("expected error for disabled provider, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "disabled") || !strings.Contains(msg, "staging") {
		t.Errorf("error should mention 'disabled' and env name 'staging', got: %s", msg)
	}
}

// TestApplyInfraModules_MissingProvider verifies that a helpful error is returned
// when an infra.* module references a provider module that doesn't exist.
func TestApplyInfraModules_MissingProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: my-db
    type: infra.database
    config:
      provider: nonexistent-provider
      engine: postgres
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := applyInfraModules(context.Background(), cfgPath, "")
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
	if msg := err.Error(); msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

// ── TestApplyInfraModules_CallsResolveSizing_ForEachSpec ──────────────────────

// sizingCapture is an IaCProvider that records every ResolveSizing call and
// returns a concrete ProviderSizing so we can assert spec.Config is enriched.
type sizingCapture struct {
	applyCapture
	sizingCalls []struct {
		resType string
		size    interfaces.Size
	}
	sizingResult *interfaces.ProviderSizing
	appliedSpecs []interfaces.ResourceSpec
}

func (s *sizingCapture) ResolveSizing(resType string, size interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sizingCalls = append(s.sizingCalls, struct {
		resType string
		size    interfaces.Size
	}{resType: resType, size: size})
	return s.sizingResult, nil
}

func (s *sizingCapture) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range plan.Actions {
		s.appliedSpecs = append(s.appliedSpecs, a.Resource)
	}
	return &interfaces.ApplyResult{}, nil
}

// TestApplyInfraModules_CallsResolveSizing_ForEachSpec verifies that
// applyWithProviderAndStore invokes provider.ResolveSizing for each spec
// that has a non-empty Size field, and that the resolved InstanceType and
// extra Specs are merged into spec.Config before the plan is computed.
func TestApplyInfraModules_CallsResolveSizing_ForEachSpec(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Size: interfaces.SizeM, Config: map[string]any{"engine": "postgres"}},
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}}, // no Size → ResolveSizing should NOT be called
		{Name: "app", Type: "infra.container_service", Size: interfaces.SizeS, Config: map[string]any{"image": "nginx"}},
	}

	fake := &sizingCapture{
		sizingResult: &interfaces.ProviderSizing{
			InstanceType: "s-1vcpu-2gb",
			Specs:        map[string]any{"memory_mb": 2048},
		},
	}

	if err := applyWithProviderAndStore(t.Context(), fake, "fake-cloud", specs, nil, nil, io.Discard, ""); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}

	// ResolveSizing should have been called twice (db + app), not for vpc.
	fake.mu.Lock()
	calls := fake.sizingCalls
	applied := fake.appliedSpecs
	fake.mu.Unlock()

	if len(calls) != 2 {
		t.Errorf("ResolveSizing calls = %d, want 2 (only sized specs)", len(calls))
	}
	callTypes := map[string]interfaces.Size{}
	for _, c := range calls {
		callTypes[c.resType] = c.size
	}
	if callTypes["infra.database"] != interfaces.SizeM {
		t.Errorf("infra.database sizing call size = %q, want %q", callTypes["infra.database"], interfaces.SizeM)
	}
	if callTypes["infra.container_service"] != interfaces.SizeS {
		t.Errorf("infra.container_service sizing call size = %q, want %q", callTypes["infra.container_service"], interfaces.SizeS)
	}

	// The applied specs should carry the resolved instance_type in their Config.
	if len(applied) == 0 {
		t.Fatal("no specs were applied — Apply was not called or plan had no actions")
	}
	for _, s := range applied {
		if s.Size == "" {
			continue // vpc — no sizing expected
		}
		if s.Config["instance_type"] != "s-1vcpu-2gb" {
			t.Errorf("spec %q: Config[instance_type] = %v, want s-1vcpu-2gb", s.Name, s.Config["instance_type"])
		}
		if s.Config["memory_mb"] != 2048 {
			t.Errorf("spec %q: Config[memory_mb] = %v, want 2048", s.Name, s.Config["memory_mb"])
		}
	}
}

// TestHasInfraModules verifies detection of infra.* vs platform.* configs.
func TestHasInfraModules(t *testing.T) {
	dir := t.TempDir()

	withInfra := filepath.Join(dir, "with-infra.yaml")
	if err := os.WriteFile(withInfra, []byte(`
modules:
  - name: db
    type: infra.database
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	legacyOnly := filepath.Join(dir, "legacy.yaml")
	if err := os.WriteFile(legacyOnly, []byte(`
modules:
  - name: app
    type: platform.do_app
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if !hasInfraModules(withInfra) {
		t.Error("hasInfraModules: want true for infra.* config, got false")
	}
	if hasInfraModules(legacyOnly) {
		t.Error("hasInfraModules: want false for platform.* config, got true")
	}
}

// ── TestApplyWithProvider_LogsCloseError ──────────────────────────────────────

// errCloser is an io.Closer that always returns an error.
type errCloser struct{ msg string }

func (e *errCloser) Close() error { return fmt.Errorf("%s", e.msg) }

// TestApplyWithProvider_LogsCloseError verifies that when the provider closer
// returns an error during applyInfraModules, a warning is written to stderr
// (instead of silently discarding the error via nolint:errcheck).
func TestApplyWithProvider_LogsCloseError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: myprov
    type: iac.provider
    config:
      provider: fake-cloud
  - name: my-vpc
    type: infra.vpc
    config:
      provider: myprov
      region: nyc3
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Override resolveIaCProvider to return a provider + error-producing closer.
	orig := resolveIaCProvider
	fake := &applyCapture{}
	closerErr := "shutdown-sentinel-error"
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, &errCloser{msg: closerErr}, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	// Redirect stderr to capture warning output.
	oldStderr := os.Stderr
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = w.Close()
		_ = r.Close()
	})

	err := applyInfraModules(context.Background(), cfgPath, "")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	if _, readErr := buf.ReadFrom(r); readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("applyInfraModules returned unexpected error: %v", err)
	}
	if !strings.Contains(stderrOutput, closerErr) {
		t.Errorf("stderr = %q, want it to contain %q", stderrOutput, closerErr)
	}
	if !strings.Contains(stderrOutput, "warning") {
		t.Errorf("stderr = %q, want it to contain 'warning'", stderrOutput)
	}
}
