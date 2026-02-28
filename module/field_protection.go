package module

import (
	"context"
	"fmt"
	"os"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/pkg/fieldcrypt"
)

// ProtectedFieldManager bundles the registry and key ring for field protection.
type ProtectedFieldManager struct {
	Registry        *fieldcrypt.Registry
	KeyRing         fieldcrypt.KeyRing
	TenantIsolation bool
	ScanDepth       int
	ScanArrays      bool
	defaultTenantID string
	// rawMasterKey is retained for legacy enc:: decryption (version 0).
	rawMasterKey []byte
}

// resolveTenant returns the effective tenant ID, applying isolation policy.
func (m *ProtectedFieldManager) resolveTenant(tenantID string) string {
	if !m.TenantIsolation {
		return m.defaultTenantID
	}
	if tenantID == "" {
		return m.defaultTenantID
	}
	return tenantID
}

// EncryptMap encrypts protected fields in the data map in-place.
func (m *ProtectedFieldManager) EncryptMap(ctx context.Context, tenantID string, data map[string]any) error {
	tid := m.resolveTenant(tenantID)
	return fieldcrypt.ScanAndEncrypt(data, m.Registry, func() ([]byte, int, error) {
		return m.KeyRing.CurrentKey(ctx, tid)
	}, m.ScanDepth)
}

// DecryptMap decrypts protected fields in the data map in-place.
// Version 0 is the legacy enc:: format and uses the raw master key.
func (m *ProtectedFieldManager) DecryptMap(ctx context.Context, tenantID string, data map[string]any) error {
	tid := m.resolveTenant(tenantID)
	return fieldcrypt.ScanAndDecrypt(data, m.Registry, func(version int) ([]byte, error) {
		if version == 0 {
			// Legacy enc:: values were encrypted with sha256(masterKey).
			// encrypt.go's decryptLegacy calls keyFn(0) expecting the raw key
			// bytes, then SHA256-hashes them before use.
			return m.rawMasterKey, nil
		}
		return m.KeyRing.KeyByVersion(ctx, tid, version)
	}, m.ScanDepth)
}

// MaskMap returns a deep copy of data with protected fields masked for logging.
func (m *ProtectedFieldManager) MaskMap(data map[string]any) map[string]any {
	return fieldcrypt.ScanAndMask(data, m.Registry, m.ScanDepth)
}

// FieldProtectionModule implements modular.Module for security.field-protection.
type FieldProtectionModule struct {
	name    string
	manager *ProtectedFieldManager
}

// NewFieldProtectionModule parses config and creates a FieldProtectionModule.
func NewFieldProtectionModule(name string, cfg map[string]any) (*FieldProtectionModule, error) {
	// Parse protected fields.
	var fields []fieldcrypt.ProtectedField
	if raw, ok := cfg["protected_fields"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				pf := fieldcrypt.ProtectedField{}
				if v, ok := m["name"].(string); ok {
					pf.Name = v
				}
				if v, ok := m["classification"].(string); ok {
					pf.Classification = fieldcrypt.FieldClassification(v)
				}
				if v, ok := m["encryption"].(bool); ok {
					pf.Encryption = v
				}
				if v, ok := m["log_behavior"].(string); ok {
					pf.LogBehavior = fieldcrypt.LogBehavior(v)
				}
				if v, ok := m["mask_pattern"].(string); ok {
					pf.MaskPattern = v
				}
				fields = append(fields, pf)
			}
		}
	}

	// Resolve master key.
	masterKeyStr, _ := cfg["master_key"].(string)
	if masterKeyStr == "" {
		masterKeyStr = os.Getenv("FIELD_ENCRYPTION_KEY")
	}

	if masterKeyStr == "" {
		return nil, fmt.Errorf("field-protection: master_key config or FIELD_ENCRYPTION_KEY env var is required")
	}
	masterKey := []byte(masterKeyStr)

	scanDepth := 10
	if v, ok := cfg["scan_depth"].(int); ok && v > 0 {
		scanDepth = v
	}

	scanArrays := true
	if v, ok := cfg["scan_arrays"].(bool); ok {
		scanArrays = v
	}

	tenantIsolation := false
	if v, ok := cfg["tenant_isolation"].(bool); ok {
		tenantIsolation = v
	}

	defaultTenantID := "default"

	mgr := &ProtectedFieldManager{
		Registry:        fieldcrypt.NewRegistry(fields),
		KeyRing:         fieldcrypt.NewLocalKeyRing(masterKey),
		TenantIsolation: tenantIsolation,
		ScanDepth:       scanDepth,
		ScanArrays:      scanArrays,
		defaultTenantID: defaultTenantID,
		rawMasterKey:    masterKey,
	}

	return &FieldProtectionModule{name: name, manager: mgr}, nil
}

// Name returns the module name.
func (m *FieldProtectionModule) Name() string { return m.name }

// Init initializes the module.
func (m *FieldProtectionModule) Init(_ modular.Application) error { return nil }

// ProvidesServices returns the services provided by this module.
func (m *FieldProtectionModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "Protected field manager for encryption/masking", Instance: m.manager},
	}
}

// RequiresServices returns service dependencies (none).
func (m *FieldProtectionModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Manager returns the ProtectedFieldManager.
func (m *FieldProtectionModule) Manager() *ProtectedFieldManager {
	return m.manager
}
