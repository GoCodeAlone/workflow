package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
)

// trackingProvider wraps an in-memory store and records Delete/Set calls so
// tests can assert which keys were deleted and written during force-rotate.
type trackingProvider struct {
	inner       map[string]string // key → value (mutable in-memory store)
	deleteCalls []string
	setCalls    []string
}

func newTrackingProvider(initial map[string]string) *trackingProvider {
	store := make(map[string]string, len(initial))
	for k, v := range initial {
		store[k] = v
	}
	return &trackingProvider{inner: store}
}

func (p *trackingProvider) Name() string { return "tracking" }

func (p *trackingProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := p.inner[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func (p *trackingProvider) Set(_ context.Context, key, value string) error {
	if p.inner == nil {
		p.inner = map[string]string{}
	}
	p.inner[key] = value
	p.setCalls = append(p.setCalls, key)
	return nil
}

func (p *trackingProvider) Delete(_ context.Context, key string) error {
	delete(p.inner, key)
	p.deleteCalls = append(p.deleteCalls, key)
	return nil
}

func (p *trackingProvider) List(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(p.inner))
	for k := range p.inner {
		names = append(names, k)
	}
	return names, nil
}

// containsSlice returns true if slice contains s.
func containsSlice(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// TestInfraBootstrap_ForceRotate_DeletesAndRegenerates is the core happy-path
// test: existing secret FOO="old" is deleted then regenerated with a new value
// that differs from "old".
func TestInfraBootstrap_ForceRotate_DeletesAndRegenerates(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "new-generated-value", nil
	})

	p := newTrackingProvider(map[string]string{"FOO": "old"})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "FOO", Type: "random_hex", Length: 32},
		},
	}
	forceRotate := map[string]bool{"FOO": true}

	result, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
	if err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	// Delete must have been called.
	if !containsSlice(p.deleteCalls, "FOO") {
		t.Errorf("Delete(FOO) not called; deleteCalls=%v", p.deleteCalls)
	}
	// Set must have been called.
	if !containsSlice(p.setCalls, "FOO") {
		t.Errorf("Set(FOO) not called; setCalls=%v", p.setCalls)
	}
	// New value must differ from old and be non-empty.
	newVal := p.inner["FOO"]
	if newVal == "old" || newVal == "" {
		t.Errorf("FOO = %q, want new non-empty value differing from %q", newVal, "old")
	}
	// The new value must appear in the returned generated map.
	if result["FOO"] != newVal {
		t.Errorf("generated[FOO] = %q, want %q", result["FOO"], newVal)
	}
}

// TestInfraBootstrap_ForceRotate_UnknownNameFailsFast asserts that a name
// that doesn't match any secrets.generate entry is rejected before touching
// the store.
func TestInfraBootstrap_ForceRotate_UnknownNameFailsFast(t *testing.T) {
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "REAL_KEY", Type: "random_hex", Length: 16},
		},
	}
	rotateNames := multiStringFlag{"BAZ"}
	_, err := buildForceRotateSet(rotateNames, cfg)
	if err == nil {
		t.Fatal("expected error for unknown name, got nil")
	}
	if !strings.Contains(err.Error(), "no secrets.generate entry named") || !strings.Contains(err.Error(), "BAZ") {
		t.Errorf("error message does not mention expected content; got: %v", err)
	}
}

// TestInfraBootstrap_ForceRotate_ProviderCredentialAllowed asserts that
// --force-rotate on a provider_credential type is now permitted (ADR 0012).
// The old rejection ("must be rotated via the upstream provider") was removed
// to enable wfctl-managed mint-new-then-revoke-old rotation.
func TestInfraBootstrap_ForceRotate_ProviderCredentialAllowed(t *testing.T) {
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	rotateNames := multiStringFlag{"SPACES"}
	set, err := buildForceRotateSet(rotateNames, cfg)
	if err != nil {
		t.Fatalf("buildForceRotateSet: expected no error for provider_credential, got: %v", err)
	}
	if !set["SPACES"] {
		t.Errorf("force-rotate set missing SPACES; got: %v", set)
	}
}

// TestInfraBootstrap_ForceRotate_ProviderCredential_MintsAndRevokes verifies
// the full mint-new-then-revoke-old flow for provider_credential force-rotate.
// The revoker captures the old credentialID; the new sub-keys are stored.
func TestInfraBootstrap_ForceRotate_ProviderCredential_MintsAndRevokes(t *testing.T) {
	// Stub generator returns a new DO Spaces credential JSON.
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return `{"access_key":"NEW_AK","secret_key":"NEW_SK"}`, nil
	})

	// Store has old sub-keys pre-populated.
	p := newTrackingProvider(map[string]string{
		"SPACES_access_key": "OLD_AK",
		"SPACES_secret_key": "OLD_SK",
	})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	forceRotate := map[string]bool{"SPACES": true}

	// Capture revoke calls.
	var revokedSource, revokedID string
	revoker := &stubProviderRevoker{
		fn: func(_ context.Context, source, credentialID string) error {
			revokedSource = source
			revokedID = credentialID
			return nil
		},
	}

	result, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate, revoker)
	if err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	// New sub-keys should be stored.
	if p.inner["SPACES_access_key"] != "NEW_AK" {
		t.Errorf("SPACES_access_key = %q, want NEW_AK", p.inner["SPACES_access_key"])
	}
	if p.inner["SPACES_secret_key"] != "NEW_SK" {
		t.Errorf("SPACES_secret_key = %q, want NEW_SK", p.inner["SPACES_secret_key"])
	}
	// Result map should contain new values.
	if result["SPACES_access_key"] != "NEW_AK" {
		t.Errorf("generated[SPACES_access_key] = %q, want NEW_AK", result["SPACES_access_key"])
	}
	// Revocation should have been called with old access_key_id.
	if revokedSource != "digitalocean.spaces" {
		t.Errorf("revokedSource = %q, want digitalocean.spaces", revokedSource)
	}
	if revokedID != "OLD_AK" {
		t.Errorf("revokedID = %q, want OLD_AK", revokedID)
	}
}

// TestInfraBootstrap_ForceRotate_ProviderCredential_RevokeFailNonFatal verifies
// that when revocation fails, the new credential is still retained (non-fatal).
func TestInfraBootstrap_ForceRotate_ProviderCredential_RevokeFailNonFatal(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return `{"access_key":"NEW_AK2","secret_key":"NEW_SK2"}`, nil
	})

	p := newTrackingProvider(map[string]string{
		"SPACES_access_key": "OLD_AK2",
		"SPACES_secret_key": "OLD_SK2",
	})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	forceRotate := map[string]bool{"SPACES": true}

	// Revoker that always fails.
	revoker := &stubProviderRevoker{
		fn: func(_ context.Context, _, _ string) error {
			return fmt.Errorf("simulated revoke failure")
		},
	}

	result, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate, revoker)
	if err != nil {
		t.Fatalf("bootstrapSecrets should not fail on revoke error; got: %v", err)
	}
	// New credential MUST still be stored even when revoke fails.
	if p.inner["SPACES_access_key"] != "NEW_AK2" {
		t.Errorf("SPACES_access_key = %q, want NEW_AK2 (must not roll back on revoke fail)", p.inner["SPACES_access_key"])
	}
	if result["SPACES_access_key"] != "NEW_AK2" {
		t.Errorf("generated[SPACES_access_key] = %q, want NEW_AK2", result["SPACES_access_key"])
	}
}

// stubProviderRevoker is a test double for interfaces.ProviderCredentialRevoker.
type stubProviderRevoker struct {
	fn func(ctx context.Context, source, credentialID string) error
}

func (r *stubProviderRevoker) RevokeProviderCredential(ctx context.Context, source, credentialID string) error {
	return r.fn(ctx, source, credentialID)
}

// TestInfraBootstrap_ForceRotate_ProviderCredential_WriteOnlyStore verifies that
// when the secrets provider is write-only (Get returns ErrUnsupported, e.g.
// GitHub Actions), revocation is skipped with a warning and the new credential
// is still stored. The revoker MUST NOT be called because the old credential ID
// is unknown (ADR 0012 §write-only contract).
func TestInfraBootstrap_ForceRotate_ProviderCredential_WriteOnlyStore(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return `{"access_key":"WO_AK","secret_key":"WO_SK"}`, nil
	})

	// writeOnlyGetUnsupportedProvider returns ErrUnsupported from Get (GitHub Actions model).
	p := &writeOnlyGetUnsupportedProvider{inner: map[string]string{}}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	forceRotate := map[string]bool{"SPACES": true}

	// Revoker must NOT be called when the old credential ID is unknown.
	revokeCalled := false
	revoker := &stubProviderRevoker{
		fn: func(_ context.Context, _, _ string) error {
			revokeCalled = true
			return nil
		},
	}

	result, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate, revoker)
	if err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	// New credential stored.
	if result["SPACES_access_key"] != "WO_AK" {
		t.Errorf("generated[SPACES_access_key] = %q, want WO_AK", result["SPACES_access_key"])
	}
	// Revoker must not have been called (no old credential ID to revoke).
	if revokeCalled {
		t.Error("revoker was called despite write-only provider (old credential ID unknown); must not revoke")
	}
}

// writeOnlyGetUnsupportedProvider is a secrets.Provider where Get returns ErrUnsupported
// (simulates GitHub Actions secrets — write-only, no read access).
type writeOnlyGetUnsupportedProvider struct {
	inner map[string]string
}

func (p *writeOnlyGetUnsupportedProvider) Name() string { return "write-only" }
func (p *writeOnlyGetUnsupportedProvider) Get(_ context.Context, _ string) (string, error) {
	return "", secrets.ErrUnsupported
}
func (p *writeOnlyGetUnsupportedProvider) Set(_ context.Context, key, value string) error {
	if p.inner == nil {
		p.inner = map[string]string{}
	}
	p.inner[key] = value
	return nil
}
func (p *writeOnlyGetUnsupportedProvider) Delete(_ context.Context, _ string) error { return nil }
func (p *writeOnlyGetUnsupportedProvider) List(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(p.inner))
	for k := range p.inner {
		names = append(names, k)
	}
	return names, nil
}

// TestInfraBootstrap_ForceRotate_BestEffortDeleteOnMissing asserts that when
// the secret doesn't exist in the store yet, bootstrap still creates the fresh
// value and returns no error (Delete returns ErrNotFound → warning, no error).
func TestInfraBootstrap_ForceRotate_BestEffortDeleteOnMissing(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "brand-new", nil
	})

	// Empty store — FOO doesn't exist. Our trackingProvider.Delete removes from
	// the map (noop when absent) and records the call. The real behaviour of
	// providers on a missing key returns ErrNotFound; we simulate that by using
	// a missingKeyProvider below so we can verify best-effort handling.
	p := newTrackingProvider(map[string]string{})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "FOO", Type: "random_hex", Length: 16},
		},
	}
	forceRotate := map[string]bool{"FOO": true}

	_, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
	if err != nil {
		t.Fatalf("bootstrapSecrets returned error on missing key: %v", err)
	}
	// Set must have been called and the value stored.
	if p.inner["FOO"] != "brand-new" {
		t.Errorf("FOO = %q, want %q", p.inner["FOO"], "brand-new")
	}
}

// TestInfraBootstrap_ForceRotate_BestEffortDeleteOnMissing_ErrNotFound uses a
// provider whose Delete returns ErrNotFound to verify that the best-effort
// path logs a warning and continues without error.
func TestInfraBootstrap_ForceRotate_BestEffortDeleteNotFound(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "fresh", nil
	})

	// Provider returns ErrNotFound on Delete (simulates write-only provider where
	// the key doesn't exist yet).
	p := &notFoundDeleteProvider{inner: map[string]string{}}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "FOO", Type: "random_hex", Length: 16},
		},
	}
	forceRotate := map[string]bool{"FOO": true}

	_, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
	if err != nil {
		t.Fatalf("expected no error on best-effort delete; got: %v", err)
	}
	if p.inner["FOO"] != "fresh" {
		t.Errorf("FOO = %q, want %q", p.inner["FOO"], "fresh")
	}
}

// notFoundDeleteProvider is a provider that returns ErrNotFound from Delete.
type notFoundDeleteProvider struct {
	inner map[string]string
}

func (p *notFoundDeleteProvider) Name() string { return "not-found-delete" }
func (p *notFoundDeleteProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := p.inner[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *notFoundDeleteProvider) Set(_ context.Context, key, value string) error {
	if p.inner == nil {
		p.inner = map[string]string{}
	}
	p.inner[key] = value
	return nil
}
func (p *notFoundDeleteProvider) Delete(_ context.Context, _ string) error {
	return secrets.ErrNotFound
}
func (p *notFoundDeleteProvider) List(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(p.inner))
	for k := range p.inner {
		names = append(names, k)
	}
	return names, nil
}

// TestInfraBootstrap_ForceRotate_CommaSeparated asserts that a single
// --force-rotate FOO,BAR flag value rotates both FOO and BAR.
func TestInfraBootstrap_ForceRotate_CommaSeparated(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "rotated-value", nil
	})

	p := newTrackingProvider(map[string]string{"FOO": "old-foo", "BAR": "old-bar"})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "FOO", Type: "random_hex", Length: 16},
			{Key: "BAR", Type: "random_hex", Length: 16},
		},
	}

	// Simulate --force-rotate FOO,BAR (single flag value with comma).
	var rotateNames multiStringFlag
	if err := rotateNames.Set("FOO,BAR"); err != nil {
		t.Fatal(err)
	}
	forceRotate, err := buildForceRotateSet(rotateNames, cfg)
	if err != nil {
		t.Fatalf("buildForceRotateSet: %v", err)
	}

	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	if !containsSlice(p.deleteCalls, "FOO") {
		t.Errorf("Delete(FOO) not called; deleteCalls=%v", p.deleteCalls)
	}
	if !containsSlice(p.deleteCalls, "BAR") {
		t.Errorf("Delete(BAR) not called; deleteCalls=%v", p.deleteCalls)
	}
	if p.inner["FOO"] != "rotated-value" {
		t.Errorf("FOO = %q, want %q", p.inner["FOO"], "rotated-value")
	}
	if p.inner["BAR"] != "rotated-value" {
		t.Errorf("BAR = %q, want %q", p.inner["BAR"], "rotated-value")
	}
}

// TestInfraBootstrap_ForceRotate_Repeatable asserts that --force-rotate FOO
// --force-rotate BAR (two separate flag invocations) rotates both.
func TestInfraBootstrap_ForceRotate_Repeatable(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "new-value", nil
	})

	p := newTrackingProvider(map[string]string{"FOO": "f", "BAR": "b"})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "FOO", Type: "random_hex", Length: 16},
			{Key: "BAR", Type: "random_base64", Length: 16},
		},
	}

	// Simulate --force-rotate FOO --force-rotate BAR (two separate flag values).
	var rotateNames multiStringFlag
	if err := rotateNames.Set("FOO"); err != nil {
		t.Fatal(err)
	}
	if err := rotateNames.Set("BAR"); err != nil {
		t.Fatal(err)
	}
	forceRotate, err := buildForceRotateSet(rotateNames, cfg)
	if err != nil {
		t.Fatalf("buildForceRotateSet: %v", err)
	}

	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	if !containsSlice(p.deleteCalls, "FOO") {
		t.Errorf("Delete(FOO) not called; deleteCalls=%v", p.deleteCalls)
	}
	if !containsSlice(p.deleteCalls, "BAR") {
		t.Errorf("Delete(BAR) not called; deleteCalls=%v", p.deleteCalls)
	}
	if p.inner["FOO"] != "new-value" {
		t.Errorf("FOO = %q, want %q", p.inner["FOO"], "new-value")
	}
	if p.inner["BAR"] != "new-value" {
		t.Errorf("BAR = %q, want %q", p.inner["BAR"], "new-value")
	}
}
