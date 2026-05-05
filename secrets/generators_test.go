package secrets

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateSecret_RandomHex(t *testing.T) {
	val, err := GenerateSecret(context.Background(), "random_hex", map[string]any{"length": 16})
	if err != nil {
		t.Fatalf("random_hex: %v", err)
	}
	decoded, err := hex.DecodeString(val)
	if err != nil {
		t.Fatalf("not valid hex: %v", err)
	}
	if len(decoded) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(decoded))
	}
}

func TestGenerateSecret_RandomHex_DefaultLength(t *testing.T) {
	val, err := GenerateSecret(context.Background(), "random_hex", map[string]any{})
	if err != nil {
		t.Fatalf("random_hex default: %v", err)
	}
	decoded, err := hex.DecodeString(val)
	if err != nil {
		t.Fatalf("not valid hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("expected 32 bytes (default), got %d", len(decoded))
	}
}

func TestGenerateSecret_RandomBase64(t *testing.T) {
	val, err := GenerateSecret(context.Background(), "random_base64", map[string]any{"length": 24})
	if err != nil {
		t.Fatalf("random_base64: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		t.Fatalf("not valid base64: %v", err)
	}
	if len(decoded) != 24 {
		t.Errorf("expected 24 bytes, got %d", len(decoded))
	}
}

func TestGenerateSecret_RandomAlphanumeric(t *testing.T) {
	val, err := GenerateSecret(context.Background(), "random_alphanumeric", map[string]any{"length": 20})
	if err != nil {
		t.Fatalf("random_alphanumeric: %v", err)
	}
	if len(val) != 20 {
		t.Errorf("expected length 20, got %d", len(val))
	}
	for _, c := range val {
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if !isAlpha && !isDigit {
			t.Errorf("unexpected character %q in alphanumeric output", c)
		}
	}
}

func TestGenerateSecret_RandomAlphanumeric_Uniqueness(t *testing.T) {
	a, _ := GenerateSecret(context.Background(), "random_alphanumeric", map[string]any{})
	b, _ := GenerateSecret(context.Background(), "random_alphanumeric", map[string]any{})
	if a == b {
		t.Error("two consecutive random_alphanumeric values should not be equal")
	}
}

func TestGenerateSecret_UnknownType(t *testing.T) {
	_, err := GenerateSecret(context.Background(), "nope", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown generator type")
	}
}

func TestGenerateSecret_ProviderCredential_DOSpaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/spaces/keys" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"key": map[string]string{
				"access_key": "AKIAIOSFODNN7EXAMPLE",
				"secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-do-token")

	// Inject test server URL by monkey-patching http.DefaultClient transport.
	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = orig }()

	val, err := GenerateSecret(context.Background(), "provider_credential", map[string]any{
		"source": "digitalocean.spaces",
		"name":   "test-key",
	})
	if err != nil {
		t.Fatalf("provider_credential DO spaces: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if result["access_key"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("access_key = %q", result["access_key"])
	}
	if result["secret_key"] != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("secret_key = %q", result["secret_key"])
	}
}

// TestGenerateSecret_ProviderCredential_DOSpaces_NoBucket is a regression test
// for the bug where the DO Spaces Keys API was called with bucket:"*" and rejected
// with "bucket name must be 3 to 63 characters long".
// When no bucket is in config the grant must use permission:"fullaccess" with no
// bucket field — which is both what the API accepts and the right default for a
// bootstrap key that will manage its own IaC state bucket.
func TestGenerateSecret_ProviderCredential_DOSpaces_NoBucket(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/spaces/keys" {
			http.NotFound(w, r)
			return
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"key": map[string]string{
				"access_key": "AKIAIOSFODNN7EXAMPLE",
				"secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-do-token")
	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = orig }()

	_, err := GenerateSecret(context.Background(), "provider_credential", map[string]any{
		"source": "digitalocean.spaces",
		"name":   "bootstrap-key",
		// No "bucket" key — simulates first-time bootstrap before bucket exists.
	})
	if err != nil {
		t.Fatalf("GenerateSecret (no bucket): %v", err)
	}

	// Parse what was actually sent to the API.
	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	grants, _ := payload["grants"].([]any)
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d: %v", len(grants), grants)
	}
	grant, _ := grants[0].(map[string]any)

	// Must use fullaccess (no bucket field) — DO API rejects bucket:"*".
	if perm, _ := grant["permission"].(string); perm != "fullaccess" {
		t.Errorf("grant.permission = %q, want %q (no bucket → fullaccess)", perm, "fullaccess")
	}
	if _, hasBucket := grant["bucket"]; hasBucket {
		t.Errorf("grant must not have a bucket field when no bucket is configured, got: %v", grant)
	}
}

// TestGenerateSecret_ProviderCredential_DOSpaces_WithBucket verifies that when a
// bucket name is supplied in config, the grant uses readwrite permission scoped
// to that specific bucket.
func TestGenerateSecret_ProviderCredential_DOSpaces_WithBucket(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/spaces/keys" {
			http.NotFound(w, r)
			return
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"key": map[string]string{
				"access_key": "AKIAIOSFODNN7EXAMPLE",
				"secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-do-token")
	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewriteTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = orig }()

	_, err := GenerateSecret(context.Background(), "provider_credential", map[string]any{
		"source": "digitalocean.spaces",
		"bucket": "my-state-bucket",
	})
	if err != nil {
		t.Fatalf("GenerateSecret (with bucket): %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	grants, _ := payload["grants"].([]any)
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	grant, _ := grants[0].(map[string]any)

	if perm, _ := grant["permission"].(string); perm != "readwrite" {
		t.Errorf("grant.permission = %q, want %q", perm, "readwrite")
	}
	if b, _ := grant["bucket"].(string); b != "my-state-bucket" {
		t.Errorf("grant.bucket = %q, want %q", b, "my-state-bucket")
	}
}

func TestGenerateSecret_ProviderCredential_UnknownSource(t *testing.T) {
	_, err := GenerateSecret(context.Background(), "provider_credential", map[string]any{
		"source": "unknown.provider",
	})
	if err == nil {
		t.Error("expected error for unknown provider source")
	}
}

func TestGenerateSecret_ProviderCredential_MissingToken(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "")
	_, err := GenerateSecret(context.Background(), "provider_credential", map[string]any{
		"source": "digitalocean.spaces",
	})
	if err == nil {
		t.Error("expected error when DIGITALOCEAN_TOKEN is unset")
	}
}

// ── infra_output generator ────────────────────────────────────────────────────

func sampleStateOutputs() map[string]map[string]any {
	return map[string]map[string]any{
		"bmw-database": {
			"uri":      "postgres://user:pass@db.example.com:5432/app",
			"host":     "db.example.com",
			"port":     "5432",
			"readonly": "true",
		},
	}
}

func TestGenerateSecret_InfraOutput_Success(t *testing.T) {
	val, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
		"source":         "bmw-database.uri",
		"_state_outputs": sampleStateOutputs(),
	})
	if err != nil {
		t.Fatalf("infra_output: unexpected error: %v", err)
	}
	if val != "postgres://user:pass@db.example.com:5432/app" {
		t.Errorf("got %q", val)
	}
}

func TestGenerateSecret_InfraOutput_DifferentField(t *testing.T) {
	val, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
		"source":         "bmw-database.host",
		"_state_outputs": sampleStateOutputs(),
	})
	if err != nil {
		t.Fatalf("infra_output: unexpected error: %v", err)
	}
	if val != "db.example.com" {
		t.Errorf("got %q", val)
	}
}

func TestGenerateSecret_InfraOutput_MissingSource(t *testing.T) {
	_, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
		"_state_outputs": sampleStateOutputs(),
	})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !containsStr(err.Error(), "source") {
		t.Errorf("error should mention 'source': %v", err)
	}
}

func TestGenerateSecret_InfraOutput_InvalidSourceFormat(t *testing.T) {
	for _, bad := range []string{"nodot", ".nomodule", "nofield."} {
		_, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
			"source":         bad,
			"_state_outputs": sampleStateOutputs(),
		})
		if err == nil {
			t.Errorf("source=%q: expected error for invalid format", bad)
		}
	}
}

func TestGenerateSecret_InfraOutput_NoStateOutputs(t *testing.T) {
	_, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
		"source": "bmw-database.uri",
		// _state_outputs intentionally absent
	})
	if err == nil {
		t.Fatal("expected error when state outputs not provided")
	}
}

func TestGenerateSecret_InfraOutput_MissingModule(t *testing.T) {
	_, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
		"source":         "nonexistent-module.uri",
		"_state_outputs": sampleStateOutputs(),
	})
	if err == nil {
		t.Fatal("expected error for missing module")
	}
	if !containsStr(err.Error(), "nonexistent-module") {
		t.Errorf("error should name the missing module: %v", err)
	}
}

func TestGenerateSecret_InfraOutput_MissingField(t *testing.T) {
	_, err := GenerateSecret(context.Background(), "infra_output", map[string]any{
		"source":         "bmw-database.nonexistent_field",
		"_state_outputs": sampleStateOutputs(),
	})
	if err == nil {
		t.Fatal("expected error for missing field")
	}
	if !containsStr(err.Error(), "nonexistent_field") {
		t.Errorf("error should name the missing field: %v", err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
