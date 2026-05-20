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

// lookupExistingSpacesKey returns the access_key of any DO Spaces key
// with the given name, or "" if no match. Used to disambiguate a 403
// "key quota exceeded" response between an account-level quota hit and
// a name-uniqueness conflict from a partial prior run.
//
// Best-effort — failures here are silent (the caller already has a
// useful error and the hint is purely advisory).
func lookupExistingSpacesKey(ctx context.Context, token, name string) string {
	page := 1
	for page <= 10 { // bounded — 10 pages × 100 = 1000 keys
		url := fmt.Sprintf("https://api.digitalocean.com/v2/spaces/keys?per_page=100&page=%d", page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return ""
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return ""
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return ""
		}
		var list struct {
			Keys []struct {
				Name      string `json:"name"`
				AccessKey string `json:"access_key"`
			} `json:"keys"`
			Links struct {
				Pages struct {
					Next string `json:"next"`
				} `json:"pages"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return ""
		}
		for _, k := range list.Keys {
			if k.Name == name {
				return k.AccessKey
			}
		}
		if list.Links.Pages.Next == "" {
			return ""
		}
		page++
	}
	return ""
}

func generateDOSpacesKey(ctx context.Context, config map[string]any) (string, error) {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return "", fmt.Errorf("secrets: provider_credential: DIGITALOCEAN_TOKEN not set")
	}

	name, _ := config["name"].(string)
	if name == "" {
		// Refuse the default fallback. A shared default name causes
		// cross-project collisions in any account that runs more than
		// one workflow-managed deploy — every project would try to
		// create or adopt the same `workflow-spaces-key`. Force the
		// operator to name the key per-project (e.g.
		// `multisite-deploy-key`, `wfcompute-deploy-key`).
		return "", fmt.Errorf("secrets: provider_credential digitalocean.spaces: `name` is required (use a project-unique slug like \"<project>-deploy-key\"; a shared default would collide across projects in the same DO account)")
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
		// fullaccess is required for bootstrap (the IaC state bucket
		// does not exist yet, so the key cannot be scoped to it).
		// Once the bucket is created, the operator should rotate to a
		// bucket-scoped key via the rotate-and-prune flow.
		fmt.Fprintf(os.Stderr, "secrets: WARN provider_credential digitalocean.spaces name=%q is being granted permission=fullaccess (no `bucket:` configured); rotate to a bucket-scoped key after first apply via `wfctl secrets rotate --target SPACES --bucket <state-bucket>` to limit blast radius.\n", name)
		grant = map[string]any{"permission": "fullaccess"}
	}
	payload := map[string]any{"name": name, "grants": []map[string]any{grant}}
	body, _ := json.Marshal(payload)

	// Log the attempted key name to aid troubleshooting. DO's 403
	// "key quota exceeded" can fire on (1) per-account quota (200) AND
	// (2) name uniqueness conflict with a zombie key from a partial
	// prior run — the user needs the attempted name to triage which.
	fmt.Fprintf(os.Stderr, "secrets: DO spaces key create: name=%q bucket=%q grant=%v\n", name, bucket, grant["permission"])

	// Pre-create existence check: the DO API does NOT enforce name
	// uniqueness — successive POSTs with the same `name` happily
	// create duplicate keys. The bootstrap layer is supposed to skip
	// generation when the credential is already stored in the
	// secret store, but a single save-failure leaves us with an
	// orphaned DO key whose secret_key is no longer retrievable.
	// Each subsequent run then creates *another* orphan, accreting
	// duplicates until the account hits the real quota.
	//
	// Pre-check by name and fail loudly with the existing access_key
	// so the operator can either delete the orphan from the DO
	// console or pass --force-rotate (which deletes + recreates).
	// We do NOT auto-adopt because DO returns secret_key only at
	// creation time; we cannot reconstruct the secret half from a
	// list lookup.
	if existing := lookupExistingSpacesKey(ctx, token, name); existing != "" {
		return "", fmt.Errorf("secrets: DO spaces key create name=%q: a key with this name already exists (access_key=%s); DO does NOT enforce name uniqueness, so blind re-create would orphan more keys. Either (1) delete it via https://cloud.digitalocean.com/account/api/spaces and re-run, or (2) re-run with --force-rotate to delete + recreate atomically", name, existing)
	}

	// The DO Spaces Keys endpoint is hardcoded. Tests stub it via the package's
	// rewriteTransport helper (see generators_test.go) — a hermetic mechanism
	// that requires explicit code in the test to take effect. Earlier drafts
	// of this file honored a DIGITALOCEAN_API_URL env var, but that override
	// was removed: an attacker who could set the env var (malicious .env,
	// hostile CI step, multi-tenant runner) would redirect the
	// `Authorization: Bearer <DIGITALOCEAN_TOKEN>` POST to their own server,
	// exfiltrating production credentials. See ADR 0021.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.digitalocean.com/v2/spaces/keys", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("secrets: DO spaces key create name=%q: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		hint := ""
		if resp.StatusCode == http.StatusForbidden && strings.Contains(string(body), "key quota") {
			// Surface the two distinct failure modes that share this
			// response code. Most accounts are nowhere near the real
			// 200-key quota; a 403 here usually means a name conflict.
			existing := lookupExistingSpacesKey(ctx, token, name)
			if existing != "" {
				hint = fmt.Sprintf(" (hint: a Spaces key named %q already exists in this account, access_key=%s; either delete it via the DO console or rename the entry in deploy.prereq.yaml)", name, existing)
			} else {
				hint = " (hint: DO returns this for the account-level 200-key quota AND name-uniqueness conflicts; this error often means a partially-applied prior run left a zombie key — try listing https://cloud.digitalocean.com/account/api/spaces and deleting matches by name)"
			}
		}
		return "", fmt.Errorf("secrets: DO spaces key create name=%q: HTTP %d: %s%s", name, resp.StatusCode, strings.TrimSpace(string(body)), hint)
	}

	var result struct {
		Key struct {
			AccessKey string `json:"access_key"`
			SecretKey string `json:"secret_key"`
			CreatedAt string `json:"created_at"`
		} `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("secrets: DO spaces key parse: %w", err)
	}

	out, err := json.Marshal(map[string]string{
		"access_key": result.Key.AccessKey,
		"secret_key": result.Key.SecretKey,
		"created_at": result.Key.CreatedAt,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}
