package secrets

import (
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
