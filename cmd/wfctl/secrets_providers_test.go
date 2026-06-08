package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/secrets"
)

// plainSecretsProvider is a minimal secrets.Provider (no metadata).
type plainSecretsProvider struct {
	stored map[string]string
}

func (f *plainSecretsProvider) Name() string { return "plain" }
func (f *plainSecretsProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := f.stored[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (f *plainSecretsProvider) Set(_ context.Context, key, value string) error {
	f.stored[key] = value
	return nil
}
func (f *plainSecretsProvider) Delete(_ context.Context, key string) error {
	delete(f.stored, key)
	return nil
}
func (f *plainSecretsProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(f.stored))
	for k := range f.stored {
		keys = append(keys, k)
	}
	return keys, nil
}

// metadataSecretsProvider extends plainSecretsProvider with MetadataProvider support.
type metadataSecretsProvider struct {
	*plainSecretsProvider
	metas []secrets.SecretMeta
}

func (f *metadataSecretsProvider) StatAll(_ context.Context) ([]secrets.SecretMeta, error) {
	return f.metas, nil
}

type targetSecretsProvider struct {
	*plainSecretsProvider
	target secrets.ProviderTarget
}

func (f *targetSecretsProvider) SecretTarget() secrets.ProviderTarget {
	return f.target
}

// ---------------------------------------------------------------------------
// Adapter tests
// ---------------------------------------------------------------------------

func TestSecretsProviderAdapter_Check_PlainProvider_Set(t *testing.T) {
	fp := &plainSecretsProvider{stored: map[string]string{"MY_KEY": "some-value"}}
	adapter := secretsProviderAdapter{fp}

	state, err := adapter.Check(context.Background(), "MY_KEY")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state != SecretSet {
		t.Errorf("Check(set key) = %v, want SecretSet", state)
	}
}

func TestSecretsProviderAdapter_Check_PlainProvider_NotSet(t *testing.T) {
	fp := &plainSecretsProvider{stored: map[string]string{}}
	adapter := secretsProviderAdapter{fp}

	state, err := adapter.Check(context.Background(), "MISSING_KEY")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state != SecretNotSet {
		t.Errorf("Check(missing key) = %v, want SecretNotSet", state)
	}
}

func TestSecretsProviderAdapter_List_PlainProvider_PresenceOnly(t *testing.T) {
	fp := &plainSecretsProvider{stored: map[string]string{"KEY_A": "v1", "KEY_B": "v2"}}
	adapter := secretsProviderAdapter{fp}

	statuses, err := adapter.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	// Plain provider: LastRotated should be zero.
	for _, s := range statuses {
		if !s.LastRotated.IsZero() {
			t.Errorf("key %q: expected zero LastRotated for plain provider, got %v", s.Name, s.LastRotated)
		}
	}
}

func TestSecretsProviderAdapter_List_MetadataProvider_UpdatedAt(t *testing.T) {
	ts := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	fp := &plainSecretsProvider{stored: map[string]string{"KEY_A": "v1"}}
	metas := []secrets.SecretMeta{
		{Name: "KEY_A", Exists: true, UpdatedAt: ts},
	}
	mp := &metadataSecretsProvider{
		plainSecretsProvider: fp,
		metas:                metas,
	}
	adapter := secretsProviderAdapter{mp}

	statuses, err := adapter.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].LastRotated != ts {
		t.Errorf("LastRotated = %v, want %v", statuses[0].LastRotated, ts)
	}
}

func TestSecretsProviderAdapter_SecretTargetUsesProviderContract(t *testing.T) {
	fp := &targetSecretsProvider{
		plainSecretsProvider: &plainSecretsProvider{stored: map[string]string{}},
		target: secrets.ProviderTarget{
			Provider: "custom",
			Scope:    "namespace",
			Subject:  "example",
			Label:    "custom namespace example",
		},
	}
	adapter := secretsProviderAdapter{fp}

	target := adapter.SecretTarget()
	if target.Label != "custom namespace example" || target.Scope != "namespace" {
		t.Fatalf("target = %+v", target)
	}
}

func TestNewSecretsProvider_NotUnknownProviderError(t *testing.T) {
	// Clear GITHUB_TOKEN so the github branch is actually reached even when the
	// ambient environment has a token set — otherwise the test would short-circuit.
	t.Setenv("GITHUB_TOKEN", "")

	// newSecretsProvider("github") should NOT return the old "unknown secrets provider" error.
	// It may fail with another error (e.g. missing token), but not the old sentinel.
	_, err := newSecretsProvider("github")
	if err == nil {
		t.Fatal("expected an error (missing GITHUB_TOKEN), got nil")
	}
	if strings.Contains(err.Error(), "unknown secrets provider") {
		t.Errorf("newSecretsProvider(\"github\") returned the old unknown-provider error: %v", err)
	}
}

func TestNewSecretsProviderFromConfig_PreservesGitHubConfig(t *testing.T) {
	t.Setenv("GH_MANAGEMENT_TOKEN", "test-token")

	p, err := newSecretsProviderFromConfig(&SecretsConfig{
		Provider: "github",
		Config: map[string]any{
			"repo":      "GoCodeAlone/buymywishlist",
			"token_env": "GH_MANAGEMENT_TOKEN",
		},
	})
	if err != nil {
		t.Fatalf("newSecretsProviderFromConfig: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewSecretsProvider_Env_Success(t *testing.T) {
	p, err := newSecretsProvider("env")
	if err != nil {
		t.Fatalf("newSecretsProvider(\"env\"): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}
