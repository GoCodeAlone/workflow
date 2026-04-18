package secrets_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/zalando/go-keyring"
)

// Compile-time assertion: KeychainProvider must satisfy secrets.Provider.
var _ secrets.Provider = (*secrets.KeychainProvider)(nil)

// mustNewKeychainProvider is a test helper that creates a provider or fails the test.
func mustNewKeychainProvider(t *testing.T, service string) *secrets.KeychainProvider {
	t.Helper()
	p, err := secrets.NewKeychainProvider(service)
	if err != nil {
		t.Fatalf("NewKeychainProvider(%q): %v", service, err)
	}
	return p
}

func TestKeychainProvider_SetAndGet(t *testing.T) {
	keyring.MockInit()
	p := mustNewKeychainProvider(t, "test-service")

	ctx := context.Background()
	if err := p.Set(ctx, "api_key", "secret-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := p.Get(ctx, "api_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "secret-123" {
		t.Errorf("got %q, want secret-123", got)
	}
}

func TestKeychainProvider_GetMissing(t *testing.T) {
	keyring.MockInit()
	p := mustNewKeychainProvider(t, "test-service")
	_, err := p.Get(context.Background(), "absent")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestKeychainProvider_Delete(t *testing.T) {
	keyring.MockInit()
	p := mustNewKeychainProvider(t, "test-service")
	ctx := context.Background()
	if err := p.Set(ctx, "x", "1"); err != nil {
		t.Fatalf("setup Set: %v", err)
	}
	if err := p.Delete(ctx, "x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := p.Get(ctx, "x"); err == nil {
		t.Fatal("expected error after Delete")
	}
}

func TestKeychainProvider_List(t *testing.T) {
	keyring.MockInit()
	p := mustNewKeychainProvider(t, "test-service")
	ctx := context.Background()
	if err := p.Set(ctx, "a", "1"); err != nil {
		t.Fatalf("setup Set a: %v", err)
	}
	if err := p.Set(ctx, "b", "2"); err != nil {
		t.Fatalf("setup Set b: %v", err)
	}
	keys, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(keys)
	want := []string{"a", "b"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("List() = %v, want %v", keys, want)
	}
}

func TestKeychainProvider_EmptyService(t *testing.T) {
	_, err := secrets.NewKeychainProvider("")
	if err == nil {
		t.Fatal("expected error for empty service name")
	}
}

func TestKeychainProvider_EmptyKey(t *testing.T) {
	keyring.MockInit()
	p := mustNewKeychainProvider(t, "test-service")
	ctx := context.Background()

	if _, err := p.Get(ctx, ""); err != secrets.ErrInvalidKey {
		t.Errorf("Get empty key: expected ErrInvalidKey, got %v", err)
	}
	if err := p.Set(ctx, "", "val"); err != secrets.ErrInvalidKey {
		t.Errorf("Set empty key: expected ErrInvalidKey, got %v", err)
	}
	if err := p.Delete(ctx, ""); err != secrets.ErrInvalidKey {
		t.Errorf("Delete empty key: expected ErrInvalidKey, got %v", err)
	}
}

func TestKeychainProvider_DeleteIdempotent_CleansTrackedKeys(t *testing.T) {
	keyring.MockInit()
	p := mustNewKeychainProvider(t, "test-service")
	ctx := context.Background()

	// Set then delete a key.
	if err := p.Set(ctx, "ephemeral", "val"); err != nil {
		t.Fatalf("setup Set: %v", err)
	}
	if err := p.Delete(ctx, "ephemeral"); err != nil {
		t.Fatalf("setup Delete: %v", err)
	}

	// Delete again (idempotent, key already gone from keyring).
	if err := p.Delete(ctx, "ephemeral"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}

	// List should not contain the deleted key.
	keys, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, k := range keys {
		if k == "ephemeral" {
			t.Error("List() still contains deleted key 'ephemeral'")
		}
	}
}
