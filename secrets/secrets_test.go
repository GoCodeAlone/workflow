package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- EnvProvider Tests ---

func TestEnvProvider_Get_Found(t *testing.T) {
	t.Setenv("TEST_DB_PASSWORD", "secret123")

	p := NewEnvProvider("")
	val, err := p.Get(context.Background(), "test_db_password")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "secret123" {
		t.Errorf("expected 'secret123', got %q", val)
	}
}

func TestEnvProvider_Get_WithPrefix(t *testing.T) {
	t.Setenv("APP_DB_HOST", "localhost")

	p := NewEnvProvider("APP_")
	val, err := p.Get(context.Background(), "db_host")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "localhost" {
		t.Errorf("expected 'localhost', got %q", val)
	}
}

func TestEnvProvider_Get_NotFound(t *testing.T) {
	p := NewEnvProvider("")
	_, err := p.Get(context.Background(), "nonexistent_secret_key_xyz")
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestEnvProvider_Get_InvalidKey(t *testing.T) {
	p := NewEnvProvider("")
	_, err := p.Get(context.Background(), "")
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestEnvProvider_Set_Delete(t *testing.T) {
	p := NewEnvProvider("")
	ctx := context.Background()

	if err := p.Set(ctx, "test_set_key", "myvalue"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := p.Get(ctx, "test_set_key")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if val != "myvalue" {
		t.Errorf("expected 'myvalue', got %q", val)
	}

	if err := p.Delete(ctx, "test_set_key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = p.Get(ctx, "test_set_key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestEnvProvider_List_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_KEY1", "val1")
	t.Setenv("MYAPP_KEY2", "val2")

	p := NewEnvProvider("MYAPP_")
	keys, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) < 2 {
		t.Errorf("expected at least 2 keys, got %d", len(keys))
	}
}

func TestEnvProvider_List_NoPrefix(t *testing.T) {
	p := NewEnvProvider("")
	_, err := p.List(context.Background())
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for List without prefix, got %v", err)
	}
}

func TestEnvProvider_DotConversion(t *testing.T) {
	t.Setenv("DATABASE_PASSWORD", "pass")

	p := NewEnvProvider("")
	val, err := p.Get(context.Background(), "database.password")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "pass" {
		t.Errorf("expected 'pass', got %q", val)
	}
}

func TestEnvProvider_Name(t *testing.T) {
	p := NewEnvProvider("")
	if p.Name() != "env" {
		t.Errorf("expected 'env', got %q", p.Name())
	}
}

// --- FileProvider Tests ---

func TestFileProvider_GetSetDeleteList(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	ctx := context.Background()

	// Set
	if err := p.Set(ctx, "api_key", "secret-key-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get
	val, err := p.Get(ctx, "api_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "secret-key-123" {
		t.Errorf("expected 'secret-key-123', got %q", val)
	}

	// List
	keys, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 || keys[0] != "api_key" {
		t.Errorf("unexpected keys: %v", keys)
	}

	// Delete
	if err := p.Delete(ctx, "api_key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = p.Get(ctx, "api_key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestFileProvider_Get_NotFound(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)

	_, err := p.Get(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileProvider_Get_TrimsNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my_secret")
	if err := os.WriteFile(path, []byte("secret-value\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := NewFileProvider(dir)
	val, err := p.Get(context.Background(), "my_secret")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "secret-value" {
		t.Errorf("expected 'secret-value', got %q", val)
	}
}

func TestFileProvider_InvalidKey(t *testing.T) {
	p := NewFileProvider(t.TempDir())
	ctx := context.Background()

	if _, err := p.Get(ctx, ""); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Get empty key: expected ErrInvalidKey, got %v", err)
	}
	if err := p.Set(ctx, "", "val"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Set empty key: expected ErrInvalidKey, got %v", err)
	}
	if err := p.Delete(ctx, ""); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete empty key: expected ErrInvalidKey, got %v", err)
	}
}

func TestFileProvider_Delete_NotFound(t *testing.T) {
	p := NewFileProvider(t.TempDir())
	err := p.Delete(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileProvider_Name(t *testing.T) {
	p := NewFileProvider("/tmp")
	if p.Name() != "file" {
		t.Errorf("expected 'file', got %q", p.Name())
	}
}

func TestFileProvider_ListEmpty(t *testing.T) {
	p := NewFileProvider(t.TempDir())
	keys, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

// --- VaultProvider Tests ---

func TestNewVaultProvider_Valid(t *testing.T) {
	p, err := NewVaultProvider(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "s.abc123",
	})
	if err != nil {
		t.Fatalf("NewVaultProvider: %v", err)
	}
	if p.Name() != "vault" {
		t.Errorf("expected 'vault', got %q", p.Name())
	}
	if p.Config().MountPath != "secret" {
		t.Errorf("expected default mount path 'secret', got %q", p.Config().MountPath)
	}
}

func TestNewVaultProvider_MissingAddress(t *testing.T) {
	_, err := NewVaultProvider(VaultConfig{Token: "s.abc"})
	if err == nil {
		t.Fatal("expected error for missing address")
	}
	if !errors.Is(err, ErrProviderInit) {
		t.Errorf("expected ErrProviderInit, got %v", err)
	}
}

func TestNewVaultProvider_MissingToken(t *testing.T) {
	_, err := NewVaultProvider(VaultConfig{Address: "https://vault.example.com"})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestVaultProvider_InvalidKey(t *testing.T) {
	p, _ := NewVaultProvider(VaultConfig{
		Address: "https://vault.example.com",
		Token:   "s.abc",
	})
	ctx := context.Background()

	if _, err := p.Get(ctx, ""); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Get empty key: expected ErrInvalidKey, got %v", err)
	}
	if err := p.Set(ctx, "", "val"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Set empty key: expected ErrInvalidKey, got %v", err)
	}
	if err := p.Delete(ctx, ""); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete empty key: expected ErrInvalidKey, got %v", err)
	}
}

// --- Resolver Tests ---

func TestResolver_Resolve_SecretRef(t *testing.T) {
	t.Setenv("DB_PASSWORD", "super-secret")

	r := NewResolver(NewEnvProvider(""))
	val, err := r.Resolve(context.Background(), "secret://db_password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if val != "super-secret" {
		t.Errorf("expected 'super-secret', got %q", val)
	}
}

func TestResolver_Resolve_PlainValue(t *testing.T) {
	r := NewResolver(NewEnvProvider(""))
	val, err := r.Resolve(context.Background(), "not-a-secret")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if val != "not-a-secret" {
		t.Errorf("expected 'not-a-secret', got %q", val)
	}
}

func TestResolver_Resolve_MissingSecret(t *testing.T) {
	r := NewResolver(NewEnvProvider(""))
	_, err := r.Resolve(context.Background(), "secret://missing_key_xyz")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestResolver_ResolveMap(t *testing.T) {
	t.Setenv("API_KEY", "key123")

	r := NewResolver(NewEnvProvider(""))
	m := map[string]any{
		"host":    "localhost",
		"api_key": "secret://api_key",
		"nested": map[string]any{
			"value": "plain",
		},
		"port": 8080,
	}

	result, err := r.ResolveMap(context.Background(), m)
	if err != nil {
		t.Fatalf("ResolveMap: %v", err)
	}

	if result["host"] != "localhost" {
		t.Errorf("expected 'localhost', got %v", result["host"])
	}
	if result["api_key"] != "key123" {
		t.Errorf("expected 'key123', got %v", result["api_key"])
	}
	if result["port"] != 8080 {
		t.Errorf("expected 8080, got %v", result["port"])
	}
}

func TestResolver_ResolveMap_NestedSecret(t *testing.T) {
	t.Setenv("NESTED_SECRET", "nested-value")

	r := NewResolver(NewEnvProvider(""))
	m := map[string]any{
		"database": map[string]any{
			"password": "secret://nested_secret",
		},
	}

	result, err := r.ResolveMap(context.Background(), m)
	if err != nil {
		t.Fatalf("ResolveMap: %v", err)
	}

	nested := result["database"].(map[string]any)
	if nested["password"] != "nested-value" {
		t.Errorf("expected 'nested-value', got %v", nested["password"])
	}
}

func TestResolver_ResolveMap_Error(t *testing.T) {
	r := NewResolver(NewEnvProvider(""))
	m := map[string]any{
		"key": "secret://missing_xyz_123",
	}

	_, err := r.ResolveMap(context.Background(), m)
	if err == nil {
		t.Fatal("expected error for missing secret in map")
	}
}

func TestResolver_Provider(t *testing.T) {
	p := NewEnvProvider("TEST_")
	r := NewResolver(p)
	if r.Provider().Name() != "env" {
		t.Errorf("expected 'env', got %q", r.Provider().Name())
	}
}
