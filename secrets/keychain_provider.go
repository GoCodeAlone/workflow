package secrets

import (
	"context"
	"errors"
	"fmt"

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
type KeychainProvider struct {
	service string
	// trackedKeys is maintained in-process for List() support, because the
	// go-keyring API doesn't provide a native list-by-service operation.
	// On cold start, List() returns only keys set during this process.
	trackedKeys map[string]struct{}
}

// NewKeychainProvider returns a provider namespaced to the given service name.
func NewKeychainProvider(service string) *KeychainProvider {
	return &KeychainProvider{
		service:     service,
		trackedKeys: make(map[string]struct{}),
	}
}

func (p *KeychainProvider) Name() string { return "keychain" }

func (p *KeychainProvider) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, err := keyring.Get(p.service, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("secrets.keychain: %w", ErrNotFound)
		}
		return "", fmt.Errorf("secrets.keychain get %q: %w", key, err)
	}
	return v, nil
}

func (p *KeychainProvider) Set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := keyring.Set(p.service, key, value); err != nil {
		return fmt.Errorf("secrets.keychain set %q: %w", key, err)
	}
	p.trackedKeys[key] = struct{}{}
	return nil
}

func (p *KeychainProvider) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := keyring.Delete(p.service, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // idempotent
		}
		return fmt.Errorf("secrets.keychain delete %q: %w", key, err)
	}
	delete(p.trackedKeys, key)
	return nil
}

func (p *KeychainProvider) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(p.trackedKeys))
	for k := range p.trackedKeys {
		out = append(out, k)
	}
	return out, nil
}
