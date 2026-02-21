package secrets

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func skipIfNoVault(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("vault"); err != nil {
		t.Skip("vault binary not found on PATH; skipping dev vault tests (install from https://developer.hashicorp.com/vault/install)")
	}
}

func TestDevVaultProvider_StartStop(t *testing.T) {
	skipIfNoVault(t)

	p, err := NewDevVaultProvider(DevVaultConfig{})
	if err != nil {
		t.Fatalf("NewDevVaultProvider: %v", err)
	}
	defer p.Close()

	if p.Addr() == "" {
		t.Error("expected non-empty address")
	}
	if p.Name() != "vault" {
		t.Errorf("expected 'vault', got %q", p.Name())
	}
}

func TestDevVaultProvider_SetGetRoundTrip(t *testing.T) {
	skipIfNoVault(t)

	p, err := NewDevVaultProvider(DevVaultConfig{})
	if err != nil {
		t.Fatalf("NewDevVaultProvider: %v", err)
	}
	defer p.Close()

	ctx := context.Background()

	// Set a secret
	if err := p.Set(ctx, "test/db-password", "super-secret-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get the secret back
	val, err := p.Get(ctx, "test/db-password#value")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "super-secret-123" {
		t.Errorf("expected 'super-secret-123', got %q", val)
	}
}

func TestDevVaultProvider_DeleteRoundTrip(t *testing.T) {
	skipIfNoVault(t)

	p, err := NewDevVaultProvider(DevVaultConfig{})
	if err != nil {
		t.Fatalf("NewDevVaultProvider: %v", err)
	}
	defer p.Close()

	ctx := context.Background()

	// Set a secret
	if err := p.Set(ctx, "test/to-delete", "delete-me"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify it exists
	_, err = p.Get(ctx, "test/to-delete#value")
	if err != nil {
		t.Fatalf("Get before delete: %v", err)
	}

	// Delete it
	if err := p.Delete(ctx, "test/to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it's gone
	_, err = p.Get(ctx, "test/to-delete")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDevVaultProvider_ListKeys(t *testing.T) {
	skipIfNoVault(t)

	p, err := NewDevVaultProvider(DevVaultConfig{})
	if err != nil {
		t.Fatalf("NewDevVaultProvider: %v", err)
	}
	defer p.Close()

	ctx := context.Background()

	// Set multiple secrets
	secrets := map[string]string{
		"app/key1": "val1",
		"app/key2": "val2",
		"app/key3": "val3",
	}
	for k, v := range secrets {
		if err := p.Set(ctx, k, v); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}

	// List all keys
	keys, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(keys) < 3 {
		t.Errorf("expected at least 3 keys, got %d: %v", len(keys), keys)
	}

	// Check that our keys are present
	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for expected := range secrets {
		if !keySet[expected] {
			t.Errorf("expected key %q in list, got %v", expected, keys)
		}
	}
}

func TestDevVaultProvider_FieldExtraction(t *testing.T) {
	skipIfNoVault(t)

	p, err := NewDevVaultProvider(DevVaultConfig{})
	if err != nil {
		t.Fatalf("NewDevVaultProvider: %v", err)
	}
	defer p.Close()

	ctx := context.Background()

	// Set a secret
	if err := p.Set(ctx, "myapp/config", "password123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get the value field specifically
	val, err := p.Get(ctx, "myapp/config#value")
	if err != nil {
		t.Fatalf("Get #value: %v", err)
	}
	if val != "password123" {
		t.Errorf("expected 'password123', got %q", val)
	}

	// Get full data (no field) — should be JSON
	fullData, err := p.Get(ctx, "myapp/config")
	if err != nil {
		t.Fatalf("Get full: %v", err)
	}
	if !strings.Contains(fullData, "password123") {
		t.Errorf("expected JSON containing 'password123', got %q", fullData)
	}

	// Get missing field
	_, err = p.Get(ctx, "myapp/config#nonexistent")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestDevVaultProvider_CustomToken(t *testing.T) {
	skipIfNoVault(t)

	p, err := NewDevVaultProvider(DevVaultConfig{
		RootToken: "my-custom-token",
	})
	if err != nil {
		t.Fatalf("NewDevVaultProvider: %v", err)
	}
	defer p.Close()

	ctx := context.Background()

	// Basic operation should work with custom token
	if err := p.Set(ctx, "test/custom-token", "works"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := p.Get(ctx, "test/custom-token#value")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "works" {
		t.Errorf("expected 'works', got %q", val)
	}
}
