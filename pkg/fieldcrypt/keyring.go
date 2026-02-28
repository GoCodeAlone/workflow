package fieldcrypt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/hkdf"
)

// KeyRing manages versioned, tenant-isolated encryption keys.
type KeyRing interface {
	CurrentKey(ctx context.Context, tenantID string) (key []byte, version int, err error)
	KeyByVersion(ctx context.Context, tenantID string, version int) ([]byte, error)
	Rotate(ctx context.Context, tenantID string) (key []byte, version int, err error)
}

// LocalKeyRing stores keys in memory, keyed by tenant.
// Keys are derived from a master key using HKDF.
type LocalKeyRing struct {
	masterKey      []byte
	mu             sync.RWMutex
	tenantVersions map[string]int    // tenantID -> current version number
	tenantKeys     map[string][]byte // "tenantID:version" -> derived key
}

// NewLocalKeyRing creates a new LocalKeyRing from a master key.
func NewLocalKeyRing(masterKey []byte) *LocalKeyRing {
	return &LocalKeyRing{
		masterKey:      masterKey,
		tenantVersions: make(map[string]int),
		tenantKeys:     make(map[string][]byte),
	}
}

// CurrentKey returns the current key version for a tenant.
// If no key exists yet, generates version 1.
func (k *LocalKeyRing) CurrentKey(_ context.Context, tenantID string) ([]byte, int, error) {
	k.mu.RLock()
	ver, ok := k.tenantVersions[tenantID]
	if ok {
		cacheKey := fmt.Sprintf("%s:%d", tenantID, ver)
		key := k.tenantKeys[cacheKey]
		k.mu.RUnlock()
		return key, ver, nil
	}
	k.mu.RUnlock()

	// No key yet; create version 1.
	k.mu.Lock()
	defer k.mu.Unlock()

	// Double-check after acquiring write lock.
	if ver, ok := k.tenantVersions[tenantID]; ok {
		cacheKey := fmt.Sprintf("%s:%d", tenantID, ver)
		return k.tenantKeys[cacheKey], ver, nil
	}

	key, err := k.deriveKey(tenantID, 1)
	if err != nil {
		return nil, 0, err
	}
	k.tenantVersions[tenantID] = 1
	k.tenantKeys[fmt.Sprintf("%s:%d", tenantID, 1)] = key
	return key, 1, nil
}

// KeyByVersion returns the key for a specific tenant+version.
func (k *LocalKeyRing) KeyByVersion(_ context.Context, tenantID string, version int) ([]byte, error) {
	cacheKey := fmt.Sprintf("%s:%d", tenantID, version)

	k.mu.RLock()
	if key, ok := k.tenantKeys[cacheKey]; ok {
		k.mu.RUnlock()
		return key, nil
	}
	k.mu.RUnlock()

	k.mu.Lock()
	defer k.mu.Unlock()

	// Double-check.
	if key, ok := k.tenantKeys[cacheKey]; ok {
		return key, nil
	}

	key, err := k.deriveKey(tenantID, version)
	if err != nil {
		return nil, err
	}
	k.tenantKeys[cacheKey] = key
	return key, nil
}

// Rotate increments the version and derives a new key for the tenant.
func (k *LocalKeyRing) Rotate(_ context.Context, tenantID string) ([]byte, int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	ver := k.tenantVersions[tenantID] + 1
	key, err := k.deriveKey(tenantID, ver)
	if err != nil {
		return nil, 0, err
	}
	k.tenantVersions[tenantID] = ver
	k.tenantKeys[fmt.Sprintf("%s:%d", tenantID, ver)] = key
	return key, ver, nil
}

// deriveKey uses HKDF-SHA256 to derive a 32-byte key.
// Info = "fieldcrypt:" + tenantID + ":v" + version.
func (k *LocalKeyRing) deriveKey(tenantID string, version int) ([]byte, error) {
	info := fmt.Sprintf("fieldcrypt:%s:v%d", tenantID, version)
	hkdfReader := hkdf.New(sha256.New, k.masterKey, nil, []byte(info))
	derived := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, derived); err != nil {
		return nil, fmt.Errorf("fieldcrypt: key derivation failed: %w", err)
	}
	return derived, nil
}
