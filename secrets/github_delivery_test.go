package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

const deliveryCredential = "synthetic-github-credential"

func newDeliveryProvider(t *testing.T, scope GitHubSecretScope) *GitHubSecretsProvider {
	t.Helper()
	var p *GitHubSecretsProvider
	var err error
	if scope == GitHubScopeOrg {
		p, err = NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilitySelected, []int64{11, 12})
	} else {
		p, err = NewGitHubSecretsProviderWithToken("owner/repo", deliveryCredential)
		if err == nil && scope == GitHubScopeEnv {
			p.SetEnvironment("preview/blue")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func deliveryNamespacePath(scope GitHubSecretScope) string {
	switch scope {
	case GitHubScopeEnv:
		return "/repos/owner/repo/environments/preview%2Fblue/secrets"
	case GitHubScopeOrg:
		return "/orgs/owner/actions/secrets"
	default:
		return "/repos/owner/repo/actions/secrets"
	}
}

func newPaginationProvider(t *testing.T, scope GitHubSecretScope, direct bool) *GitHubSecretsProvider {
	t.Helper()
	if direct {
		return newDeliveryProvider(t, scope)
	}
	t.Setenv("GH_DELIVERY_TEST_TOKEN", deliveryCredential)
	var p *GitHubSecretsProvider
	var err error
	if scope == GitHubScopeOrg {
		p, err = NewGitHubOrgSecretsProvider("owner", "GH_DELIVERY_TEST_TOKEN", OrgVisibilitySelected, []int64{11, 12})
	} else {
		p, err = NewGitHubSecretsProvider("owner/repo", "GH_DELIVERY_TEST_TOKEN")
		if err == nil && scope == GitHubScopeEnv {
			p.SetEnvironment("preview/blue")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGitHubDeliveryDirectConstructors(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ambient-credential-must-not-be-used")
	p := newDeliveryProvider(t, GitHubScopeRepo)
	if p.token != deliveryCredential {
		t.Fatal("direct constructor did not retain the supplied credential")
	}
	if p.client.Timeout <= 0 || p.client.Timeout > 30*time.Second || p.client.CheckRedirect == nil {
		t.Fatal("direct constructor must bound requests and disable redirects")
	}
	if err := p.client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v, want ErrUseLastResponse", err)
	}
	ids := []int64{11, 12}
	op, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilitySelected, ids)
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = 99
	if op.selectedRepoIDs[0] != 11 {
		t.Fatal("constructor retained the caller's mutable repository ID slice")
	}
	private, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, "", nil)
	if err != nil || private.orgVisibility != OrgVisibilityPrivate {
		t.Fatalf("default visibility = %v, error = %v", private, err)
	}
}

func TestGitHubDeliveryConstructorsRejectInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		make func() error
	}{
		{"repo missing owner", func() error { _, err := NewGitHubSecretsProviderWithToken("/repo", deliveryCredential); return err }},
		{"repo extra segment", func() error {
			_, err := NewGitHubSecretsProviderWithToken("owner/repo/extra", deliveryCredential)
			return err
		}},
		{"repo URL injection", func() error {
			_, err := NewGitHubSecretsProviderWithToken("owner/repo?x=1", deliveryCredential)
			return err
		}},
		{"empty token", func() error { _, err := NewGitHubSecretsProviderWithToken("owner/repo", ""); return err }},
		{"header token", func() error {
			_, err := NewGitHubSecretsProviderWithToken("owner/repo", deliveryCredential+"\n")
			return err
		}},
		{"org slash", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner/other", deliveryCredential, "", nil)
			return err
		}},
		{"visibility", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, "unknown", nil)
			return err
		}},
		{"missing selected IDs", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilitySelected, nil)
			return err
		}},
		{"negative ID", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilitySelected, []int64{-1})
			return err
		}},
		{"zero ID", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilitySelected, []int64{0})
			return err
		}},
		{"duplicate ID", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilitySelected, []int64{11, 11})
			return err
		}},
		{"IDs with private", func() error {
			_, err := NewGitHubOrgSecretsProviderWithToken("owner", deliveryCredential, OrgVisibilityPrivate, []int64{11})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.make()
			if err == nil || strings.Contains(err.Error(), deliveryCredential) {
				t.Fatalf("expected a credential-free validation error, got %v", err)
			}
		})
	}
}

func TestGitHubDeliveryExactStatAcrossScopes(t *testing.T) {
	for _, scope := range []GitHubSecretScope{GitHubScopeRepo, GitHubScopeEnv, GitHubScopeOrg} {
		t.Run(string(scope), func(t *testing.T) {
			p := newDeliveryProvider(t, scope)
			path := deliveryNamespacePath(scope)
			updated := time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)
			var calls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				if r.Header.Get("Authorization") != "Bearer "+deliveryCredential {
					t.Error("request did not use the direct credential")
				}
				if r.Method == http.MethodGet && r.URL.EscapedPath() == path+"/MISSING" {
					http.NotFound(w, r)
					return
				}
				if r.Method != http.MethodGet || r.URL.EscapedPath() != path+"/NEW_TOKEN" {
					t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL.EscapedPath())
					http.NotFound(w, r)
					return
				}
				json.NewEncoder(w).Encode(ghSecretEntry{Name: "NEW_TOKEN", UpdatedAt: updated})
			}))
			defer srv.Close()
			p.client.Transport = rewriteTransport{base: srv.URL}
			meta, err := p.Stat(context.Background(), "NEW_TOKEN")
			if err != nil || meta.Name != "NEW_TOKEN" || !meta.Exists || !meta.UpdatedAt.Equal(updated) {
				t.Fatalf("Stat = %+v, %v", meta, err)
			}
			missing, err := p.Stat(context.Background(), "MISSING")
			if !errors.Is(err, ErrNotFound) || missing.Exists || missing.Name != "MISSING" {
				t.Fatalf("missing Stat = %+v, %v", missing, err)
			}
			want := []string{"GET " + path + "/NEW_TOKEN", "GET " + path + "/MISSING"}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("requests = %v, want exact metadata requests %v", calls, want)
			}
		})
	}
}

func TestGitHubDeliveryIdentityAcrossScopes(t *testing.T) {
	for _, scope := range []GitHubSecretScope{GitHubScopeRepo, GitHubScopeEnv, GitHubScopeOrg} {
		t.Run(string(scope), func(t *testing.T) {
			p := newDeliveryProvider(t, scope)
			var calls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				if r.Method != http.MethodGet {
					t.Errorf("unexpected identity method: %s", r.Method)
					http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
					return
				}
				switch r.URL.Path {
				case "/repos/owner/repo":
					io.WriteString(w, `{"id":100,"owner":{"id":200}}`)
				case "/orgs/owner":
					io.WriteString(w, `{"id":200}`)
				case "/repos/owner/repo/environments/preview/blue":
					io.WriteString(w, `{"id":300}`)
				default:
					t.Errorf("unexpected identity request: %s %s", r.Method, r.URL.EscapedPath())
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			p.client.Transport = rewriteTransport{base: srv.URL}
			identity, err := p.Identity(context.Background())
			if err != nil || identity.Scope != scope {
				t.Fatalf("Identity = %+v, %v", identity, err)
			}
			if scope == GitHubScopeOrg {
				if identity.OrganizationID != 200 || identity.Visibility != OrgVisibilitySelected || !reflect.DeepEqual(identity.SelectedRepositoryIDs, []int64{11, 12}) {
					t.Fatalf("organization identity = %+v", identity)
				}
				identity.SelectedRepositoryIDs[0] = 99
				if p.selectedRepoIDs[0] != 11 {
					t.Fatal("Identity exposed the mutable provider ID slice")
				}
				if !reflect.DeepEqual(calls, []string{"GET /orgs/owner"}) {
					t.Fatalf("org requests = %v", calls)
				}
			} else {
				if identity.RepositoryID != 100 || identity.OwnerID != 200 {
					t.Fatalf("repository identity = %+v", identity)
				}
				want := []string{"GET /repos/owner/repo"}
				if scope == GitHubScopeEnv {
					if identity.EnvironmentID != 300 {
						t.Fatalf("environment identity = %+v", identity)
					}
					want = append(want, "GET /repos/owner/repo/environments/preview%2Fblue")
				}
				if !reflect.DeepEqual(calls, want) {
					t.Fatalf("identity requests = %v, want %v", calls, want)
				}
			}
		})
	}
}

type deliveryRoundTripper func(*http.Request) (*http.Response, error)

func (f deliveryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubDeliverySafeErrors(t *testing.T) {
	for _, method := range []string{"stat", "identity", "write", "list", "stat-all"} {
		for _, failure := range []string{"forbidden", "transport", "malformed", "redirect"} {
			t.Run(method+"/"+failure, func(t *testing.T) {
				p := newDeliveryProvider(t, GitHubScopeRepo)
				calls := 0
				p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
					calls++
					if failure == "transport" {
						return nil, fmt.Errorf("transport echoed %s", deliveryCredential)
					}
					status := http.StatusForbidden
					body := deliveryCredential
					headers := http.Header{}
					if failure == "malformed" {
						status = http.StatusOK
						body = `{"id": "` + deliveryCredential + `"}`
					}
					if failure == "redirect" {
						status = http.StatusTemporaryRedirect
						headers.Set("Location", "https://unapproved.example/"+deliveryCredential)
					}
					return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				var err error
				switch method {
				case "stat":
					_, err = p.Stat(context.Background(), "NEW_TOKEN")
				case "identity":
					_, err = p.Identity(context.Background())
				case "write":
					_, err = p.SetWithReceipt(context.Background(), "NEW_TOKEN", "synthetic-secret-value")
				case "list":
					_, err = p.List(context.Background())
				case "stat-all":
					_, err = p.StatAll(context.Background())
				}
				if err == nil || strings.Contains(err.Error(), deliveryCredential) {
					t.Fatalf("expected a safe error, got %v", err)
				}
				if errors.Is(err, ErrNotFound) {
					t.Fatal("an inaccessible or malformed response was classified as absent")
				}
				if calls != 1 {
					t.Fatalf("made %d requests after failure, want 1", calls)
				}
			})
		}
	}
}

func TestGitHubDeliverySafeMethodsIgnoreLegacyRedirectPolicy(t *testing.T) {
	for _, method := range []string{"stat", "identity", "write"} {
		t.Run(method, func(t *testing.T) {
			t.Setenv("GH_DELIVERY_TEST_TOKEN", deliveryCredential)
			p, err := NewGitHubSecretsProvider("owner/repo", "GH_DELIVERY_TEST_TOKEN")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			p.client.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
			p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://unapproved.example/" + deliveryCredential}}, Body: io.NopCloser(strings.NewReader(deliveryCredential)), Request: r}, nil
			})
			switch method {
			case "stat":
				_, err = p.Stat(context.Background(), "NEW_TOKEN")
			case "identity":
				_, err = p.Identity(context.Background())
			case "write":
				_, err = p.SetWithReceipt(context.Background(), "NEW_TOKEN", "synthetic-secret-value")
			}
			if err == nil || strings.Contains(err.Error(), deliveryCredential) || calls != 1 {
				t.Fatalf("legacy client safe method: error = %v, calls = %d", err, calls)
			}
		})
	}
}

func TestGitHubDeliveryRejectsIncompleteMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		identity   bool
	}{
		{"wrong secret name", `{"name":"OTHER_TOKEN"}`, false},
		{"missing repository ID", `{"owner":{"id":200}}`, true},
		{"missing owner ID", `{"id":100}`, true},
		{"trailing response", `{"id":100,"owner":{"id":200}} {}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newDeliveryProvider(t, GitHubScopeRepo)
			p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			var err error
			if tc.identity {
				_, err = p.Identity(context.Background())
			} else {
				_, err = p.Stat(context.Background(), "NEW_TOKEN")
			}
			if err == nil {
				t.Fatal("incomplete/mismatched metadata was accepted")
			}
		})
	}
}

func TestGitHubDeliveryCanceledBeforePUTIsDefinite(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newDeliveryProvider(t, GitHubScopeRepo)
	puts := 0
	p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			puts++
		}
		body, _ := json.Marshal(repoPublicKeyResponse{KeyID: "delivery-key", Key: base64.StdEncoding.EncodeToString(pub[:])})
		cancel()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})
	receipt, err := p.SetWithReceipt(ctx, "NEW_TOKEN", "synthetic-secret-value")
	if !errors.Is(err, context.Canceled) || receipt.MayHaveWritten || receipt.Created || puts != 0 {
		t.Fatalf("pre-PUT cancellation: receipt = %+v, error = %v, PUTs = %d", receipt, err, puts)
	}
}

func TestGitHubDeliveryInvalidKeysNeverRequest(t *testing.T) {
	for _, key := range []string{"", "123_TOKEN", "GITHUB_TOKEN", "github_test", "TOKEN/OTHER", strings.Repeat("A", 257)} {
		t.Run(key, func(t *testing.T) {
			p := newDeliveryProvider(t, GitHubScopeRepo)
			calls := 0
			p.client.Transport = deliveryRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("request should not be made")
			})
			if _, err := p.Stat(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Stat key error = %v", err)
			}
			receipt, err := p.SetWithReceipt(context.Background(), key, "synthetic-secret-value")
			if !errors.Is(err, ErrInvalidKey) || receipt.Created || receipt.MayHaveWritten || calls != 0 {
				t.Fatalf("invalid key: receipt = %+v, error = %v, requests = %d", receipt, err, calls)
			}
		})
	}
}

func TestGitHubDeliveryWriteReceiptAndEncryption(t *testing.T) {
	for _, scope := range []GitHubSecretScope{GitHubScopeRepo, GitHubScopeEnv, GitHubScopeOrg} {
		for _, status := range []int{http.StatusCreated, http.StatusNoContent} {
			t.Run(fmt.Sprintf("%s/%d", scope, status), func(t *testing.T) {
				pub, priv, err := box.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				p := newDeliveryProvider(t, scope)
				path := deliveryNamespacePath(scope)
				var payload struct {
					EncryptedValue        string  `json:"encrypted_value"`
					KeyID                 string  `json:"key_id"`
					Visibility            string  `json:"visibility"`
					SelectedRepositoryIDs []int64 `json:"selected_repository_ids"`
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodGet:
						if r.URL.EscapedPath() != path+"/public-key" {
							t.Errorf("unexpected public key path = %s", r.URL.EscapedPath())
							http.NotFound(w, r)
							return
						}
						json.NewEncoder(w).Encode(repoPublicKeyResponse{KeyID: "delivery-key", Key: base64.StdEncoding.EncodeToString(pub[:])})
					case http.MethodPut:
						if r.URL.EscapedPath() != path+"/NEW_TOKEN" {
							t.Errorf("unexpected secret path = %s", r.URL.EscapedPath())
							http.NotFound(w, r)
							return
						}
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							t.Error(err)
						}
						w.WriteHeader(status)
					default:
						t.Errorf("unexpected delivery method: %s", r.Method)
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				p.client.Transport = rewriteTransport{base: srv.URL}
				receipt, err := p.SetWithReceipt(context.Background(), "NEW_TOKEN", "synthetic-secret-value")
				if err != nil || receipt.Created != (status == http.StatusCreated) || !receipt.MayHaveWritten {
					t.Fatalf("receipt = %+v, error = %v", receipt, err)
				}
				if payload.KeyID != "delivery-key" || payload.EncryptedValue == "" {
					t.Fatal("missing encrypted payload/key ID")
				}
				sealed, err := base64.StdEncoding.DecodeString(payload.EncryptedValue)
				if err != nil || len(sealed) < 32+box.Overhead {
					t.Fatalf("invalid sealed box: %v", err)
				}
				var ephemeral [32]byte
				copy(ephemeral[:], sealed[:32])
				h, err := blake2b.New(24, nil)
				if err != nil {
					t.Fatal(err)
				}
				h.Write(ephemeral[:])
				h.Write(pub[:])
				var nonce [24]byte
				copy(nonce[:], h.Sum(nil))
				plaintext, ok := box.Open(nil, sealed[32:], &nonce, &ephemeral, priv)
				if !ok || string(plaintext) != "synthetic-secret-value" {
					t.Fatal("GitHub-compatible sealed-box decryption did not recover the supplied value")
				}
				if scope == GitHubScopeOrg {
					if payload.Visibility != "selected" || !reflect.DeepEqual(payload.SelectedRepositoryIDs, []int64{11, 12}) {
						t.Fatal("organization visibility/repository selection not preserved")
					}
				} else if payload.Visibility != "" || len(payload.SelectedRepositoryIDs) != 0 {
					t.Fatal("repository/environment upload included organization controls")
				}
			})
		}
	}
}

func TestGitHubDeliveryWriteFailureReceipts(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		publicKeyStatus int
		publicKey       string
		putStatus       int
		putError        bool
		mayHaveWritten  bool
		wantPUT         bool
	}{
		{"public key forbidden", 403, "", 0, false, false, false},
		{"invalid public key", 200, "not-a-key", 0, false, false, false},
		{"rejected PUT", 200, base64.StdEncoding.EncodeToString(pub[:]), 403, false, false, true},
		{"server PUT failure", 200, base64.StdEncoding.EncodeToString(pub[:]), 500, false, true, true},
		{"gateway PUT timeout", 200, base64.StdEncoding.EncodeToString(pub[:]), 504, false, true, true},
		{"request PUT timeout", 200, base64.StdEncoding.EncodeToString(pub[:]), 408, false, true, true},
		{"transport PUT failure", 200, base64.StdEncoding.EncodeToString(pub[:]), 0, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newDeliveryProvider(t, GitHubScopeRepo)
			puts := 0
			p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
				status, body := tc.publicKeyStatus, deliveryCredential
				if r.Method == http.MethodGet && status == 200 {
					b, _ := json.Marshal(repoPublicKeyResponse{KeyID: "delivery-key", Key: tc.publicKey})
					body = string(b)
				} else if r.Method == http.MethodPut {
					puts++
					if tc.putError {
						return nil, fmt.Errorf("transport echoed %s", deliveryCredential)
					}
					status = tc.putStatus
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			receipt, err := p.SetWithReceipt(context.Background(), "NEW_TOKEN", "synthetic-secret-value")
			if err == nil || strings.Contains(err.Error(), deliveryCredential) || receipt.Created || receipt.MayHaveWritten != tc.mayHaveWritten {
				t.Fatalf("receipt = %+v, error = %v", receipt, err)
			}
			if (puts == 1) != tc.wantPUT || puts > 1 {
				t.Fatalf("PUT count = %d, want PUT = %v", puts, tc.wantPUT)
			}
		})
	}
}

func TestGitHubDeliveryPUTErrorsHideAllCredentialMaterial(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const issuedValue = "synthetic-issued-value-distinct-from-github-auth"
	for _, tc := range []struct {
		name      string
		status    int
		transport bool
	}{
		{"client rejection", 403, false},
		{"server failure", 500, false},
		{"redirect", 307, false},
		{"transport failure", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newDeliveryProvider(t, GitHubScopeRepo)
			var calls []string
			var ciphertext string
			p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer "+deliveryCredential {
					t.Errorf("unexpected host or authentication")
				}
				if r.Method == http.MethodGet && r.URL.EscapedPath() == "/repos/owner/repo/actions/secrets/public-key" {
					body, _ := json.Marshal(repoPublicKeyResponse{KeyID: "delivery-key", Key: base64.StdEncoding.EncodeToString(pub[:])})
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
				}
				if r.Method != http.MethodPut || r.URL.EscapedPath() != "/repos/owner/repo/actions/secrets/NEW_TOKEN" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
					return nil, errors.New("unexpected request")
				}
				var payload struct {
					EncryptedValue string `json:"encrypted_value"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				ciphertext = payload.EncryptedValue
				leak := deliveryCredential + " " + issuedValue + " " + ciphertext
				if tc.transport {
					return nil, errors.New(leak)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://unapproved.example/" + issuedValue}}, Body: io.NopCloser(strings.NewReader(leak)), Request: r}, nil
			})
			receipt, err := p.SetWithReceipt(context.Background(), "NEW_TOKEN", issuedValue)
			if err == nil || ciphertext == "" {
				t.Fatalf("missing PUT error or ciphertext: %v", err)
			}
			for _, secret := range []string{deliveryCredential, issuedValue, ciphertext} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("PUT error exposed credential material")
				}
			}
			wantCalls := []string{"GET /repos/owner/repo/actions/secrets/public-key", "PUT /repos/owner/repo/actions/secrets/NEW_TOKEN"}
			if !reflect.DeepEqual(calls, wantCalls) || receipt.Created || receipt.MayHaveWritten != (tc.status != 403) {
				t.Fatalf("PUT failure receipt = %+v, calls = %v", receipt, calls)
			}
		})
	}
}

func TestGitHubDeliverySecretPaginationAcrossScopes(t *testing.T) {
	for _, scope := range []GitHubSecretScope{GitHubScopeRepo, GitHubScopeEnv, GitHubScopeOrg} {
		for _, metadata := range []bool{false, true} {
			for _, direct := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/metadata=%v/direct=%v", scope, metadata, direct), func(t *testing.T) {
					p := newPaginationProvider(t, scope, direct)
					path := deliveryNamespacePath(scope)
					var calls []string
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls = append(calls, r.URL.RequestURI())
						if r.Method != http.MethodGet || r.URL.EscapedPath() != path || r.URL.Query().Get("per_page") != "100" {
							t.Errorf("unexpected list request: %s %s", r.Method, r.URL.RequestURI())
							http.NotFound(w, r)
							return
						}
						name := "FIRST"
						switch r.URL.Query().Get("page") {
						case "":
							w.Header().Set("Link", "<https://api.github.com"+path+`?per_page=100&page=2>; rel="next"`)
						case "2":
							name = "SECOND"
						default:
							t.Errorf("unexpected page: %s", r.URL.RequestURI())
							http.NotFound(w, r)
							return
						}
						json.NewEncoder(w).Encode(map[string]any{"secrets": []ghSecretEntry{{Name: name, CreatedAt: time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)}}})
					}))
					defer srv.Close()
					p.client.Transport = rewriteTransport{base: srv.URL}
					var names []string
					if metadata {
						metas, err := p.StatAll(context.Background())
						if err != nil {
							t.Fatal(err)
						}
						for _, meta := range metas {
							if !meta.Exists || meta.UpdatedAt.IsZero() {
								t.Fatalf("incomplete metadata: %+v", meta)
							}
							names = append(names, meta.Name)
						}
					} else {
						var err error
						names, err = p.List(context.Background())
						if err != nil {
							t.Fatal(err)
						}
					}
					wantCalls := []string{path + "?per_page=100", path + "?page=2&per_page=100"}
					if !reflect.DeepEqual(names, []string{"FIRST", "SECOND"}) || !reflect.DeepEqual(calls, wantCalls) {
						t.Fatalf("names = %v, calls = %v; want both pages via %v", names, calls, wantCalls)
					}
				})
			}
		}
	}
}

func TestGitHubDeliveryRejectsUnsafeSecretPagination(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		wantCalls  int
	}{
		{"foreign origin", "https://unapproved.example/repos/owner/repo/actions/secrets?page=2", 1},
		{"foreign scope", githubAPIBase + "/repos/owner/other/actions/secrets?page=2", 1},
		{"foreign path", githubAPIBase + "/user?page=2", 1},
		{"URL credentials", "https://credential@api.github.com/repos/owner/repo/actions/secrets?page=2", 1},
		{"cycle", githubAPIBase + "/repos/owner/repo/actions/secrets?per_page=100", 1},
		{"later cycle", githubAPIBase + "/repos/owner/repo/actions/secrets?page=2", 2},
	} {
		for _, metadata := range []bool{false, true} {
			for _, direct := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/metadata=%v/direct=%v", tc.name, metadata, direct), func(t *testing.T) {
					p := newPaginationProvider(t, GitHubScopeRepo, direct)
					calls := 0
					p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
						calls++
						return &http.Response{StatusCode: 200, Header: http.Header{"Link": []string{"<" + tc.link + `>; rel="next"`}}, Body: io.NopCloser(strings.NewReader(`{"secrets":[{"name":"FIRST"}]}`)), Request: r}, nil
					})
					var err error
					if metadata {
						_, err = p.StatAll(context.Background())
					} else {
						_, err = p.List(context.Background())
					}
					if err == nil || calls != tc.wantCalls {
						t.Fatalf("unsafe pagination error = %v, calls = %d, want %d", err, calls, tc.wantCalls)
					}
				})
			}
		}
	}
}

func TestGitHubDeliverySecretPaginationAllLinkHeaders(t *testing.T) {
	for _, direct := range []bool{false, true} {
		for _, metadata := range []bool{false, true} {
			t.Run(fmt.Sprintf("direct=%v/metadata=%v", direct, metadata), func(t *testing.T) {
				p := newPaginationProvider(t, GitHubScopeRepo, direct)
				var calls []string
				p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
					calls = append(calls, r.URL.RequestURI())
					if r.Method != http.MethodGet || r.URL.EscapedPath() != "/repos/owner/repo/actions/secrets" {
						t.Fatal("unexpected secret-list destination")
					}
					name := "FIRST"
					headers := http.Header{}
					if r.URL.Query().Get("page") == "2" {
						name = "SECOND"
					} else {
						headers.Add("Link", `<https://api.github.com/repos/owner/repo/actions/secrets?per_page=100&page=2>; rel="last"`)
						headers.Add("Link", `<https://api.github.com/repos/owner/repo/actions/secrets?per_page=100&page=2>; rel="next"; title="page, two"`)
					}
					body, _ := json.Marshal(map[string]any{"secrets": []ghSecretEntry{{Name: name}}})
					return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
				})
				var names []string
				if metadata {
					metas, err := p.StatAll(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					for _, meta := range metas {
						names = append(names, meta.Name)
					}
				} else {
					var err error
					names, err = p.List(context.Background())
					if err != nil {
						t.Fatal(err)
					}
				}
				want := []string{"/repos/owner/repo/actions/secrets?per_page=100", "/repos/owner/repo/actions/secrets?page=2&per_page=100"}
				if !reflect.DeepEqual(names, []string{"FIRST", "SECOND"}) || !reflect.DeepEqual(calls, want) {
					t.Fatalf("split Link inventory = %v, requests = %v", names, calls)
				}
			})
		}
	}
}

func TestGitHubDeliverySecretPaginationRejectsMalformedNext(t *testing.T) {
	for _, headers := range [][]string{
		{`https://api.github.com/repos/owner/repo/actions/secrets?page=2; rel="next"`},
		{`<https://api.github.com/repos/owner/repo/actions/secrets?page=2; rel="next"`},
		{`<https://api.github.com/repos/owner/repo/actions/secrets?page=2>; rel="next`},
		{`<https://api.github.com/repos/owner/repo/actions/secrets?page=2>; rel="next"`, `<https://api.github.com/repos/owner/repo/actions/secrets?page=3>; rel="next"`},
		{`<https://api.github.com/repos/owner/repo/actions/secrets?page=2>; rel="next"; rel="next"`},
	} {
		for _, direct := range []bool{false, true} {
			for _, metadata := range []bool{false, true} {
				t.Run(fmt.Sprintf("headers=%q/direct=%v/metadata=%v", headers, direct, metadata), func(t *testing.T) {
					p := newPaginationProvider(t, GitHubScopeRepo, direct)
					calls := 0
					p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
						calls++
						return &http.Response{StatusCode: 200, Header: http.Header{"Link": headers}, Body: io.NopCloser(strings.NewReader(`{"secrets":[{"name":"FIRST"}]}`)), Request: r}, nil
					})
					var err error
					if metadata {
						_, err = p.StatAll(context.Background())
					} else {
						_, err = p.List(context.Background())
					}
					if err == nil || calls != 1 {
						t.Fatalf("malformed/ambiguous Link error = %v, requests = %d", err, calls)
					}
				})
			}
		}
	}
}

func TestGitHubDeliveryRejectsMetadataBeyondReadLimit(t *testing.T) {
	const prefix = `{"name":"NEW_TOKEN"}`
	// A valid JSON prefix and padding fill the old reader limit; the real
	// response continues with another JSON value past that artificial EOF.
	body := prefix + strings.Repeat(" ", (1<<20)-len(prefix)) + `{"name":"OTHER_TOKEN"}`
	p := newDeliveryProvider(t, GitHubScopeRepo)
	p.client.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if meta, err := p.Stat(context.Background(), "NEW_TOKEN"); err == nil || meta.Exists {
		t.Fatalf("oversized metadata was accepted: %+v, %v", meta, err)
	}
}

func TestGitHubDeliveryListingPreservesCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		for _, metadata := range []bool{false, true} {
			t.Run(fmt.Sprintf("deadline=%v/metadata=%v", deadline, metadata), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				want := context.Canceled
				if deadline {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
					want = context.DeadlineExceeded
				} else {
					cancel()
				}
				defer cancel()
				p := newDeliveryProvider(t, GitHubScopeRepo)
				p.client.Transport = deliveryRoundTripper(func(*http.Request) (*http.Response, error) {
					return nil, fmt.Errorf("transport echoed %s", deliveryCredential)
				})
				var err error
				if metadata {
					_, err = p.StatAll(ctx)
				} else {
					_, err = p.List(ctx)
				}
				if !errors.Is(err, want) || strings.Contains(err.Error(), deliveryCredential) {
					t.Fatalf("listing cancellation = %v, want %v without transport details", err, want)
				}
			})
		}
	}
}
