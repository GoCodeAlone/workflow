package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"crypto/rand"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

// roundtripStub is a GitHub Actions secrets API stub that records
// every request + decrypts every PUT body using a deterministic key
// pair (generated per-test). Used by the T20 integration matrix.
type roundtripStub struct {
	mu       sync.Mutex
	requests []recordedRequest
	pubKey   [32]byte
	privKey  [32]byte
}

type recordedRequest struct {
	Method  string
	Path    string
	Payload map[string]any
	// DecryptedValue is the plaintext recovered from
	// encrypted_value (only populated for PUT requests).
	DecryptedValue string
}

func newRoundtripStub(t *testing.T) (*roundtripStub, *httptest.Server) {
	t.Helper()
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate stub key: %v", err)
	}
	stub := &roundtripStub{pubKey: *pub, privKey: *priv}
	srv := httptest.NewServer(stub)
	return stub, srv
}

func (s *roundtripStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/public-key"):
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key_id": "stub-key-id",
			"key":    base64.StdEncoding.EncodeToString(s.pubKey[:]),
		})
		return
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		req := recordedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Payload: payload,
		}
		if encStr, ok := payload["encrypted_value"].(string); ok {
			if decrypted, ok := s.decrypt(encStr); ok {
				req.DecryptedValue = decrypted
			}
		}
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		return
	default:
		http.NotFound(w, r)
	}
}

// decrypt inverts encryptSecret. The wire shape is:
//
//	eph_pub_key (32 bytes) || box.Seal(plaintext, nonce, recipient, eph_priv)
//
// where nonce = blake2b-192(eph_pub || recipient_pub) per libsodium
// sealed-box.
func (s *roundtripStub) decrypt(encB64 string) (string, bool) {
	cipher, err := base64.StdEncoding.DecodeString(encB64)
	if err != nil || len(cipher) < 32 {
		return "", false
	}
	var ephPub [32]byte
	copy(ephPub[:], cipher[:32])

	h, err := blake2b.New(24, nil)
	if err != nil {
		return "", false
	}
	h.Write(ephPub[:])
	h.Write(s.pubKey[:])
	var nonce [24]byte
	copy(nonce[:], h.Sum(nil))

	plain, ok := box.Open(nil, cipher[32:], &nonce, &ephPub, &s.privKey)
	if !ok {
		return "", false
	}
	return string(plain), true
}

func (s *roundtripStub) calls() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

// TestScope_Integration_Matrix exercises every (scope) × (set value
// + roundtrip) combination through one httptest stub.
//
// Per workflow#735 SPEC T20.
func TestScope_Integration_Matrix(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "stub-token")

	cases := []struct {
		name         string
		buildProv    func(t *testing.T, base string) *GitHubSecretsProvider
		secret       string
		value        string
		wantPath     string
		extraPayload map[string]any
	}{
		{
			name: "repo-scope writes to /repos/.../actions/secrets",
			buildProv: func(t *testing.T, _ string) *GitHubSecretsProvider {
				p, err := NewGitHubSecretsProvider("acme/repo", "GITHUB_TOKEN")
				if err != nil {
					t.Fatalf("NewGitHubSecretsProvider: %v", err)
				}
				return p
			},
			secret:       "REPO_SECRET",
			value:        "repo-value",
			wantPath:     "/repos/acme/repo/actions/secrets/REPO_SECRET",
			extraPayload: nil,
		},
		{
			name: "env-scope writes to /repos/.../environments/.../secrets",
			buildProv: func(t *testing.T, _ string) *GitHubSecretsProvider {
				p, err := NewGitHubSecretsProvider("acme/repo", "GITHUB_TOKEN")
				if err != nil {
					t.Fatalf("NewGitHubSecretsProvider: %v", err)
				}
				p.SetEnvironment("staging")
				return p
			},
			secret:       "ENV_SECRET",
			value:        "env-value",
			wantPath:     "/repos/acme/repo/environments/staging/secrets/ENV_SECRET",
			extraPayload: nil,
		},
		{
			name: "org-scope all-visibility writes to /orgs/.../actions/secrets",
			buildProv: func(t *testing.T, _ string) *GitHubSecretsProvider {
				p, err := NewGitHubOrgSecretsProvider("acme", "GITHUB_TOKEN", OrgVisibilityAll, nil)
				if err != nil {
					t.Fatalf("NewGitHubOrgSecretsProvider: %v", err)
				}
				return p
			},
			secret:   "ORG_SECRET",
			value:    "org-value",
			wantPath: "/orgs/acme/actions/secrets/ORG_SECRET",
			extraPayload: map[string]any{
				"visibility": "all",
			},
		},
		{
			name: "org-scope private-visibility omits selected_repository_ids",
			buildProv: func(t *testing.T, _ string) *GitHubSecretsProvider {
				p, err := NewGitHubOrgSecretsProvider("acme", "GITHUB_TOKEN", OrgVisibilityPrivate, nil)
				if err != nil {
					t.Fatalf("NewGitHubOrgSecretsProvider: %v", err)
				}
				return p
			},
			secret:   "PRIVATE_SECRET",
			value:    "private-value",
			wantPath: "/orgs/acme/actions/secrets/PRIVATE_SECRET",
			extraPayload: map[string]any{
				"visibility": "private",
			},
		},
		{
			name: "org-scope selected-visibility includes repo IDs",
			buildProv: func(t *testing.T, _ string) *GitHubSecretsProvider {
				p, err := NewGitHubOrgSecretsProvider("acme", "GITHUB_TOKEN", OrgVisibilitySelected, []int64{1, 2, 3})
				if err != nil {
					t.Fatalf("NewGitHubOrgSecretsProvider: %v", err)
				}
				return p
			},
			secret:   "SELECTED_SECRET",
			value:    "selected-value",
			wantPath: "/orgs/acme/actions/secrets/SELECTED_SECRET",
			extraPayload: map[string]any{
				"visibility": "selected",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub, srv := newRoundtripStub(t)
			defer srv.Close()
			p := c.buildProv(t, srv.URL)
			p.client = &http.Client{Transport: rewriteTransport{base: srv.URL}}

			if err := p.Set(context.Background(), c.secret, c.value); err != nil {
				t.Fatalf("Set: %v", err)
			}

			calls := stub.calls()
			var putCall recordedRequest
			for _, ca := range calls {
				if ca.Method == http.MethodPut {
					putCall = ca
					break
				}
			}
			if putCall.Method == "" {
				t.Fatalf("no PUT recorded; calls=%v", calls)
			}
			if putCall.Path != c.wantPath {
				t.Errorf("PUT path = %q; want %q", putCall.Path, c.wantPath)
			}
			if putCall.DecryptedValue != c.value {
				t.Errorf("roundtripped value = %q; want %q", putCall.DecryptedValue, c.value)
			}
			for k, want := range c.extraPayload {
				if got := putCall.Payload[k]; got != want {
					t.Errorf("payload[%q] = %v; want %v (full payload: %v)", k, got, want, putCall.Payload)
				}
			}
			// Repo+env scopes must NOT include visibility.
			if c.extraPayload == nil {
				if _, has := putCall.Payload["visibility"]; has {
					t.Errorf("non-org scope leaked visibility into payload: %v", putCall.Payload)
				}
			}
		})
	}
}

// TestScope_Integration_SelectedRepoIDsPropagate verifies that the
// selected_repository_ids array is serialised correctly when the
// org provider uses visibility=selected.
func TestScope_Integration_SelectedRepoIDsPropagate(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "stub")
	stub, srv := newRoundtripStub(t)
	defer srv.Close()

	p, err := NewGitHubOrgSecretsProvider("acme", "GITHUB_TOKEN", OrgVisibilitySelected, []int64{42, 1337})
	if err != nil {
		t.Fatalf("NewGitHubOrgSecretsProvider: %v", err)
	}
	p.client = &http.Client{Transport: rewriteTransport{base: srv.URL}}

	if err := p.Set(context.Background(), "K", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	calls := stub.calls()
	var putPayload map[string]any
	for _, c := range calls {
		if c.Method == http.MethodPut {
			putPayload = c.Payload
		}
	}
	ids, _ := putPayload["selected_repository_ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("selected_repository_ids = %v (want [42, 1337])", ids)
	}
	// JSON decoded into []any with float64 elements.
	want := map[float64]bool{42: true, 1337: true}
	for _, id := range ids {
		fid, ok := id.(float64)
		if !ok {
			t.Errorf("id %v not numeric", id)
			continue
		}
		if !want[fid] {
			t.Errorf("unexpected repo id %v", fid)
		}
		delete(want, fid)
	}
	if len(want) != 0 {
		t.Errorf("missing repo IDs: %v", want)
	}
}
