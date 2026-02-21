package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	vault "github.com/hashicorp/vault/api"
)

// VaultProvider implements a HashiCorp Vault secret provider using the
// official vault/api client library. It supports KV v2 operations:
// Get, Set, Delete, and List.
type VaultProvider struct {
	config VaultConfig
	client *vault.Client
}

// NewVaultProvider creates a new Vault provider using the official vault/api client.
// It validates the config, creates an api.Client, sets the token and namespace.
func NewVaultProvider(cfg VaultConfig) (*VaultProvider, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("%w: vault address is required", ErrProviderInit)
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("%w: vault token is required", ErrProviderInit)
	}
	if cfg.MountPath == "" {
		cfg.MountPath = "secret"
	}
	cfg.Address = strings.TrimRight(cfg.Address, "/")

	apiCfg := vault.DefaultConfig()
	apiCfg.Address = cfg.Address

	client, err := vault.NewClient(apiCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create vault client: %v", ErrProviderInit, err)
	}

	client.SetToken(cfg.Token)
	if cfg.Namespace != "" {
		client.SetNamespace(cfg.Namespace)
	}

	return &VaultProvider{
		config: cfg,
		client: client,
	}, nil
}

// NewVaultProviderHTTP is a deprecated alias for NewVaultProvider.
// It exists for backward compatibility.
//
// Deprecated: Use NewVaultProvider instead.
func NewVaultProviderHTTP(cfg VaultConfig) (*VaultProvider, error) {
	return NewVaultProvider(cfg)
}

// NewVaultProviderFromClient creates a VaultProvider from an existing vault/api client.
// This is useful for testing or when you need custom client configuration.
func NewVaultProviderFromClient(client *vault.Client, cfg VaultConfig) *VaultProvider {
	if cfg.MountPath == "" {
		cfg.MountPath = "secret"
	}
	return &VaultProvider{
		config: cfg,
		client: client,
	}
}

func (p *VaultProvider) Name() string { return "vault" }

// Get retrieves a secret value from Vault KV v2.
// The key can be in the format "path" or "path#field".
// If #field is specified, returns that specific field from the secret data.
// Otherwise, returns the entire data as JSON.
func (p *VaultProvider) Get(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}

	path, field := parseVaultKey(key)

	kv := p.client.KVv2(p.config.MountPath)
	secret, err := kv.Get(ctx, path)
	if err != nil {
		// Vault returns a 404 for missing secrets
		if isVaultNotFound(err) {
			return "", fmt.Errorf("%w: vault returned not found for key %q", ErrNotFound, key)
		}
		return "", fmt.Errorf("secrets: vault get failed: %w", err)
	}

	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("%w: no data at key %q", ErrNotFound, key)
	}

	if field != "" {
		val, ok := secret.Data[field]
		if !ok {
			return "", fmt.Errorf("%w: field %q not found at key %q", ErrNotFound, field, path)
		}
		return fmt.Sprintf("%v", val), nil
	}

	// Return entire data as JSON
	data, err := json.Marshal(secret.Data)
	if err != nil {
		return "", fmt.Errorf("secrets: failed to marshal vault data: %w", err)
	}
	return string(data), nil
}

// Set stores a secret value in Vault KV v2.
// The value is stored as {"value": val} in the secret data map.
func (p *VaultProvider) Set(ctx context.Context, key, value string) error {
	if key == "" {
		return ErrInvalidKey
	}

	path, _ := parseVaultKey(key)

	kv := p.client.KVv2(p.config.MountPath)
	_, err := kv.Put(ctx, path, map[string]interface{}{
		"value": value,
	})
	if err != nil {
		return fmt.Errorf("secrets: vault set failed: %w", err)
	}
	return nil
}

// Delete removes a secret from Vault KV v2.
func (p *VaultProvider) Delete(ctx context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}

	path, _ := parseVaultKey(key)

	// Use metadata delete for permanent removal
	err := p.client.KVv2(p.config.MountPath).DeleteMetadata(ctx, path)
	if err != nil {
		if isVaultNotFound(err) {
			return fmt.Errorf("%w: key %q", ErrNotFound, key)
		}
		return fmt.Errorf("secrets: vault delete failed: %w", err)
	}
	return nil
}

// List returns all secret keys under the mount path.
// It uses the Vault logical LIST operation on the metadata path.
func (p *VaultProvider) List(ctx context.Context) ([]string, error) {
	return p.listRecursive(ctx, "")
}

// listRecursive walks the metadata tree and collects all leaf keys.
func (p *VaultProvider) listRecursive(ctx context.Context, prefix string) ([]string, error) {
	// Construct metadata path: {mount}/metadata or {mount}/metadata/{prefix}
	// Vault expects no trailing slash on the path — the prefix includes trailing slash for dirs
	metadataPath := p.config.MountPath + "/metadata"
	if prefix != "" {
		metadataPath += "/" + strings.TrimSuffix(prefix, "/")
	}

	secret, err := p.client.Logical().ListWithContext(ctx, metadataPath)
	if err != nil {
		return nil, fmt.Errorf("secrets: vault list failed: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}

	keysRaw, ok := secret.Data["keys"]
	if !ok {
		return nil, nil
	}

	keysList, ok := keysRaw.([]interface{})
	if !ok {
		return nil, nil
	}

	var result []string
	for _, k := range keysList {
		key, ok := k.(string)
		if !ok {
			continue
		}
		fullKey := prefix + key
		if strings.HasSuffix(key, "/") {
			// Directory — recurse
			subKeys, err := p.listRecursive(ctx, fullKey)
			if err != nil {
				return nil, err
			}
			result = append(result, subKeys...)
		} else {
			result = append(result, fullKey)
		}
	}
	return result, nil
}

// Config returns the provider's Vault configuration.
func (p *VaultProvider) Config() VaultConfig { return p.config }

// Client returns the underlying vault/api client for advanced use.
func (p *VaultProvider) Client() *vault.Client { return p.client }

// parseVaultKey splits "path#field" into (path, field).
func parseVaultKey(key string) (path, field string) {
	if idx := strings.LastIndex(key, "#"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}

// isVaultNotFound checks if a vault error is a 404 / not found.
func isVaultNotFound(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "404") ||
		strings.Contains(errMsg, "no secret") ||
		strings.Contains(errMsg, "Not Found")
}
