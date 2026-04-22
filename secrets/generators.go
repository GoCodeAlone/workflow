package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
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
	case "infra_output":
		return generateFromInfraOutput(config)
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

// generateFromInfraOutput resolves a secret value from the outputs of a
// previously-applied IaC resource. The caller is expected to pre-load the
// resource outputs into config["_state_outputs"] (map[string]map[string]any)
// before invoking GenerateSecret. This separation keeps the generator
// stateless and fully testable without a real state backend.
//
// config["source"] must be "module_name.output_field" (e.g. "bmw-database.uri").
func generateFromInfraOutput(config map[string]any) (string, error) {
	source, _ := config["source"].(string)
	if source == "" {
		return "", fmt.Errorf("secrets: infra_output: 'source' is required (format: \"module.field\")")
	}
	dot := strings.Index(source, ".")
	if dot < 1 || dot >= len(source)-1 {
		return "", fmt.Errorf("secrets: infra_output: invalid source %q: expected \"module.field\" format", source)
	}
	moduleName := source[:dot]
	field := source[dot+1:]

	stateOutputs, _ := config["_state_outputs"].(map[string]map[string]any)
	if stateOutputs == nil {
		return "", fmt.Errorf("secrets: infra_output: state outputs not available for source %q — did infra apply succeed?", source)
	}
	outputs, ok := stateOutputs[moduleName]
	if !ok {
		return "", fmt.Errorf("secrets: infra_output: module %q not found in state (available: %s)", moduleName, joinKeys(stateOutputs))
	}
	val, ok := outputs[field]
	if !ok {
		return "", fmt.Errorf("secrets: infra_output: field %q not found in outputs of module %q", field, moduleName)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("secrets: infra_output: output field %q of module %q is %T, expected string", field, moduleName, val)
	}
	return s, nil
}

// joinKeys returns a comma-separated list of map keys for error messages.
func joinKeys(m map[string]map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
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

	// DO Spaces Keys API requires a concrete bucket name (3-63 chars) when
	// using "read" / "write" / "readwrite" permissions. A wildcard bucket
	// value is rejected ("bucket name must be 3 to 63 characters long").
	// If the caller passes `bucket:` in config, grant that specific bucket;
	// otherwise use "fullaccess" which needs no bucket and is the right
	// default for a bootstrap key that manages its own IaC state bucket.
	bucket, _ := config["bucket"].(string)
	var grant map[string]any
	if bucket != "" {
		grant = map[string]any{"bucket": bucket, "permission": "readwrite"}
	} else {
		grant = map[string]any{"permission": "fullaccess"}
	}
	payload := map[string]any{"name": name, "grants": []map[string]any{grant}}
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("secrets: DO spaces key create: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
