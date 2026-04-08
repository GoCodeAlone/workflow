package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// Reconstruct nonce: BLAKE2b-32(senderPub || recipientPub)[:24]
	h, _ := blake2b.New(32, nil)
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
