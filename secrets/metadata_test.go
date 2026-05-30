package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestMetadataProvider_InterfaceShape verifies that:
// - EnvProvider satisfies Provider
// - SecretMeta zero-value has zero UpdatedAt
func TestMetadataProvider_InterfaceShape(t *testing.T) {
	// Compile-time check: EnvProvider must satisfy Provider.
	var _ Provider = (*EnvProvider)(nil)

	m := SecretMeta{Name: "X", Exists: true}
	if !m.UpdatedAt.IsZero() {
		t.Fatal("expected zero UpdatedAt for new SecretMeta")
	}
}

// ---------------------------------------------------------------------------
// FileProvider StatAll + CheckAccess
// ---------------------------------------------------------------------------

func TestFileProvider_StatAll(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	ctx := context.Background()

	// Write two key files.
	if err := os.WriteFile(filepath.Join(dir, "KEY_A"), []byte("valA"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "KEY_B"), []byte("valB"), 0600); err != nil {
		t.Fatal(err)
	}

	metas, err := p.StatAll(ctx)
	if err != nil {
		t.Fatalf("StatAll: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("expected 2 metas, got %d", len(metas))
	}
	// All should exist; mtime should be non-zero.
	for _, m := range metas {
		if !m.Exists {
			t.Errorf("key %q: expected Exists=true", m.Name)
		}
		if m.UpdatedAt.IsZero() {
			t.Errorf("key %q: expected non-zero UpdatedAt (file mtime)", m.Name)
		}
	}
}

func TestFileProvider_CheckAccess_Success(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	if err := p.CheckAccess(context.Background()); err != nil {
		t.Errorf("expected nil error for writable dir, got %v", err)
	}
}

func TestFileProvider_CheckAccess_MissingDir(t *testing.T) {
	p := NewFileProvider("/nonexistent/path/xyz123")
	err := p.CheckAccess(context.Background())
	if err == nil {
		t.Error("expected error for missing directory")
	}
}

// ---------------------------------------------------------------------------
// EnvProvider StatAll + CheckAccess
// ---------------------------------------------------------------------------

func TestEnvProvider_StatAll(t *testing.T) {
	prefix := "WFTEST_META_"
	t.Setenv(prefix+"KEY1", "v1")
	t.Setenv(prefix+"KEY2", "v2")

	p := NewEnvProvider(prefix)
	ctx := context.Background()

	metas, err := p.StatAll(ctx)
	if err != nil {
		t.Fatalf("StatAll: %v", err)
	}
	if len(metas) < 2 {
		t.Fatalf("expected at least 2 metas, got %d", len(metas))
	}
	for _, m := range metas {
		if m.UpdatedAt.IsZero() {
			// Env provider can't know mtime — UpdatedAt should always be zero.
		}
		if !m.Exists {
			t.Errorf("key %q: expected Exists=true", m.Name)
		}
	}
}

func TestEnvProvider_StatAll_NoPrefix(t *testing.T) {
	// Without prefix, StatAll should return ErrUnsupported.
	p := NewEnvProvider("")
	_, err := p.StatAll(context.Background())
	if err == nil {
		t.Error("expected error from StatAll with no prefix")
	}
}

func TestEnvProvider_CheckAccess(t *testing.T) {
	p := NewEnvProvider("")
	if err := p.CheckAccess(context.Background()); err != nil {
		t.Errorf("CheckAccess on EnvProvider: %v", err)
	}
}
