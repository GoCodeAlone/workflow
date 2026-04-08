package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
)

const alphanumChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateSecret produces a secret value based on genType and config.
// Supported types: "random_hex", "random_base64", "random_alphanumeric", "provider_credential".
// For "provider_credential", the returned string is a JSON-encoded map.
func GenerateSecret(ctx context.Context, genType string, config map[string]any) (string, error) {
	switch genType {
	case "random_hex":
		return generateRandomHex(config)
	case "random_base64":
		return generateRandomBase64(config)
	case "random_alphanumeric":
		return generateRandomAlphanumeric(config)
	case "provider_credential":
		return generateProviderCredential(ctx, config)
	default:
		return "", fmt.Errorf("secrets: unknown generator type %q", genType)
	}
}

func configLength(config map[string]any, def int) int {
	if v, ok := config["length"]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return def
}

func generateRandomHex(config map[string]any) (string, error) {
	n := configLength(config, 32)
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secrets: random_hex: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func generateRandomBase64(config map[string]any) (string, error) {
	n := configLength(config, 32)
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secrets: random_base64: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func generateRandomAlphanumeric(config map[string]any) (string, error) {
	n := configLength(config, 32)
	result := make([]byte, n)
	charCount := big.NewInt(int64(len(alphanumChars)))
	for i := range result {
		idx, err := rand.Int(rand.Reader, charCount)
		if err != nil {
			return "", fmt.Errorf("secrets: random_alphanumeric: %w", err)
		}
		result[i] = alphanumChars[idx.Int64()]
	}
	return string(result), nil
}

func generateProviderCredential(ctx context.Context, config map[string]any) (string, error) {
	source, _ := config["source"].(string)
	switch source {
	case "digitalocean.spaces":
		return generateDOSpacesKey(ctx, config)
	default:
		return "", fmt.Errorf("secrets: provider_credential: unknown source %q", source)
	}
}

func generateDOSpacesKey(ctx context.Context, config map[string]any) (string, error) {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return "", fmt.Errorf("secrets: provider_credential: DIGITALOCEAN_TOKEN not set")
	}

	name, _ := config["name"].(string)
	if name == "" {
		name = "workflow-spaces-key"
	}

	payload := map[string]any{"name": name, "grants": []map[string]any{{"bucket": "*", "permission": "readwrite"}}}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.digitalocean.com/v2/spaces/keys", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("secrets: DO spaces key create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("secrets: DO spaces key create: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Key struct {
			AccessKey string `json:"access_key"`
			SecretKey string `json:"secret_key"`
		} `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("secrets: DO spaces key parse: %w", err)
	}

	out, err := json.Marshal(map[string]string{
		"access_key": result.Key.AccessKey,
		"secret_key": result.Key.SecretKey,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}
