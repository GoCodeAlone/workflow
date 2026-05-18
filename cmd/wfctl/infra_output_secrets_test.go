package main

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// simpleProvider is a read/write/list fake for testing.
type simpleProvider struct {
	data map[string]string
}

// setCountProvider wraps simpleProvider and records every Set call. Use it
// when a test needs to assert that Set was or was not called a specific
// number of times (e.g. the "unchanged" optimisation path).
type setCountProvider struct {
	simpleProvider
	setCalls int
}

func (p *setCountProvider) Set(ctx context.Context, key, val string) error {
	p.setCalls++
	return p.simpleProvider.Set(ctx, key, val)
}

func newSimpleProvider() *simpleProvider {
	return &simpleProvider{data: map[string]string{}}
}

func (p *simpleProvider) Name() string { return "simple-fake" }

func (p *simpleProvider) Get(_ context.Context, key string) (string, error) {
	if v, ok := p.data[key]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}

func (p *simpleProvider) Set(_ context.Context, key, val string) error {
	p.data[key] = val
	return nil
}

func (p *simpleProvider) Delete(_ context.Context, key string) error {
	delete(p.data, key)
	return nil
}

func (p *simpleProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.data))
	for k := range p.data {
		keys = append(keys, k)
	}
	return keys, nil
}

// sampleStates returns a slice of ResourceState with known outputs.
func sampleStates() []interfaces.ResourceState {
	return []interfaces.ResourceState{
		{
			Name: "bmw-database",
			Type: "infra.database",
			Outputs: map[string]any{
				"uri":  "postgres://user:pass@db.example.com:5432/app",
				"host": "db.example.com",
			},
		},
		{
			Name: "bmw-cache",
			Type: "infra.cache",
			Outputs: map[string]any{
				"url": "redis://cache.example.com:6379",
			},
		},
	}
}

// ── buildStateOutputsMap ──────────────────────────────────────────────────────

func TestBuildStateOutputsMap_Basic(t *testing.T) {
	states := sampleStates()
	m := buildStateOutputsMap(states)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m["bmw-database"]["uri"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("bmw-database.uri: got %v", m["bmw-database"]["uri"])
	}
	if m["bmw-cache"]["url"] != "redis://cache.example.com:6379" {
		t.Errorf("bmw-cache.url: got %v", m["bmw-cache"]["url"])
	}
}

func TestBuildStateOutputsMap_NilOutputsSkipped(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "no-outputs", Type: "infra.vpc"},
		{Name: "has-outputs", Type: "infra.database", Outputs: map[string]any{"uri": "pg://..."}},
	}
	m := buildStateOutputsMap(states)
	if _, ok := m["no-outputs"]; ok {
		t.Error("module with nil outputs should not appear in map")
	}
	if m["has-outputs"]["uri"] != "pg://..." {
		t.Errorf("unexpected: %v", m["has-outputs"])
	}
}

func TestBuildStateOutputsMap_Empty(t *testing.T) {
	m := buildStateOutputsMap(nil)
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// ── syncInfraOutputSecrets ────────────────────────────────────────────────────

func TestSyncInfraOutputSecrets_NilConfig(t *testing.T) {
	p := newSimpleProvider()
	err := syncInfraOutputSecrets(context.Background(), nil, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("nil config should be no-op: %v", err)
	}
	if len(p.data) != 0 {
		t.Errorf("no secrets should be written: %v", p.data)
	}
}

func TestSyncInfraOutputSecrets_NoInfraOutputGens(t *testing.T) {
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 32},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("no infra_output generators should be no-op: %v", err)
	}
	if len(p.data) != 0 {
		t.Errorf("no secrets should be written: %v", p.data)
	}
}

func TestSyncInfraOutputSecrets_WritesSecret(t *testing.T) {
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "STAGING_DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if p.data["STAGING_DATABASE_URL"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("STAGING_DATABASE_URL: got %q", p.data["STAGING_DATABASE_URL"])
	}
}

func TestSyncInfraOutputSecrets_RoutesGeneratorToNamedStore(t *testing.T) {
	envProvider := secrets.NewEnvProvider("ROUTED_")
	_ = envProvider.Delete(context.Background(), "DATABASE_URL")
	t.Cleanup(func() { _ = envProvider.Delete(context.Background(), "DATABASE_URL") })
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri", Store: "github-env"},
		},
	}
	wfCfg := &config.WorkflowConfig{
		SecretStores: map[string]*config.SecretStoreConfig{
			"github-env": {
				Provider: "env",
				Config:   map[string]any{"prefix": "ROUTED_"},
			},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), wfCfg, "staging", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if _, ok := p.data["DATABASE_URL"]; ok {
		t.Fatalf("default provider received DATABASE_URL; wanted named store routing")
	}
	if got := os.Getenv("ROUTED_DATABASE_URL"); got != "postgres://user:pass@db.example.com:5432/app" {
		t.Fatalf("ROUTED_DATABASE_URL = %q, want infra output", got)
	}
}

func TestSyncInfraOutputSecrets_WritesMultiple(t *testing.T) {
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
			{Key: "REDIS_URL", Type: "infra_output", Source: "bmw-cache.url"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if p.data["DATABASE_URL"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("DATABASE_URL: got %q", p.data["DATABASE_URL"])
	}
	if p.data["REDIS_URL"] != "redis://cache.example.com:6379" {
		t.Errorf("REDIS_URL: got %q", p.data["REDIS_URL"])
	}
}

func TestSyncInfraOutputSecrets_SkipsExisting(t *testing.T) {
	p := newSimpleProvider()
	p.data["DATABASE_URL"] = "already-set"
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	// Must not overwrite the existing value.
	if p.data["DATABASE_URL"] != "already-set" {
		t.Errorf("existing secret should not be overwritten: got %q", p.data["DATABASE_URL"])
	}
}

func TestSyncInfraOutputSecrets_WriteOnlyProviderSkips(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{"DATABASE_URL"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if len(p.stored) != 0 {
		t.Errorf("should not write to write-only provider when secret exists: %v", p.stored)
	}
}

func TestSyncInfraOutputSecrets_WriteOnlyProviderWrites(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if p.stored["DATABASE_URL"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("DATABASE_URL: got %q", p.stored["DATABASE_URL"])
	}
}

func TestSyncInfraOutputSecrets_MissingModule(t *testing.T) {
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "X", Type: "infra_output", Source: "nonexistent.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err == nil {
		t.Fatal("expected error for missing module in state")
	}
}

func TestSyncInfraOutputSecrets_EmptyStates(t *testing.T) {
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "X", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, nil, nil, "", nil, false)
	if err == nil {
		t.Fatal("expected error when state has no matching module")
	}
}

// ── syncInfraOutputSecrets --refresh-outputs mode ─────────────────────────────

// TestSyncInfraOutputSecrets_RefreshOutputs_UpdatesStaleSecret verifies that
// when refreshOutputs=true and the existing secret value differs from the
// current infra output, the secret is updated (not skipped).
func TestSyncInfraOutputSecrets_RefreshOutputs_UpdatesStaleSecret(t *testing.T) {
	p := newSimpleProvider()
	p.data["DATABASE_URL"] = "postgres://old-host:5432/app" // stale value
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, true)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	// Must overwrite stale value with the current infra output.
	if p.data["DATABASE_URL"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("DATABASE_URL: got %q, want current infra output", p.data["DATABASE_URL"])
	}
}

// TestSyncInfraOutputSecrets_RefreshOutputs_UnchangedSkipsSet verifies that
// when refreshOutputs=true and the existing secret already matches the infra
// output, provider.Set is NOT called — the "unchanged" optimisation must not
// be masked by a write of the same value.
func TestSyncInfraOutputSecrets_RefreshOutputs_UnchangedSkipsSet(t *testing.T) {
	current := "postgres://user:pass@db.example.com:5432/app"
	// setCountProvider records how many times Set is called. If the
	// "unchanged" path fires, the count must be 0.
	p := &setCountProvider{simpleProvider: simpleProvider{data: map[string]string{"DATABASE_URL": current}}}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, true)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if p.setCalls != 0 {
		t.Errorf("Set must not be called when value is unchanged; called %d time(s)", p.setCalls)
	}
	if p.data["DATABASE_URL"] != current {
		t.Errorf("value must remain unchanged: got %q", p.data["DATABASE_URL"])
	}
}

// TestSyncInfraOutputSecrets_RefreshOutputs_CreatesNew verifies that when
// refreshOutputs=true and the secret does not yet exist, it is created (same
// as normal mode).
func TestSyncInfraOutputSecrets_RefreshOutputs_CreatesNew(t *testing.T) {
	p := newSimpleProvider()
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, true)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if p.data["DATABASE_URL"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("DATABASE_URL: got %q", p.data["DATABASE_URL"])
	}
}

// TestSyncInfraOutputSecrets_RefreshOutputs_WriteOnlyProviderUpdates verifies
// that for write-only providers (GitHub Actions style), --refresh-outputs
// always overwrites an existing secret because comparison is not possible.
func TestSyncInfraOutputSecrets_RefreshOutputs_WriteOnlyProviderUpdates(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{"DATABASE_URL"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, true)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	// Write-only provider cannot compare, so Set must have been called.
	if p.stored["DATABASE_URL"] != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("DATABASE_URL: got %q, want infra output value", p.stored["DATABASE_URL"])
	}
}

// TestSyncInfraOutputSecrets_NormalMode_DoesNotOverwrite verifies the
// invariant that normal apply (refreshOutputs=false) never overwrites a
// user-managed secret, even when the infra output value has changed.
func TestSyncInfraOutputSecrets_NormalMode_DoesNotOverwrite(t *testing.T) {
	p := newSimpleProvider()
	p.data["DATABASE_URL"] = "user-managed-value"
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	err := syncInfraOutputSecrets(context.Background(), cfg, p, sampleStates(), nil, "", nil, false)
	if err != nil {
		t.Fatalf("syncInfraOutputSecrets: %v", err)
	}
	if p.data["DATABASE_URL"] != "user-managed-value" {
		t.Errorf("normal apply must not overwrite existing secret: got %q", p.data["DATABASE_URL"])
	}
}

// ── bootstrapSecrets skips infra_output ───────────────────────────────────────

func TestBootstrapSecrets_SkipsInfraOutputGens(t *testing.T) {
	generatorCalled := false
	withStubGenerator(t, func(_ context.Context, genType string, _ map[string]any) (string, error) {
		if genType == "infra_output" {
			generatorCalled = true
			return "", fmt.Errorf("infra_output should not be called during bootstrap")
		}
		return "random-value", nil
	})
	p := &writeOnlyProvider{listOK: true}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 32},
			{Key: "DATABASE_URL", Type: "infra_output", Source: "bmw-database.uri"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if generatorCalled {
		t.Error("infra_output generator must not be called during bootstrap")
	}
	// JWT_SECRET should be written; DATABASE_URL should not.
	if _, ok := p.stored["JWT_SECRET"]; !ok {
		t.Error("JWT_SECRET should have been generated")
	}
	if _, ok := p.stored["DATABASE_URL"]; ok {
		t.Error("DATABASE_URL (infra_output) should not be set during bootstrap")
	}
}
