package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NewVaultProviderHTTP creates a new Vault provider that uses the Vault HTTP API.
// This replaces the stub VaultProvider with real functionality.
func NewVaultProviderHTTP(cfg VaultConfig) (*VaultProvider, error) {
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

	p := &VaultProvider{config: cfg}
	p.httpClient = &http.Client{Timeout: 10 * time.Second}
	return p, nil
}

// vaultReadResponse represents the JSON response from Vault KV v2 read.
type vaultReadResponse struct {
	Data struct {
		Data map[string]interface{} `json:"data"`
	} `json:"data"`
}

// GetFromVault retrieves a secret value from the Vault HTTP API.
// The key can be in the format "path" or "path#field".
// If #field is specified, returns that specific field from the secret data.
// Otherwise, returns the entire data as JSON.
func (p *VaultProvider) GetFromVault(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}
	if p.httpClient == nil {
		return "", fmt.Errorf("%w: vault provider is a stub (use NewVaultProviderHTTP)", ErrUnsupported)
	}

	// Parse key: "path#field" or just "path"
	path, field := parseVaultKey(key)

	url := fmt.Sprintf("%s/v1/%s/data/%s", p.config.Address, p.config.MountPath, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("secrets: failed to create vault request: %w", err)
	}
	req.Header.Set("X-Vault-Token", p.config.Token)
	if p.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.config.Namespace)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("secrets: vault request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("secrets: failed to read vault response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: vault returned status %d for key %q", ErrNotFound, resp.StatusCode, key)
	}

	var vaultResp vaultReadResponse
	if err := json.Unmarshal(body, &vaultResp); err != nil {
		return "", fmt.Errorf("secrets: failed to parse vault response: %w", err)
	}

	if vaultResp.Data.Data == nil {
		return "", fmt.Errorf("%w: no data at key %q", ErrNotFound, key)
	}

	if field != "" {
		val, ok := vaultResp.Data.Data[field]
		if !ok {
			return "", fmt.Errorf("%w: field %q not found at key %q", ErrNotFound, field, path)
		}
		return fmt.Sprintf("%v", val), nil
	}

	// Return entire data as JSON
	data, err := json.Marshal(vaultResp.Data.Data)
	if err != nil {
		return "", fmt.Errorf("secrets: failed to marshal vault data: %w", err)
	}
	return string(data), nil
}

// parseVaultKey splits "path#field" into (path, field).
func parseVaultKey(key string) (path, field string) {
	if idx := strings.LastIndex(key, "#"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}
