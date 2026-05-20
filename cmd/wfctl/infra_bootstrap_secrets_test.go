package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

// writeOnlyProvider simulates a GitHub-style provider where Get is not
// supported but List returns known names.
type writeOnlyProvider struct {
	existing  []string
	stored    map[string]string
	getCalls  int
	listCalls int
	listOK    bool
}

func (p *writeOnlyProvider) Name() string { return "write-only-fake" }

func (p *writeOnlyProvider) Get(_ context.Context, _ string) (string, error) {
	p.getCalls++
	return "", secrets.ErrUnsupported
}

func (p *writeOnlyProvider) Set(_ context.Context, key, value string) error {
	if p.stored == nil {
		p.stored = map[string]string{}
	}
	p.stored[key] = value
	return nil
}

func (p *writeOnlyProvider) Delete(_ context.Context, _ string) error {
	return nil
}

func (p *writeOnlyProvider) List(_ context.Context) ([]string, error) {
	p.listCalls++
	if !p.listOK {
		return nil, secrets.ErrUnsupported
	}
	return append([]string(nil), p.existing...), nil
}

// withStubGenerator swaps the package-level generateSecret for the duration
// of the test, so provider_credential paths don't reach out to cloud APIs.
func withStubGenerator(t *testing.T, fn func(ctx context.Context, genType string, cfg map[string]any) (string, error)) {
	t.Helper()
	prev := generateSecret
	generateSecret = fn
	t.Cleanup(func() { generateSecret = prev })
}

// TestBootstrapSecrets_WriteOnlyProviderSkipsExisting verifies that when the
// provider is write-only (GitHub Actions), bootstrapSecrets consults List()
// and skips regeneration if the secret name already exists. Without this,
// every bootstrap run regenerates, and for provider_credential that orphans
// upstream credentials (e.g. DO Spaces access keys).
func TestBootstrapSecrets_WriteOnlyProviderSkipsExisting(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{"JWT_SECRET", "SPACES_access_key", "SPACES_secret_key"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 32},
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 0 {
		t.Fatalf("stored = %v, want empty (all secrets already exist)", p.stored)
	}
	if p.listCalls != 1 {
		t.Fatalf("List called %d times, want 1 (should be cached)", p.listCalls)
	}
}

// TestBootstrapSecrets_WriteOnlyProviderGeneratesWhenMissing verifies the
// fallback still generates when List shows the name is absent.
func TestBootstrapSecrets_WriteOnlyProviderGeneratesWhenMissing(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{"UNRELATED"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 8},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if _, ok := p.stored["JWT_SECRET"]; !ok {
		t.Fatalf("JWT_SECRET was not stored; stored=%v", p.stored)
	}
}

// TestBootstrapSecrets_WriteOnlyProviderListUnsupported verifies that when
// both Get and List return ErrUnsupported, bootstrap regenerates (preserves
// prior behaviour for providers with no introspection at all).
func TestBootstrapSecrets_WriteOnlyProviderListUnsupported(t *testing.T) {
	p := &writeOnlyProvider{listOK: false}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 8},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 1 {
		t.Fatalf("stored = %v, want 1 entry (List unsupported → regenerate)", p.stored)
	}
}

// TestBootstrapSecrets_ProviderCredentialAllSubKeysPresent verifies the
// provider_credential skip path: both access_key and secret_key sub-keys
// must exist before the generator is skipped.
func TestBootstrapSecrets_ProviderCredentialAllSubKeysPresent(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		t.Fatal("generator must not be called when both sub-keys already exist")
		return "", nil
	})
	p := &writeOnlyProvider{
		existing: []string{"SPACES_access_key", "SPACES_secret_key"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 0 {
		t.Fatalf("stored = %v, want empty", p.stored)
	}
}

// TestBootstrapSecrets_ProviderCredentialPartialRegenerates verifies that a
// partial prior write (one sub-key missing) triggers regeneration.
func TestBootstrapSecrets_ProviderCredentialPartialRegenerates(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "new-access",
			"secret_key": "new-secret",
		})
		return string(out), nil
	})
	// Only the access_key is present — the secret_key is missing, so the
	// stored credential is unusable and bootstrap must regenerate.
	p := &writeOnlyProvider{
		existing: []string{"SPACES_access_key"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if got := p.stored["SPACES_access_key"]; got != "new-access" {
		t.Errorf("SPACES_access_key = %q, want %q", got, "new-access")
	}
	if got := p.stored["SPACES_secret_key"]; got != "new-secret" {
		t.Errorf("SPACES_secret_key = %q, want %q", got, "new-secret")
	}
}

// TestBootstrapSecrets_StorageFilter_OnlyPersistsSubKeys verifies that
// provider_credential JSON is filtered to the canonical sub-keys defined in
// providerCredentialSubKeys before being persisted as GH Secrets. Without
// this filter, sidecar metadata that the generator now emits alongside the
// canonical creds (e.g. created_at after Task 8) would leak into the GH
// Secrets store as phantom keys like SPACES_created_at — breaking the
// audit-keys/prune contract that "every GH Secret matches an upstream key"
// (ADR 0020 same-commit constraint with Task 8).
//
// This is the failing test for Task 9 of the spaces-key-iac-resource plan.
// Until Task 10 implements the sub-key allow-list filter in bootstrapSecrets,
// this test fails at the SPACES_created_at assertion.
func TestBootstrapSecrets_StorageFilter_OnlyPersistsSubKeys(t *testing.T) {
	// Stub generateSecret to mimic the post-Task-8 generateDOSpacesKey shape:
	// access_key + secret_key (canonical) plus created_at (sidecar metadata).
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK",
			"secret_key": "SK",
			"created_at": "2026-05-08T10:00:00Z",
		})
		return string(out), nil
	})

	// Empty existing → bootstrap will generate.
	p := &writeOnlyProvider{
		existing: nil,
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{
				Key:    "SPACES",
				Type:   "provider_credential",
				Source: "digitalocean.spaces",
				Name:   "test-key",
			},
		},
	}

	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	// Storage MUST contain the two canonical sub-keys.
	if _, ok := p.stored["SPACES_access_key"]; !ok {
		t.Errorf("SPACES_access_key should be stored; stored=%v", p.stored)
	}
	if _, ok := p.stored["SPACES_secret_key"]; !ok {
		t.Errorf("SPACES_secret_key should be stored; stored=%v", p.stored)
	}

	// Storage MUST NOT contain sidecar metadata fields like created_at:
	// these are not real GH Secrets and would pollute audit-keys/prune output.
	if _, ok := p.stored["SPACES_created_at"]; ok {
		t.Errorf("SPACES_created_at MUST NOT be stored as a GH Secret (storage-filter regression); stored=%v", p.stored)
	}
}

// TestBootstrapSecrets_ProviderCredentialProbeIgnoresBareKey verifies that a
// plain secret named the same as the provider_credential key (without the
// _access_key / _secret_key suffixes) does not cause a false skip.
func TestBootstrapSecrets_ProviderCredentialProbeIgnoresBareKey(t *testing.T) {
	generateCalls := 0
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		generateCalls++
		out, _ := json.Marshal(map[string]string{
			"access_key": "a",
			"secret_key": "b",
		})
		return string(out), nil
	})
	// "SPACES" is present, but the real sub-keys are not — must regenerate.
	p := &writeOnlyProvider{
		existing: []string{"SPACES"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if generateCalls != 1 {
		t.Fatalf("generator called %d times, want 1", generateCalls)
	}
}

// failOnSetProvider triggers a Set() failure on the first matching key
// — used to exercise the transactional rollback path when a
// provider_credential creation succeeds upstream but the store-write
// half fails (e.g. GH PAT lost permissions mid-bootstrap).
type failOnSetProvider struct {
	failKey  string
	setCalls []string
}

func (p *failOnSetProvider) Name() string { return "fail-on-set" }
func (p *failOnSetProvider) Get(_ context.Context, _ string) (string, error) {
	return "", secrets.ErrUnsupported
}
func (p *failOnSetProvider) Set(_ context.Context, key, _ string) error {
	p.setCalls = append(p.setCalls, key)
	if key == p.failKey {
		return errFakeStoreUnavailable
	}
	return nil
}
func (p *failOnSetProvider) Delete(_ context.Context, _ string) error { return nil }
func (p *failOnSetProvider) List(_ context.Context) ([]string, error) {
	return nil, secrets.ErrUnsupported
}

// recordingRevoker captures RevokeProviderCredential calls so the test
// can assert rollback occurred. Implements interfaces.ProviderCredentialRevoker.
type recordingRevoker struct {
	calls []revokeCall
}
type revokeCall struct {
	source       string
	credentialID string
}

func (r *recordingRevoker) RevokeProviderCredential(_ context.Context, source, credentialID string) error {
	r.calls = append(r.calls, revokeCall{source: source, credentialID: credentialID})
	return nil
}

var errFakeStoreUnavailable = errFakeStore("store unavailable (simulated)")

type errFakeStore string

func (e errFakeStore) Error() string { return string(e) }

// TestBootstrapSecrets_ProviderCredential_RollbackOnSetFailure is the
// regression test for the orphan-key bug: when generateSecret returns a
// fresh DO Spaces key but provider.Set fails to persist it, bootstrap
// MUST revoke the just-minted upstream credential.
//
// Pre-fix behaviour: Set fails → return error → DO key remains in the
// account with an unrecoverable secret_key. Every subsequent run mints
// another orphan with the same name.
//
// Post-fix: Set failure triggers credRevoker.RevokeProviderCredential
// with the access_key from the just-generated subKeyMap.
func TestBootstrapSecrets_ProviderCredential_RollbackOnSetFailure(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK_ORPHAN",
			"secret_key": "SK_DOOMED",
		})
		return string(out), nil
	})

	p := &failOnSetProvider{failKey: "SPACES_secret_key"}
	rev := &recordingRevoker{}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}

	_, _, err := bootstrapSecrets(context.Background(), p, cfg, nil, rev)
	if err == nil {
		t.Fatal("expected Set failure to surface as error")
	}

	if len(rev.calls) != 1 {
		t.Fatalf("expected 1 rollback-revoke call; got %d", len(rev.calls))
	}
	if rev.calls[0].credentialID != "AK_ORPHAN" {
		t.Errorf("rollback called with credentialID=%q want AK_ORPHAN", rev.calls[0].credentialID)
	}
	if rev.calls[0].source != "digitalocean.spaces" {
		t.Errorf("rollback source=%q want digitalocean.spaces", rev.calls[0].source)
	}
}

// TestBootstrapSecrets_ProviderCredential_RollbackOnFirstSetFailure
// guards the most insidious shape of the bug: the very first Set call
// fails (e.g. access_key write). The pre-fix code extracted
// newAccessKey *during* the for-range loop, so a first-iteration
// failure left newAccessKey empty even though the upstream key exists.
// The fix extracts access_key BEFORE the loop.
func TestBootstrapSecrets_ProviderCredential_RollbackOnFirstSetFailure(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK_FIRST",
			"secret_key": "SK_FIRST",
		})
		return string(out), nil
	})

	p := &failOnSetProvider{failKey: "SPACES_access_key"}
	rev := &recordingRevoker{}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}

	_, _, err := bootstrapSecrets(context.Background(), p, cfg, nil, rev)
	if err == nil {
		t.Fatal("expected Set failure")
	}
	if len(rev.calls) != 1 || rev.calls[0].credentialID != "AK_FIRST" {
		t.Errorf("rollback calls = %v; want one revoke of AK_FIRST", rev.calls)
	}
}
