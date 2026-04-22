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
	existing []string
	stored   map[string]string
	getCalls int
	listCalls int
	listOK   bool
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
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
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
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
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
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
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
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
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
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if got := p.stored["SPACES_access_key"]; got != "new-access" {
		t.Errorf("SPACES_access_key = %q, want %q", got, "new-access")
	}
	if got := p.stored["SPACES_secret_key"]; got != "new-secret" {
		t.Errorf("SPACES_secret_key = %q, want %q", got, "new-secret")
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
	if _, err := bootstrapSecrets(context.Background(), p, cfg); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if generateCalls != 1 {
		t.Fatalf("generator called %d times, want 1", generateCalls)
	}
}
