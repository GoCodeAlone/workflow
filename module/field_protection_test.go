package module

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/pkg/fieldcrypt"
)

func TestFieldProtectionModuleConfigParsing(t *testing.T) {
	cfg := map[string]any{
		"master_key":       "test-master-key-that-is-long-en",
		"tenant_isolation": true,
		"scan_depth":       5,
		"scan_arrays":      false,
		"protected_fields": []any{
			map[string]any{
				"name":           "ssn",
				"classification": "pii",
				"encryption":     true,
				"log_behavior":   "redact",
			},
			map[string]any{
				"name":           "email",
				"classification": "pii",
				"encryption":     true,
				"log_behavior":   "mask",
			},
		},
	}

	mod, err := NewFieldProtectionModule("test-fp", cfg)
	if err != nil {
		t.Fatalf("NewFieldProtectionModule: %v", err)
	}
	if mod.Name() != "test-fp" {
		t.Errorf("name = %q", mod.Name())
	}

	mgr := mod.Manager()
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	if !mgr.TenantIsolation {
		t.Error("tenant_isolation should be true")
	}
	if mgr.ScanDepth != 5 {
		t.Errorf("scan_depth = %d", mgr.ScanDepth)
	}
	if mgr.ScanArrays {
		t.Error("scan_arrays should be false")
	}
	if !mgr.Registry.IsProtected("ssn") {
		t.Error("ssn should be protected")
	}
	if !mgr.Registry.IsProtected("email") {
		t.Error("email should be protected")
	}
	if mgr.Registry.IsProtected("name") {
		t.Error("name should not be protected")
	}
}

func TestFieldProtectionEncryptDecryptRoundTrip(t *testing.T) {
	cfg := map[string]any{
		"master_key": "test-key-for-encrypt-decrypt-rt!",
		"protected_fields": []any{
			map[string]any{
				"name":       "ssn",
				"encryption": true,
			},
			map[string]any{
				"name":       "email",
				"encryption": true,
			},
		},
	}

	mod, err := NewFieldProtectionModule("fp", cfg)
	if err != nil {
		t.Fatal(err)
	}
	mgr := mod.Manager()
	ctx := context.Background()

	data := map[string]any{
		"ssn":   "123-45-6789",
		"email": "user@example.com",
		"name":  "John Doe",
	}

	if err := mgr.EncryptMap(ctx, "tenant1", data); err != nil {
		t.Fatalf("EncryptMap: %v", err)
	}

	if !fieldcrypt.IsEncrypted(data["ssn"].(string)) {
		t.Error("ssn should be encrypted")
	}
	if !fieldcrypt.IsEncrypted(data["email"].(string)) {
		t.Error("email should be encrypted")
	}
	if data["name"] != "John Doe" {
		t.Error("name should not be modified")
	}

	if err := mgr.DecryptMap(ctx, "tenant1", data); err != nil {
		t.Fatalf("DecryptMap: %v", err)
	}

	if data["ssn"] != "123-45-6789" {
		t.Errorf("ssn = %q", data["ssn"])
	}
	if data["email"] != "user@example.com" {
		t.Errorf("email = %q", data["email"])
	}
}

func TestFieldProtectionMaskMap(t *testing.T) {
	cfg := map[string]any{
		"master_key": "mask-test-key-32-chars-exactly!!",
		"protected_fields": []any{
			map[string]any{
				"name":         "ssn",
				"log_behavior": "redact",
			},
			map[string]any{
				"name":         "email",
				"log_behavior": "mask",
			},
			map[string]any{
				"name":         "phone",
				"log_behavior": "hash",
			},
		},
	}

	mod, err := NewFieldProtectionModule("fp", cfg)
	if err != nil {
		t.Fatal(err)
	}
	mgr := mod.Manager()

	data := map[string]any{
		"ssn":   "123-45-6789",
		"email": "user@example.com",
		"phone": "555-123-4567",
		"name":  "John Doe",
	}

	masked := mgr.MaskMap(data)

	if masked["ssn"] != "[REDACTED]" {
		t.Errorf("ssn mask = %q", masked["ssn"])
	}
	if masked["email"] == "user@example.com" {
		t.Error("email should be masked")
	}
	if masked["phone"] == "555-123-4567" {
		t.Error("phone should be hashed")
	}
	if masked["name"] != "John Doe" {
		t.Error("name should be unchanged")
	}

	// Original data should be unmodified.
	if data["ssn"] != "123-45-6789" {
		t.Error("original ssn was modified")
	}
}

func TestFieldProtectionProvidesServices(t *testing.T) {
	cfg := map[string]any{
		"master_key":       "svc-test-key-32-chars-exactly!!",
		"protected_fields": []any{},
	}

	mod, err := NewFieldProtectionModule("my-fp", cfg)
	if err != nil {
		t.Fatal(err)
	}

	svcs := mod.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "my-fp" {
		t.Errorf("service name = %q", svcs[0].Name)
	}
	if _, ok := svcs[0].Instance.(*ProtectedFieldManager); !ok {
		t.Error("service instance should be *ProtectedFieldManager")
	}
}
