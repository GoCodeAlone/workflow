package module

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/GoCodeAlone/workflow/secrets"
)

// ---------------------------------------------------------------------------
// In-memory secrets.Provider for testing — map-backed, never touches keychain
// ---------------------------------------------------------------------------

type memSecretsProvider struct {
	data map[string]string
}

func newMemSecretsProvider(initial map[string]string) *memSecretsProvider {
	m := &memSecretsProvider{data: make(map[string]string)}
	for k, v := range initial {
		m.data[k] = v
	}
	return m
}

func (p *memSecretsProvider) Name() string { return "mem" }

func (p *memSecretsProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := p.data[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func (p *memSecretsProvider) Set(_ context.Context, key, value string) error {
	p.data[key] = value
	return nil
}

func (p *memSecretsProvider) Delete(_ context.Context, key string) error {
	delete(p.data, key)
	return nil
}

func (p *memSecretsProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.data))
	for k := range p.data {
		keys = append(keys, k)
	}
	return keys, nil
}

// makeTestTokenJSON returns a JSON response for a token endpoint.
func makeTestTokenJSON(accessToken, refreshToken string, expiresIn int) string {
	b, _ := json.Marshal(map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    expiresIn,
		"token_type":    "Bearer",
	})
	return string(b)
}

// makeTestStoredToken serialises an oauth2.Token as JSON for storage in a secrets provider.
func makeTestStoredToken(accessToken, refreshToken string, expiry time.Time) string {
	tok := oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		Expiry:       expiry,
	}
	b, _ := json.Marshal(tok)
	return string(b)
}

// ---------------------------------------------------------------------------
// Test 1: none auth — plain *http.Client with configured timeout
// ---------------------------------------------------------------------------

func TestHTTPClient_NoneAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header for none-auth client")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	m := &HTTPClientModule{
		moduleName: "test-none",
		cfg: HTTPClientConfig{
			Timeout: 10 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type: "none",
			},
		},
	}
	if err := m.buildClient(context.Background(), nil); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	if m.Client() == nil {
		t.Fatal("expected non-nil *http.Client")
	}
	if m.Client().Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", m.Client().Timeout)
	}

	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unexpected status %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Test 2: static_bearer — Authorization: Bearer <token> header injected
// ---------------------------------------------------------------------------

func TestHTTPClient_StaticBearer(t *testing.T) {
	const wantToken = "my-static-token"

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	m := &HTTPClientModule{
		moduleName: "test-bearer",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:        "static_bearer",
				BearerToken: wantToken,
			},
		},
	}
	if err := m.buildClient(context.Background(), nil); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	want := "Bearer " + wantToken
	if gotAuth != want {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, want)
	}
}

// ---------------------------------------------------------------------------
// Test 3: oauth2_client_credentials — fetches token once, caches for next
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2ClientCredentials(t *testing.T) {
	var tokenFetchCount int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenFetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, makeTestTokenJSON("access-token-cc", "", 3600))
	}))
	defer tokenSrv.Close()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	m := &HTTPClientModule{
		moduleName: "test-cc",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:         "oauth2_client_credentials",
				TokenURL:     tokenSrv.URL + "/token",
				ClientID:     "client-id",
				ClientCredential: "client-secret", //nolint:gosec // G101: test credential
			},
		},
	}
	if err := m.buildClient(context.Background(), nil); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	// First request — should trigger token fetch.
	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer resp.Body.Close()

	if gotAuth != "Bearer access-token-cc" {
		t.Errorf("Authorization header: got %q", gotAuth)
	}

	// Second request — token should still be valid; no additional token fetch.
	resp2, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()

	if n := atomic.LoadInt32(&tokenFetchCount); n != 1 {
		t.Errorf("expected 1 token fetch, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 4: oauth2_refresh_token — missing token surfaces 401 error, no panic
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2RefreshToken_TokenAbsent(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should NOT be called — provider has no token.
		t.Error("token endpoint called unexpectedly")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tokenSrv.Close()

	provider := newMemSecretsProvider(nil) // empty — no token

	m := &HTTPClientModule{
		moduleName: "test-rt-absent",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:              "oauth2_refresh_token",
				TokenURL:          tokenSrv.URL + "/token",
				ClientID:          "client-id",
				ClientCredential:  "client-secret", //nolint:gosec // G101: test credential
				TokenProviderName: "mem",
				TokenProviderKey:  "oauth_token",
			},
		},
	}
	if err := m.buildClient(context.Background(), provider); err != nil {
		t.Fatalf("buildClient must not error on absent token: %v", err)
	}

	// Making a request should fail because no token is available.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	_, err := m.Client().Get(upstream.URL)
	if err == nil {
		t.Fatal("expected error when no token is present, got nil")
	}

	// The error must wrap *oauth2.RetrieveError (not a raw panic or other type).
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		t.Errorf("expected *oauth2.RetrieveError, got %T: %v", err, err)
	}
	if re != nil && re.Response != nil && re.Response.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 status in RetrieveError, got %d", re.Response.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Test 5: oauth2_refresh_token — token present, used without refresh
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2RefreshToken_TokenPresent(t *testing.T) {
	var tokenFetchCount int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenFetchCount, 1)
		w.WriteHeader(http.StatusInternalServerError) // should not be called
	}))
	defer tokenSrv.Close()

	// Store a valid (non-expired) token in the provider.
	validToken := makeTestStoredToken("live-access-token", "live-refresh-token",
		time.Now().Add(1*time.Hour))
	provider := newMemSecretsProvider(map[string]string{
		"oauth_token": validToken,
	})

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	m := &HTTPClientModule{
		moduleName: "test-rt-present",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:              "oauth2_refresh_token",
				TokenURL:          tokenSrv.URL + "/token",
				ClientID:          "client-id",
				ClientCredential:  "client-secret", //nolint:gosec // G101: test credential
				TokenProviderName: "mem",
				TokenProviderKey:  "oauth_token",
			},
		},
	}
	if err := m.buildClient(context.Background(), provider); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if gotAuth != "Bearer live-access-token" {
		t.Errorf("Authorization header: got %q", gotAuth)
	}
	if n := atomic.LoadInt32(&tokenFetchCount); n != 0 {
		t.Errorf("expected 0 token fetches, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 6: oauth2_refresh_token — expired token triggers refresh; rotated token
//         persisted back to secrets provider.
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2RefreshToken_Refresh(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, makeTestTokenJSON("new-access-token", "new-refresh-token", 3600))
	}))
	defer tokenSrv.Close()

	// Store an expired token (access token expired, refresh token valid).
	expiredToken := makeTestStoredToken("expired-access-token", "valid-refresh-token",
		time.Now().Add(-1*time.Hour))
	provider := newMemSecretsProvider(map[string]string{
		"oauth_token": expiredToken,
	})

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	m := &HTTPClientModule{
		moduleName: "test-rt-refresh",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:              "oauth2_refresh_token",
				TokenURL:          tokenSrv.URL + "/token",
				ClientID:          "client-id",
				ClientCredential:  "client-secret", //nolint:gosec // G101: test credential
				TokenProviderName: "mem",
				TokenProviderKey:  "oauth_token",
			},
		},
	}
	if err := m.buildClient(context.Background(), provider); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if gotAuth != "Bearer new-access-token" {
		t.Errorf("Authorization header: got %q", gotAuth)
	}

	// Verify the rotated token was persisted back to the provider.
	stored, err := provider.Get(context.Background(), "oauth_token")
	if err != nil {
		t.Fatalf("failed to read persisted token: %v", err)
	}
	var persistedTok oauth2.Token
	if err := json.Unmarshal([]byte(stored), &persistedTok); err != nil {
		t.Fatalf("persisted token is not valid JSON: %v", err)
	}
	if persistedTok.AccessToken != "new-access-token" {
		t.Errorf("persisted access token: got %q, want %q", persistedTok.AccessToken, "new-access-token")
	}
	if persistedTok.RefreshToken != "new-refresh-token" {
		t.Errorf("persisted refresh token: got %q, want %q", persistedTok.RefreshToken, "new-refresh-token")
	}
}

// ---------------------------------------------------------------------------
// Test 7: oauth2_refresh_token — cached token rejected (401); client refreshes
//         and retries once; second request succeeds.
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2RefreshToken_401Retry(t *testing.T) {
	var tokenFetchCount int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenFetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, makeTestTokenJSON("fresh-access-token", "fresh-refresh-token", 3600))
	}))
	defer tokenSrv.Close()

	// Start with a "valid" (not expired) token that the upstream will reject with 401.
	initialToken := makeTestStoredToken("stale-access-token", "valid-refresh-token",
		time.Now().Add(1*time.Hour))
	provider := newMemSecretsProvider(map[string]string{
		"oauth_token": initialToken,
	})

	var requestCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)
		if n == 1 {
			// Reject the first attempt with 401 to trigger the retry path.
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second attempt (after token refresh) should succeed.
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	m := &HTTPClientModule{
		moduleName: "test-rt-401",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:              "oauth2_refresh_token",
				TokenURL:          tokenSrv.URL + "/token",
				ClientID:          "client-id",
				ClientCredential:  "client-secret", //nolint:gosec // G101: test credential
				TokenProviderName: "mem",
				TokenProviderKey:  "oauth_token",
			},
		},
	}
	if err := m.buildClient(context.Background(), provider); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&requestCount); n != 2 {
		t.Errorf("expected 2 upstream requests (original + retry), got %d", n)
	}
	if n := atomic.LoadInt32(&tokenFetchCount); n < 1 {
		t.Errorf("expected at least 1 token fetch during 401 recovery, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 8: oauth2_refresh_token — late token arrival. Provider starts empty;
//         first request errors; token is written to provider externally;
//         subsequent request succeeds — no restart needed.
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2RefreshToken_LateTokenArrival(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Not called in this scenario — we inject the token directly.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tokenSrv.Close()

	provider := newMemSecretsProvider(nil) // empty initially

	m := &HTTPClientModule{
		moduleName: "test-rt-late",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:              "oauth2_refresh_token",
				TokenURL:          tokenSrv.URL + "/token",
				ClientID:          "client-id",
				ClientCredential:  "client-secret", //nolint:gosec // G101: test credential
				TokenProviderName: "mem",
				TokenProviderKey:  "oauth_token",
			},
		},
	}
	if err := m.buildClient(context.Background(), provider); err != nil {
		t.Fatalf("buildClient must not error on absent token: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Phase 1 — no token yet; request should error.
	_, err := m.Client().Get(upstream.URL)
	if err == nil {
		t.Fatal("expected error before token is available")
	}

	// Simulate external token arrival (e.g. via step.secret_set).
	arrivedToken := makeTestStoredToken("arrived-access-token", "arrived-refresh-token",
		time.Now().Add(1*time.Hour))
	if err := provider.Set(context.Background(), "oauth_token", arrivedToken); err != nil {
		t.Fatalf("failed to set token in provider: %v", err)
	}

	// Phase 2 — token now present; request should succeed without restart.
	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request after token arrival failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after token arrival, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Test 9: static_bearer — bearer token resolved from a secrets provider ref
// ---------------------------------------------------------------------------

func TestHTTPClient_StaticBearer_SecretRef(t *testing.T) {
	const wantToken = "secret-bearer-token-from-ref"

	// Seed a secrets provider with the bearer token value.
	prov := newMemSecretsProvider(map[string]string{
		"bearer": wantToken,
	})

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Build module with bearer_token_ref (no inline bearer_token).
	m := &HTTPClientModule{
		moduleName: "test-bearer-ref",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type: "static_bearer",
				// BearerToken intentionally empty — must be resolved from ref.
				BearerTokenRef: SecretRef{
					Provider: "test-secrets",
					Key:      "bearer",
				},
			},
		},
	}

	// Create an isolated app and register the provider under the name the ref expects.
	app := CreateIsolatedApp(t)
	if err := app.RegisterService("test-secrets", prov); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop(context.Background()) }()

	resp, err := m.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	want := "Bearer " + wantToken
	if gotAuth != want {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, want)
	}
}

// ---------------------------------------------------------------------------
// Test 10: oauth2_refresh_token — concurrent requests, one goroutine triggers a
//          401/refresh mid-flight.  Must be race-free under -race.
// ---------------------------------------------------------------------------

func TestHTTPClient_OAuth2RefreshToken_ConcurrentRefresh(t *testing.T) {
	const goroutines = 2
	const requestsEach = 10

	// Token endpoint: always return a fresh token.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, makeTestTokenJSON("concurrent-access-token", "concurrent-refresh-token", 3600))
	}))
	defer tokenSrv.Close()

	// Upstream: first request from goroutine 0 returns 401 to trigger a refresh
	// mid-flight while goroutine 1 may already be in RoundTrip.
	var requestIdx int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestIdx, 1)
		// Force one early 401 to exercise the swap path under concurrency.
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Start with a "valid" token so ReuseTokenSource doesn't need to call Token()
	// before the first request.
	initialToken := makeTestStoredToken("stale-access-token", "valid-refresh-token",
		time.Now().Add(1*time.Hour))
	provider := newMemSecretsProvider(map[string]string{
		"oauth_token": initialToken,
	})

	m := &HTTPClientModule{
		moduleName: "test-concurrent",
		cfg: HTTPClientConfig{
			Timeout: 5 * time.Second,
			Auth: HTTPClientAuthConfig{
				Type:              "oauth2_refresh_token",
				TokenURL:          tokenSrv.URL + "/token",
				ClientID:          "client-id",
				ClientCredential:  "client-secret", //nolint:gosec // G101: test credential
				TokenProviderName: "mem",
				TokenProviderKey:  "oauth_token",
			},
		},
		logger: &noopLogger{},
	}
	if err := m.buildClient(context.Background(), provider); err != nil {
		t.Fatalf("buildClient: %v", err)
	}

	var wg sync.WaitGroup
	var errCount int32

	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range requestsEach {
				resp, err := m.Client().Get(upstream.URL)
				if err != nil {
					// oauth2.RetrieveError on the very first forced-401 is acceptable —
					// the retry path handles it; transient errors from concurrent refresh
					// are not expected but won't fail the race detector check.
					atomic.AddInt32(&errCount, 1)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
					t.Errorf("goroutine %d: unexpected status %d", id, resp.StatusCode)
				}
			}
		}(g)
	}
	wg.Wait()
	// errCount may be non-zero due to the deliberate first-401; we only care about
	// the absence of data races (enforced by -race flag at the go test level).
	t.Logf("concurrent requests completed; transport errors (expected ~1): %d", atomic.LoadInt32(&errCount))
}

// ---------------------------------------------------------------------------
// Integration test (Task 1.13) — load module via factory, do round-trip
// ---------------------------------------------------------------------------

func TestHTTPClient_Factory_NoneAuth(t *testing.T) {
	var requestCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	}))
	defer upstream.Close()

	cfg := map[string]any{
		"base_url": upstream.URL,
		"timeout":  "5s",
		"auth": map[string]any{
			"type": "none",
		},
	}

	mod := HTTPClientModuleFactory("integration-test", cfg)
	if mod == nil {
		t.Fatal("factory returned nil module")
	}

	// Init and Start
	app := CreateIsolatedApp(t)
	if err := mod.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mod.Stop(context.Background()) }()

	// Verify HTTPClient interface is satisfied.
	var _ HTTPClient = mod // compile-time assertion

	if mod.BaseURL() != upstream.URL {
		t.Errorf("BaseURL: got %q, want %q", mod.BaseURL(), upstream.URL)
	}

	resp, err := mod.Client().Get(upstream.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Errorf("expected 1 upstream request, got %d", n)
	}
}
