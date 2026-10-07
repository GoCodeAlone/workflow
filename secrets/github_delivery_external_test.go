package secrets_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/secrets"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

type githubFixtureTransport func(*http.Request) (*http.Response, error)

func (f githubFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func githubFixtureResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

// This test crosses the exported plugin-to-provider boundary. Only the remote
// transport is replaced: identity, exact metadata, encryption, and receipt logic
// are the production provider, with the production API URL retained.
func TestGitHubDeliveryPublicTransportBoundary(t *testing.T) {
	const auth = "external-fixture-github-auth"
	const value = "external-fixture-issued-cloudflare-value"
	for _, scope := range []secrets.GitHubSecretScope{secrets.GitHubScopeRepo, secrets.GitHubScopeEnv, secrets.GitHubScopeOrg} {
		t.Run(string(scope), func(t *testing.T) {
			pub, priv, err := box.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			path := "/repos/owner/repo/actions/secrets"
			if scope == secrets.GitHubScopeEnv {
				path = "/repos/owner/repo/environments/preview%2Fblue/secrets"
			}
			if scope == secrets.GitHubScopeOrg {
				path = "/orgs/owner/actions/secrets"
			}
			var calls []string
			var ciphertext string
			transport := githubFixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" || r.URL.User != nil {
					t.Fatal("transport option changed the production API destination")
				}
				if r.Header.Get("Authorization") != "Bearer "+auth {
					t.Fatal("incorrect direct-token authentication")
				}
				deadline, bounded := r.Context().Deadline()
				if !bounded || time.Until(deadline) > 30*time.Second {
					t.Fatal("transport option bypassed the request timeout")
				}
				if r.Method == http.MethodGet {
					switch r.URL.EscapedPath() {
					case "/repos/owner/repo":
						return githubFixtureResponse(r, 200, `{"id":100,"owner":{"id":200}}`), nil
					case "/repos/owner/repo/environments/preview%2Fblue":
						return githubFixtureResponse(r, 200, `{"id":300}`), nil
					case "/orgs/owner":
						return githubFixtureResponse(r, 200, `{"id":200}`), nil
					case path + "/MISSING_TOKEN":
						return githubFixtureResponse(r, 404, auth), nil
					case path + "/EXISTING_TOKEN":
						return githubFixtureResponse(r, 200, `{"name":"EXISTING_TOKEN","updated_at":"2026-10-07T01:02:03Z"}`), nil
					case path + "/public-key":
						body, _ := json.Marshal(map[string]string{"key_id": "fixture-key", "key": base64.StdEncoding.EncodeToString(pub[:])})
						return githubFixtureResponse(r, 200, string(body)), nil
					}
				}
				if r.Method == http.MethodPut && r.URL.EscapedPath() == path+"/NEW_TOKEN" {
					var payload struct {
						EncryptedValue        string  `json:"encrypted_value"`
						KeyID                 string  `json:"key_id"`
						Visibility            string  `json:"visibility"`
						SelectedRepositoryIDs []int64 `json:"selected_repository_ids"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					ciphertext = payload.EncryptedValue
					if payload.KeyID != "fixture-key" || ciphertext == "" || strings.Contains(ciphertext, value) {
						t.Fatal("invalid encrypted upload")
					}
					if scope == secrets.GitHubScopeOrg {
						if payload.Visibility != "selected" || !reflect.DeepEqual(payload.SelectedRepositoryIDs, []int64{11, 12}) {
							t.Fatal("organization destination controls were lost")
						}
					} else if payload.Visibility != "" || len(payload.SelectedRepositoryIDs) != 0 {
						t.Fatal("unexpected organization controls")
					}
					return githubFixtureResponse(r, 201, auth+" "+value+" "+ciphertext), nil
				}
				t.Fatalf("unexpected remote request: %s %s", r.Method, r.URL.EscapedPath())
				return nil, errors.New("unexpected fixture request")
			})
			option := secrets.WithGitHubSecretsHTTPTransport(transport)
			var p *secrets.GitHubSecretsProvider
			if scope == secrets.GitHubScopeOrg {
				p, err = secrets.NewGitHubOrgSecretsProviderWithToken("owner", auth, secrets.OrgVisibilitySelected, []int64{11, 12}, option)
			} else {
				p, err = secrets.NewGitHubSecretsProviderWithToken("owner/repo", auth, option)
				if err == nil && scope == secrets.GitHubScopeEnv {
					p.SetEnvironment("preview/blue")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			identity, err := p.Identity(context.Background())
			if err != nil || identity.Scope != scope {
				t.Fatalf("Identity = %+v, %v", identity, err)
			}
			wantCalls := []string{"GET /repos/owner/repo"}
			if scope == secrets.GitHubScopeOrg {
				if identity.OrganizationID != 200 || identity.Visibility != secrets.OrgVisibilitySelected || !reflect.DeepEqual(identity.SelectedRepositoryIDs, []int64{11, 12}) {
					t.Fatalf("unexpected organization identity: %+v", identity)
				}
				wantCalls = []string{"GET /orgs/owner"}
			} else {
				if identity.RepositoryID != 100 || identity.OwnerID != 200 {
					t.Fatalf("unexpected repository identity: %+v", identity)
				}
				if scope == secrets.GitHubScopeEnv {
					if identity.EnvironmentID != 300 {
						t.Fatalf("unexpected environment identity: %+v", identity)
					}
					wantCalls = append(wantCalls, "GET /repos/owner/repo/environments/preview%2Fblue")
				}
			}
			missing, err := p.Stat(context.Background(), "MISSING_TOKEN")
			if !errors.Is(err, secrets.ErrNotFound) || missing.Exists || strings.Contains(err.Error(), auth) {
				t.Fatalf("missing metadata = %+v, %v", missing, err)
			}
			meta, err := p.Stat(context.Background(), "EXISTING_TOKEN")
			if err != nil || !meta.Exists || !meta.UpdatedAt.Equal(time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)) {
				t.Fatalf("metadata = %+v, %v", meta, err)
			}
			receipt, err := p.SetWithReceipt(context.Background(), "NEW_TOKEN", value)
			if err != nil || !receipt.Created || !receipt.MayHaveWritten {
				t.Fatalf("receipt = %+v, %v", receipt, err)
			}
			wantCalls = append(wantCalls, "GET "+path+"/MISSING_TOKEN", "GET "+path+"/EXISTING_TOKEN", "GET "+path+"/public-key", "PUT "+path+"/NEW_TOKEN")
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, wantCalls)
			}
			sealed, err := base64.StdEncoding.DecodeString(ciphertext)
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
			if !ok || string(plaintext) != value {
				t.Fatal("public provider boundary did not preserve the issued value through sealed-box encryption")
			}
		})
	}
}

func TestGitHubDeliveryPublicTransportSafeFailures(t *testing.T) {
	const auth = "external-failure-github-auth"
	const value = "external-failure-issued-value"
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"transport", "server", "redirect"} {
		t.Run(failure, func(t *testing.T) {
			var calls []string
			var ciphertext string
			option := secrets.WithGitHubSecretsHTTPTransport(githubFixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.EscapedPath())
				if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
					t.Fatal("unexpected API host")
				}
				if r.Method == http.MethodGet && r.URL.EscapedPath() == "/repos/owner/repo/actions/secrets/public-key" {
					body, _ := json.Marshal(map[string]string{"key_id": "fixture-key", "key": base64.StdEncoding.EncodeToString(pub[:])})
					return githubFixtureResponse(r, 200, string(body)), nil
				}
				if r.Method != http.MethodPut || r.URL.EscapedPath() != "/repos/owner/repo/actions/secrets/NEW_TOKEN" {
					t.Fatal("unexpected API request")
				}
				var payload struct {
					EncryptedValue string `json:"encrypted_value"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				ciphertext = payload.EncryptedValue
				leak := auth + " " + value + " " + ciphertext
				if failure == "transport" {
					return nil, errors.New(leak)
				}
				resp := githubFixtureResponse(r, 500, leak)
				if failure == "redirect" {
					resp.StatusCode = 307
					resp.Header.Set("Location", "https://unapproved.example/"+value)
				}
				return resp, nil
			}))
			p, err := secrets.NewGitHubSecretsProviderWithToken("owner/repo", auth, option)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := p.SetWithReceipt(context.Background(), "NEW_TOKEN", value)
			if err == nil || ciphertext == "" || receipt.Created || !receipt.MayHaveWritten {
				t.Fatalf("failure receipt = %+v, %v", receipt, err)
			}
			for _, sentinel := range []string{auth, value, ciphertext} {
				if strings.Contains(err.Error(), sentinel) {
					t.Fatal("public transport boundary exposed credential material")
				}
			}
			want := []string{"GET /repos/owner/repo/actions/secrets/public-key", "PUT /repos/owner/repo/actions/secrets/NEW_TOKEN"}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestGitHubDeliveryPublicTransportRejectsNil(t *testing.T) {
	option := secrets.WithGitHubSecretsHTTPTransport(nil)
	if _, err := secrets.NewGitHubSecretsProviderWithToken("owner/repo", "synthetic-auth", option); err == nil {
		t.Fatal("nil explicit transport was accepted")
	}
	if _, err := secrets.NewGitHubOrgSecretsProviderWithToken("owner", "synthetic-auth", secrets.OrgVisibilityPrivate, nil, option); err == nil {
		t.Fatal("nil explicit organization transport was accepted")
	}
}
