package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

// newTestGitHubProvider creates a provider wired to a test server.
func newTestGitHubProvider(t *testing.T, srv *httptest.Server) *GitHubSecretsProvider {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token")
	p, err := NewGitHubSecretsProvider("owner/repo", "GITHUB_TOKEN")
	if err != nil {
		t.Fatalf("NewGitHubSecretsProvider: %v", err)
	}
	p.client = &http.Client{Transport: rewriteTransport{base: srv.URL}}
	return p
}

// rewriteTransport redirects requests to a test server.
type rewriteTransport struct{ base string }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = rt.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(req2)
}

func TestGitHubProvider_Get_ReturnsUnsupported(t *testing.T) {
	p := &GitHubSecretsProvider{owner: "o", repo: "r", token: "t", client: &http.Client{}}
	_, err := p.Get(context.Background(), "MY_SECRET")
	if err != ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

func TestGitHubProvider_Name(t *testing.T) {
	p := &GitHubSecretsProvider{}
	if p.Name() != "github" {
		t.Errorf("Name() = %q, want %q", p.Name(), "github")
	}
}

func TestGitHubProvider_List_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/repo/actions/secrets" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"secrets": []map[string]any{
				{"name": "DB_PASSWORD"},
				{"name": "API_KEY"},
			},
		})
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	names, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}
	if names[0] != "DB_PASSWORD" || names[1] != "API_KEY" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestGitHubProvider_Set_SendsEncryptedPayload(t *testing.T) {
	// Generate a NaCl key pair to act as the repo's key pair.
	recipientPub, recipientPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var receivedPayload map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/secrets/public-key":
			json.NewEncoder(w).Encode(repoPublicKeyResponse{
				KeyID: "key123",
				Key:   base64.StdEncoding.EncodeToString(recipientPub[:]),
			})
		case "/repos/owner/repo/actions/secrets/MY_SECRET":
			if r.Method != http.MethodPut {
				http.Error(w, "bad method", http.StatusMethodNotAllowed)
				return
			}
			json.NewDecoder(r.Body).Decode(&receivedPayload)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	if err := p.Set(context.Background(), "MY_SECRET", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if receivedPayload["key_id"] != "key123" {
		t.Errorf("key_id = %q, want %q", receivedPayload["key_id"], "key123")
	}
	if receivedPayload["encrypted_value"] == "" {
		t.Error("encrypted_value is empty")
	}

	// Decrypt to verify roundtrip correctness.
	ciphertext, err := base64.StdEncoding.DecodeString(receivedPayload["encrypted_value"])
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	if len(ciphertext) < 32 {
		t.Fatalf("ciphertext too short: %d bytes", len(ciphertext))
	}

	var senderPub [32]byte
	copy(senderPub[:], ciphertext[:32])

	// Reconstruct nonce: BLAKE2b-192(senderPub || recipientPub).
	// Must use a native 24-byte output — blake2b(x,24) != blake2b(x,32)[:24]
	// because BLAKE2b parameterises the output length into the hash itself.
	h, _ := blake2b.New(24, nil)
	h.Write(senderPub[:])
	h.Write(recipientPub[:])
	var nonce [24]byte
	copy(nonce[:], h.Sum(nil))

	plaintext, ok := box.Open(nil, ciphertext[32:], &nonce, &senderPub, recipientPriv)
	if !ok {
		t.Fatal("decryption failed — sealed box format mismatch")
	}
	if string(plaintext) != "hunter2" {
		t.Errorf("decrypted = %q, want %q", string(plaintext), "hunter2")
	}
}

func TestGitHubProvider_Delete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/repos/owner/repo/actions/secrets/OLD_SECRET" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	if err := p.Delete(context.Background(), "OLD_SECRET"); err != nil {
		t.Errorf("Delete: %v", err)
	}
}

func TestGitHubProvider_Delete_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	err := p.Delete(context.Background(), "MISSING")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// ---------------------------------------------------------------------------
// Error body inclusion tests
// ---------------------------------------------------------------------------

// TestGitHubProvider_Set_ErrorBodyIncluded verifies that a non-2xx response from
// the secrets PUT endpoint includes the response body in the returned error.
func TestGitHubProvider_Set_ErrorBodyIncluded(t *testing.T) {
	recipientPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/secrets/public-key":
			json.NewEncoder(w).Encode(repoPublicKeyResponse{
				KeyID: "key123",
				Key:   base64.StdEncoding.EncodeToString(recipientPub[:]),
			})
		default:
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"Secret","code":"invalid"}]}`)) //nolint:errcheck
		}
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	err = p.Set(context.Background(), "BAD_SECRET", "value")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error should contain status code 422, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Validation Failed") {
		t.Errorf("error should contain response body, got: %v", err)
	}
}

// TestGitHubProvider_Delete_ErrorBodyIncluded verifies that a non-204/404 response
// from the secrets DELETE endpoint includes the response body in the error.
func TestGitHubProvider_Delete_ErrorBodyIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Must have admin rights to Repository."}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	err := p.Delete(context.Background(), "SOME_SECRET")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should contain 403, got: %v", err)
	}
	if !strings.Contains(err.Error(), "admin rights") {
		t.Errorf("error should contain response body, got: %v", err)
	}
}

// TestGitHubProvider_List_ErrorBodyIncluded verifies that a non-200 response from
// the list endpoint includes the response body in the error.
func TestGitHubProvider_List_ErrorBodyIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials","documentation_url":"https://docs.github.com"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	_, err := p.List(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should contain 401, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Bad credentials") {
		t.Errorf("error should contain response body, got: %v", err)
	}
}

// TestGitHubProvider_RepoPublicKey_ErrorBodyIncluded verifies that a non-200 from
// the public-key endpoint (used internally by Set) includes the body in the error.
func TestGitHubProvider_RepoPublicKey_ErrorBodyIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Resource not accessible by integration"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	// Set calls repoPublicKey internally; the body should bubble up.
	err := p.Set(context.Background(), "MY_KEY", "value")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should contain 403, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Resource not accessible") {
		t.Errorf("error should contain response body, got: %v", err)
	}
}

// TestGitHubProvider_EmptyBodyNoTrailingColon verifies that when the error
// response has an empty body the error message doesn't end with ": ".
func TestGitHubProvider_EmptyBodyNoTrailingColon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// No body written.
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	_, err := p.List(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.HasSuffix(msg, ": ") {
		t.Errorf("error should not end with trailing colon-space when body is empty, got: %q", msg)
	}
	if !strings.Contains(msg, "500") {
		t.Errorf("error should contain 500, got: %v", err)
	}
}

func TestNewGitHubSecretsProvider_InvalidRepo(t *testing.T) {
	t.Setenv("GH_TOKEN", "tok")
	_, err := NewGitHubSecretsProvider("no-slash", "GH_TOKEN")
	if err == nil {
		t.Error("expected error for malformed repo")
	}
}

func TestNewGitHubSecretsProvider_MissingToken(t *testing.T) {
	t.Setenv("MISSING_TOKEN_VAR", "")
	_, err := NewGitHubSecretsProvider("owner/repo", "MISSING_TOKEN_VAR")
	if err == nil {
		t.Error("expected error for empty token")
	}
}

// TestBlake2bNonceLengthMatters is a regression guard for the BLAKE2b nonce
// derivation in encryptSecret. libsodium's crypto_box_seal specifies a 24-byte
// nonce derived by hashing to exactly 24 bytes (BLAKE2b-192). Deriving a 32-byte
// hash and truncating to 24 bytes produces a *different* value because BLAKE2b
// parameterises the output length into the hash state — blake2b(x,24) != blake2b(x,32)[:24].
//
// GitHub's secret encryption endpoint rejects payloads encrypted with the wrong
// nonce with "improperly encrypted secret" (HTTP 422). This test documents and
// enforces the distinction so that future refactors cannot silently regress to
// the truncation approach.
func TestBlake2bNonceLengthMatters(t *testing.T) {
	input := []byte("some deterministic test input for blake2b nonce derivation")

	// 24-byte native output (correct — matches libsodium crypto_box_seal).
	h24, err := blake2b.New(24, nil)
	if err != nil {
		t.Fatalf("blake2b.New(24): %v", err)
	}
	h24.Write(input)
	nonce24 := h24.Sum(nil) // exactly 24 bytes

	// 32-byte output truncated to 24 bytes (incorrect — old implementation).
	h32, err := blake2b.New(32, nil)
	if err != nil {
		t.Fatalf("blake2b.New(32): %v", err)
	}
	h32.Write(input)
	nonce32truncated := h32.Sum(nil)[:24]

	if len(nonce24) != 24 {
		t.Fatalf("expected 24-byte output from blake2b.New(24), got %d", len(nonce24))
	}
	if string(nonce24) == string(nonce32truncated) {
		t.Fatal("blake2b(x,24) == blake2b(x,32)[:24] — they should differ; " +
			"if this fails the nonce regression guard is broken and GitHub will " +
			"reject secrets with 'improperly encrypted secret'")
	}
}
