package secrets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

// KeychainProvider implements Provider using the OS credential store
// (macOS Keychain, Linux Secret Service, Windows Credential Manager).
//
// All keys are namespaced under a single "service" string so multiple
// workflow services on the same machine don't collide.
//
// On Linux, requires a running Secret Service implementation (libsecret,
// gnome-keyring, or KWallet). Headless servers without one should use
// FileProvider or VaultProvider instead.
//
// KeychainProvider is safe for concurrent use. mu guards all access to
// trackedKeys.
type KeychainProvider struct {
	service string
	mu      sync.RWMutex
	// trackedKeys is maintained in-process for List() support, because the
	// go-keyring API doesn't provide a native list-by-service operation.
	// On cold start, List() returns only keys set during this process.
	// All reads and writes are protected by mu.
	trackedKeys map[string]struct{}
}

// NewKeychainProvider returns a provider namespaced to the given service name.
// Service must not be empty — an empty service stores secrets in a shared
// namespace where they can collide across applications.
func NewKeychainProvider(service string) (*KeychainProvider, error) {
	if service == "" {
		return nil, fmt.Errorf("secrets.keychain: service name must not be empty")
	}
	return &KeychainProvider{
		service:     service,
		trackedKeys: make(map[string]struct{}),
	}, nil
}

// Name returns the provider identifier "keychain".
func (p *KeychainProvider) Name() string { return "keychain" }

// Get retrieves the secret stored under the given key from the OS keychain.
func (p *KeychainProvider) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if key == "" {
		return "", ErrInvalidKey
	}
	v, err := keyring.Get(p.service, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("secrets.keychain: key %q: %w", key, ErrNotFound)
		}
		return "", fmt.Errorf("secrets.keychain get %q: %w", key, err)
	}
	return v, nil
}

// Set stores the given value under key in the OS keychain.
func (p *KeychainProvider) Set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return ErrInvalidKey
	}
	if err := keyring.Set(p.service, key, value); err != nil {
		return fmt.Errorf("secrets.keychain set %q: %w", key, err)
	}
	p.mu.Lock()
	p.trackedKeys[key] = struct{}{}
	p.mu.Unlock()
	return nil
}

// Delete removes the secret stored under key from the OS keychain.
func (p *KeychainProvider) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return ErrInvalidKey
	}
	if err := keyring.Delete(p.service, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			// Idempotent: still clean up trackedKeys so List() stays consistent.
			p.mu.Lock()
			delete(p.trackedKeys, key)
			p.mu.Unlock()
			return nil
		}
		return fmt.Errorf("secrets.keychain delete %q: %w", key, err)
	}
	p.mu.Lock()
	delete(p.trackedKeys, key)
	p.mu.Unlock()
	return nil
}

// List returns all keys that have been set during the lifetime of this provider instance.
func (p *KeychainProvider) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.trackedKeys))
	for k := range p.trackedKeys {
		out = append(out, k)
	}
	return out, nil
}
