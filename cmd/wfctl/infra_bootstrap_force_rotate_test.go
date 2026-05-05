package main

import (
	"context"
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

	result, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
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

// TestInfraBootstrap_ForceRotate_ProviderCredentialRefused asserts that
// --force-rotate on a provider_credential type returns a descriptive error
// before touching the store.
func TestInfraBootstrap_ForceRotate_ProviderCredentialRefused(t *testing.T) {
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	rotateNames := multiStringFlag{"SPACES"}
	_, err := buildForceRotateSet(rotateNames, cfg)
	if err == nil {
		t.Fatal("expected error for provider_credential, got nil")
	}
	if !strings.Contains(err.Error(), "must be rotated via the upstream provider") {
		t.Errorf("error message should mention upstream provider rotation; got: %v", err)
	}
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

	_, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
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

	_, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
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

	if _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate); err != nil {
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

	if _, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate); err != nil {
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
