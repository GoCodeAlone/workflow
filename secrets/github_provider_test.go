package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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

func TestGitHubProvider_EnvironmentScopeUsesEnvironmentEndpoints(t *testing.T) {
	recipientPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/repos/owner/repo/environments/staging/secrets/public-key":
			json.NewEncoder(w).Encode(repoPublicKeyResponse{
				KeyID: "env-key",
				Key:   base64.StdEncoding.EncodeToString(recipientPub[:]),
			})
		case "/repos/owner/repo/environments/staging/secrets/DATABASE_URL":
			if r.Method != http.MethodPut {
				http.Error(w, "bad method", http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/repos/owner/repo/environments/staging/secrets":
			json.NewEncoder(w).Encode(map[string]any{
				"secrets": []map[string]any{{"name": "DATABASE_URL"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	p.SetEnvironment("staging")
	if p.Environment() != "staging" {
		t.Fatalf("Environment() = %q, want staging", p.Environment())
	}
	if err := p.Set(context.Background(), "DATABASE_URL", "postgres://example"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := p.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []string{
		"GET /repos/owner/repo/environments/staging/secrets/public-key",
		"PUT /repos/owner/repo/environments/staging/secrets/DATABASE_URL",
		"GET /repos/owner/repo/environments/staging/secrets",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitHubProvider_ListEnvironments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/repo/environments" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"environments": []map[string]any{
				{"name": "staging"},
				{"name": "production"},
			},
		})
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	envs, err := p.ListEnvironments(context.Background())
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("expected 2 environments, got %d: %v", len(envs), envs)
	}
	if envs[0].Provider != "github" || envs[0].Name != "staging" || !envs[0].Exists {
		t.Fatalf("unexpected first environment: %+v", envs[0])
	}
	if !strings.Contains(envs[0].Label, "staging") || !strings.Contains(envs[0].Label, "owner/repo") {
		t.Fatalf("label = %q, want environment and repo context", envs[0].Label)
	}
}

func TestGitHubProvider_ValidateEnvironmentNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/environments/missing" {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	_, err := p.ValidateEnvironment(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ValidateEnvironment error = %v, want ErrNotFound", err)
	}
}

func TestGitHubProvider_EnsureEnvironmentCreatesMissing(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/environments/preview":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/owner/repo/environments/preview":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"name": "preview"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	env, err := p.EnsureEnvironment(context.Background(), "preview")
	if err != nil {
		t.Fatalf("EnsureEnvironment: %v", err)
	}
	if env.Provider != "github" || env.Name != "preview" || !env.Exists {
		t.Fatalf("unexpected environment: %+v", env)
	}
	want := []string{
		"GET /repos/owner/repo/environments/preview",
		"PUT /repos/owner/repo/environments/preview",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitHubProvider_EnsureEnvironmentUsesExisting(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/environments/production" {
			json.NewEncoder(w).Encode(map[string]any{"name": "production"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	env, err := p.EnsureEnvironment(context.Background(), "production")
	if err != nil {
		t.Fatalf("EnsureEnvironment: %v", err)
	}
	if env.Name != "production" || !env.Exists {
		t.Fatalf("unexpected environment: %+v", env)
	}
	want := []string{"GET /repos/owner/repo/environments/production"}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
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

// TestGitHubProvider_OrgScopeURL asserts org-scoped requests route to
// /orgs/{org}/actions/secrets and that PUT payload includes visibility.
func TestGitHubProvider_OrgScopeURL(t *testing.T) {
	var seenPath string
	var seenPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		switch {
		case strings.HasSuffix(r.URL.Path, "/public-key"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"key_id": "kid",
				"key":    "C2cZi4nfu9ND7+iRGz9Z+Zf2cZ6OAd1d2c2DqEbtv0M=",
			})
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &seenPayload)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("GITHUB_TOKEN", "test-token")
	p, err := NewGitHubOrgSecretsProvider("my-org", "GITHUB_TOKEN", OrgVisibilityAll, nil)
	if err != nil {
		t.Fatalf("NewGitHubOrgSecretsProvider: %v", err)
	}
	p.client = &http.Client{Transport: rewriteTransport{base: srv.URL}}

	if err := p.Set(context.Background(), "MY_SECRET", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !strings.HasPrefix(seenPath, "/orgs/my-org/actions/secrets/") {
		t.Errorf("PUT path = %q; want /orgs/my-org/actions/secrets/...", seenPath)
	}
	if vis, _ := seenPayload["visibility"].(string); vis != "all" {
		t.Errorf("payload visibility = %q; want all", vis)
	}
}

func TestGitHubProvider_OrgScope_Selected_RequiresRepoIDs(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "x")
	_, err := NewGitHubOrgSecretsProvider("my-org", "GITHUB_TOKEN", OrgVisibilitySelected, nil)
	if err == nil {
		t.Fatal("expected error when visibility=selected and no repo IDs")
	}
	if !strings.Contains(err.Error(), "selected_repository_ids") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestGitHubProvider_OrgScope_PrivateVisibility(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/public-key") {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"key_id": "kid", "key": "C2cZi4nfu9ND7+iRGz9Z+Zf2cZ6OAd1d2c2DqEbtv0M=",
			})
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	t.Setenv("GITHUB_TOKEN", "x")
	p, _ := NewGitHubOrgSecretsProvider("o", "GITHUB_TOKEN", OrgVisibilityPrivate, nil)
	p.client = &http.Client{Transport: rewriteTransport{base: srv.URL}}
	_ = p.Set(context.Background(), "K", "v")
	if payload["visibility"] != "private" {
		t.Errorf("visibility = %v want private", payload["visibility"])
	}
}

func TestGitHubProvider_RepoScope_NoVisibility(t *testing.T) {
	// Repo scope must NOT include visibility in payload (org-only field).
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/public-key") {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"key_id": "kid", "key": "C2cZi4nfu9ND7+iRGz9Z+Zf2cZ6OAd1d2c2DqEbtv0M=",
			})
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	p := newTestGitHubProvider(t, srv)
	_ = p.Set(context.Background(), "K", "v")
	if _, hasVis := payload["visibility"]; hasVis {
		t.Errorf("repo-scope PUT should not include visibility; got payload=%v", payload)
	}
}

// ---------------------------------------------------------------------------
// StatAll + CheckAccess tests
// ---------------------------------------------------------------------------

func TestGitHubProvider_StatAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/secrets":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"secrets": []map[string]any{
					{
						"name":       "A",
						"created_at": "2026-05-01T00:00:00Z",
						"updated_at": "2026-05-20T00:00:00Z",
					},
				},
			})
		case "/repos/owner/repo/actions/secrets/public-key":
			// 32 zero bytes encoded as base64
			key := make([]byte, 32)
			json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
				"key_id": "1",
				"key":    base64.StdEncoding.EncodeToString(key),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)

	metas, err := p.StatAll(context.Background())
	if err != nil {
		t.Fatalf("StatAll: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("expected 1 meta, got %d", len(metas))
	}
	if metas[0].Name != "A" {
		t.Errorf("Name = %q, want A", metas[0].Name)
	}
	if !metas[0].Exists {
		t.Error("expected Exists=true")
	}
	// UpdatedAt should be 2026-05-20
	if metas[0].UpdatedAt.Year() != 2026 || metas[0].UpdatedAt.Month() != 5 || metas[0].UpdatedAt.Day() != 20 {
		t.Errorf("UpdatedAt = %v, want 2026-05-20", metas[0].UpdatedAt)
	}
}

func TestGitHubProvider_CheckAccess_Success(t *testing.T) {
	key := make([]byte, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"key_id": "1",
			"key":    base64.StdEncoding.EncodeToString(key),
		})
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	if err := p.CheckAccess(context.Background()); err != nil {
		t.Errorf("CheckAccess expected nil, got %v", err)
	}
}

func TestGitHubProvider_CheckAccess_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"must have admin rights"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := newTestGitHubProvider(t, srv)
	err := p.CheckAccess(context.Background())
	if err == nil {
		t.Fatal("expected non-nil error for 403")
	}
	// Must NOT contain the token string.
	if strings.Contains(err.Error(), "test-token") {
		t.Errorf("error leaks token: %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403: %v", err)
	}
	if !strings.Contains(err.Error(), "creds redacted") {
		t.Errorf("error should mention 'creds redacted': %v", err)
	}
}

func TestGitHubProvider_ScopeReporter(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "x")
	p, _ := NewGitHubSecretsProvider("o/r", "GITHUB_TOKEN")
	if p.Scope() != GitHubScopeRepo {
		t.Errorf("repo: scope = %q", p.Scope())
	}
	p.SetEnvironment("staging")
	if p.Scope() != GitHubScopeEnv {
		t.Errorf("env: scope = %q", p.Scope())
	}
	p.SetEnvironment("")
	if p.Scope() != GitHubScopeRepo {
		t.Errorf("env-clear: scope = %q", p.Scope())
	}
	op, _ := NewGitHubOrgSecretsProvider("o", "GITHUB_TOKEN", OrgVisibilityAll, nil)
	if op.Scope() != GitHubScopeOrg {
		t.Errorf("org: scope = %q", op.Scope())
	}
}
