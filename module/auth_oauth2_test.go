package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- helpers ---

// newTestOAuth2Setup creates a JWTAuthModule and an OAuth2Module wired together,
// plus a fake OAuth2 provider server whose URLs can be injected into the config.
func newTestOAuth2Setup(t *testing.T, userInfoHandler http.HandlerFunc) (*OAuth2Module, *JWTAuthModule, *httptest.Server) {
	t.Helper()

	jwtAuth := NewJWTAuthModule("jwt-auth", "test-secret", 24*time.Hour, "test")

	// Build a mock OAuth2 provider that handles:
	//   /auth    → authorization endpoint (just redirects back with code)
	//   /token   → token endpoint (returns a fake access_token)
	//   /userinfo → user info endpoint (caller-supplied handler)
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	if userInfoHandler != nil {
		mux.HandleFunc("/userinfo", userInfoHandler)
	}
	server := httptest.NewServer(mux)

	providerCfg := OAuth2ProviderConfig{
		Name:         "testprovider",
		ClientID:     "client-id",
		ClientSecret: "client-secret", //nolint:gosec // test credential
		AuthURL:      server.URL + "/auth",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
		Scopes:       []string{"email"},
		RedirectURL:  "http://localhost/auth/oauth2/testprovider/callback",
	}

	mod := NewOAuth2Module("oauth2", []OAuth2ProviderConfig{providerCfg}, jwtAuth)
	return mod, jwtAuth, server
}

// --- tests ---

func TestOAuth2_Name(t *testing.T) {
	mod := NewOAuth2Module("my-oauth2", nil, nil)
	if mod.Name() != "my-oauth2" {
		t.Errorf("expected 'my-oauth2', got '%s'", mod.Name())
	}
}

func TestOAuth2_Init(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)
	app := CreateIsolatedApp(t)
	if err := mod.Init(app); err != nil {
		t.Errorf("Init should not return error: %v", err)
	}
}

func TestOAuth2_ProvidesServices(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)
	svcs := mod.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "oauth2" {
		t.Errorf("expected service name 'oauth2', got '%s'", svcs[0].Name)
	}
}

func TestOAuth2_LoginRedirect(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth2/testprovider/login", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d; body: %s", w.Code, w.Body.String())
	}

	location := w.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header in redirect")
	}
	// Should point to the fake auth server
	if !containsStr(location, server.URL+"/auth") {
		t.Errorf("expected redirect to %s/auth, got %s", server.URL, location)
	}
	// Must contain state parameter
	if !containsStr(location, "state=") {
		t.Error("expected state parameter in auth URL")
	}
	// Must contain client_id
	if !containsStr(location, "client-id") {
		t.Error("expected client_id in auth URL")
	}

	// Cookie with state should be set
	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "oauth2_state" {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Error("expected oauth2_state cookie to be set")
	}
}

func TestOAuth2_LoginRedirectContainsState(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth2/testprovider/login", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	location := w.Header().Get("Location")

	// Extract state from location URL
	stateFromURL := extractQueryParam(location, "state")
	if stateFromURL == "" {
		t.Fatal("no state in redirect URL")
	}

	// The state must be stored in the module
	mod.mu.Lock()
	_, stored := mod.states[stateFromURL]
	mod.mu.Unlock()
	if !stored {
		t.Error("state was not stored in module")
	}
}

func TestOAuth2_CallbackSuccess(t *testing.T) {
	userInfoHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "42",
			"email": "oauth@example.com",
			"name":  "OAuth User",
		})
	}
	mod, _, server := newTestOAuth2Setup(t, userInfoHandler)
	defer server.Close()

	// Store a state token manually so the callback can validate it.
	state := "test-state-value"
	mod.mu.Lock()
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state+"&code=auth-code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("expected JWT token in response")
	}
	user, ok := resp["user"].(map[string]any)
	if !ok {
		t.Fatal("expected user object in response")
	}
	if user["name"] != "OAuth User" {
		t.Errorf("expected name 'OAuth User', got %v", user["name"])
	}
}

func TestOAuth2_CallbackInvalidState(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state=wrong-state&code=auth-code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for mismatched state, got %d", w.Code)
	}

	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !containsStr(resp["error"], "state") {
		t.Errorf("expected error about state, got %q", resp["error"])
	}
}

func TestOAuth2_CallbackMissingCode(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	state := "valid-state"
	mod.mu.Lock()
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state, nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing code, got %d", w.Code)
	}
}

func TestOAuth2_CallbackExpiredState(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	state := "expired-state"
	mod.mu.Lock()
	// Store a state that has already expired.
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(-time.Minute)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state+"&code=code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for expired state, got %d", w.Code)
	}
}

func TestOAuth2_CallbackUnknownProvider(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/unknown/callback?state=s&code=c", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown provider, got %d", w.Code)
	}
}

func TestOAuth2_LoginUnknownProvider(t *testing.T) {
	mod, _, server := newTestOAuth2Setup(t, nil)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth2/unknown/login", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown provider, got %d", w.Code)
	}
}

func TestOAuth2_CallbackTokenExchangeFailure(t *testing.T) {
	// Override the token endpoint to return an error.
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	jwtAuth := NewJWTAuthModule("jwt-auth", "test-secret", 24*time.Hour, "test")
	providerCfg := OAuth2ProviderConfig{
		Name:         "testprovider",
		ClientID:     "client-id",
		ClientSecret: "client-secret", //nolint:gosec // test credential
		AuthURL:      server.URL + "/auth",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
		Scopes:       []string{"email"},
		RedirectURL:  "http://localhost/callback",
	}
	mod := NewOAuth2Module("oauth2", []OAuth2ProviderConfig{providerCfg}, jwtAuth)

	state := "valid-state"
	mod.mu.Lock()
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state+"&code=bad-code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for failed token exchange, got %d", w.Code)
	}
}

func TestOAuth2_NewUserCreation(t *testing.T) {
	userInfoHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "new-user-99",
			"email": "newuser@example.com",
			"name":  "New User",
		})
	}
	mod, jwtAuth, server := newTestOAuth2Setup(t, userInfoHandler)
	defer server.Close()

	state := "state-new-user"
	mod.mu.Lock()
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state+"&code=auth-code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify user was created in jwtAuth's store using the public lookup.
	key := "oauth:testprovider:new-user-99"
	u, exists := jwtAuth.lookupUser(key)
	if !exists {
		t.Fatal("user was not created in jwtAuth store")
	}
	if u.Name != "New User" {
		t.Errorf("expected name 'New User', got '%s'", u.Name)
	}
}

func TestOAuth2_ReturningUserLookup(t *testing.T) {
	callCount := 0
	userInfoHandler := func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "returning-99",
			"email": "returning@example.com",
			"name":  "Returning User",
		})
	}
	mod, jwtAuth, server := newTestOAuth2Setup(t, userInfoHandler)
	defer server.Close()

	oauthKey := "oauth:testprovider:returning-99"

	// Pre-populate the user in jwtAuth's store via the public method.
	_, err := jwtAuth.CreateOAuthUser(oauthKey, "Returning User", map[string]any{"role": "user"})
	if err != nil {
		t.Fatalf("pre-populate user: %v", err)
	}
	// Override the auto-assigned ID so we can verify the same record is returned.
	preExisting, _ := jwtAuth.lookupUser(oauthKey)
	preExistingID := preExisting.ID

	state := "state-returning"
	mod.mu.Lock()
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state+"&code=auth-code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// User count should not have increased (existing user reused).
	u, exists := jwtAuth.lookupUser(oauthKey)
	if !exists {
		t.Fatal("user not found after callback")
	}
	if u.ID != preExistingID {
		t.Errorf("expected pre-existing user id %s, got id=%s", preExistingID, u.ID)
	}
}

func TestOAuth2_GoogleProviderDefaults(t *testing.T) {
	pc := OAuth2ProviderConfig{
		Name:         "google",
		ClientID:     "gid",
		ClientSecret: "gsecret", //nolint:gosec // test credential
		RedirectURL:  "http://localhost/cb",
	}
	entry := buildProviderEntry(&pc)
	if entry == nil {
		t.Fatal("expected non-nil entry for google provider")
	}
	if entry.userInfoURL != "https://www.googleapis.com/oauth2/v2/userinfo" {
		t.Errorf("unexpected userInfoURL: %s", entry.userInfoURL)
	}
	if len(pc.Scopes) == 0 {
		t.Error("expected default scopes for google")
	}
}

func TestOAuth2_GitHubProviderDefaults(t *testing.T) {
	pc := OAuth2ProviderConfig{
		Name:         "github",
		ClientID:     "ghid",
		ClientSecret: "ghsecret", //nolint:gosec // test credential
		RedirectURL:  "http://localhost/cb",
	}
	entry := buildProviderEntry(&pc)
	if entry == nil {
		t.Fatal("expected non-nil entry for github provider")
	}
	if entry.userInfoURL != "https://api.github.com/user" {
		t.Errorf("unexpected userInfoURL: %s", entry.userInfoURL)
	}
	if len(pc.Scopes) == 0 {
		t.Error("expected default scopes for github")
	}
}

func TestOAuth2_GenericProviderMissingURLs(t *testing.T) {
	pc := OAuth2ProviderConfig{
		Name:         "custom",
		ClientID:     "id",
		ClientSecret: "secret", //nolint:gosec // test credential
		// No AuthURL or TokenURL → entry should be nil
	}
	entry := buildProviderEntry(&pc)
	if entry != nil {
		t.Error("expected nil entry for generic provider missing URLs")
	}
}

func TestOAuth2_MissingClientIDorSecret(t *testing.T) {
	pc := OAuth2ProviderConfig{
		Name:    "google",
		// No ClientID or ClientSecret
	}
	entry := buildProviderEntry(&pc)
	if entry != nil {
		t.Error("expected nil entry when ClientID/ClientSecret missing")
	}
}

func TestOAuth2_StateStoredAndConsumed(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)

	mod.storeState("abc123")

	if !mod.validateAndConsumeState("abc123") {
		t.Error("expected valid state to pass")
	}
	// Consuming twice should fail (state deleted after first use).
	if mod.validateAndConsumeState("abc123") {
		t.Error("expected state to be consumed (one-time use)")
	}
}

func TestOAuth2_EmptyStateRejected(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)
	if mod.validateAndConsumeState("") {
		t.Error("expected empty state to fail validation")
	}
}

func TestOAuth2_PurgeExpiredStates(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)

	mod.mu.Lock()
	mod.states["expired"] = oauth2StateEntry{expiry: time.Now().Add(-time.Second)}
	mod.mu.Unlock()

	// storeState triggers purge
	mod.storeState("fresh")

	mod.mu.Lock()
	_, expiredExists := mod.states["expired"]
	mod.mu.Unlock()

	if expiredExists {
		t.Error("expected expired state to be purged")
	}
}

func TestOAuth2_NotFoundRoute(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/some/other/path", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", w.Code)
	}
}

func TestOAuth2_GenerateState_Uniqueness(t *testing.T) {
	states := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s, err := generateState()
		if err != nil {
			t.Fatalf("generateState error: %v", err)
		}
		if states[s] {
			t.Fatal("duplicate state generated")
		}
		states[s] = true
	}
}

func TestOAuth2_CallbackNoJWTAuth(t *testing.T) {
	// Build a mock provider that works fine but the OAuth2Module has no jwtAuth.
	userInfoHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "1",
			"email": "user@example.com",
			"name":  "User",
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/userinfo", userInfoHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	providerCfg := OAuth2ProviderConfig{
		Name:         "testprovider",
		ClientID:     "cid",
		ClientSecret: "csecret", //nolint:gosec // test credential
		AuthURL:      server.URL + "/auth",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
		Scopes:       []string{"email"},
		RedirectURL:  "http://localhost/cb",
	}
	// nil jwtAuth
	mod := NewOAuth2Module("oauth2", []OAuth2ProviderConfig{providerCfg}, nil)

	state := "state-nojwt"
	mod.mu.Lock()
	mod.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
	mod.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth2/testprovider/callback?state="+state+"&code=code", nil)
	w := httptest.NewRecorder()
	mod.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when jwtAuth not configured, got %d", w.Code)
	}
}

func TestOAuth2_ExtractString(t *testing.T) {
	m := map[string]any{
		"id":    float64(42),
		"email": "e@x.com",
		"empty": "",
	}

	if v := extractString(m, "email"); v != "e@x.com" {
		t.Errorf("expected 'e@x.com', got %q", v)
	}
	if v := extractString(m, "id"); v != "42" {
		t.Errorf("expected '42', got %q", v)
	}
	if v := extractString(m, "empty", "email"); v != "e@x.com" {
		t.Errorf("expected fallback to email, got %q", v)
	}
	if v := extractString(m, "missing"); v != "" {
		t.Errorf("expected empty string for missing key, got %q", v)
	}
}

func TestOAuth2_FetchUserInfoBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	mod := NewOAuth2Module("oauth2", nil, nil)
	entry := &oauth2ProviderEntry{userInfoURL: server.URL}

	_, err := mod.fetchUserInfo(context.Background(), entry, "tok")
	if err == nil {
		t.Error("expected error for non-200 userinfo response")
	}
}

func TestOAuth2_FetchUserInfoEmptyURL(t *testing.T) {
	mod := NewOAuth2Module("oauth2", nil, nil)
	entry := &oauth2ProviderEntry{userInfoURL: ""}
	_, err := mod.fetchUserInfo(context.Background(), entry, "tok")
	if err == nil {
		t.Error("expected error when userInfoURL is empty")
	}
}

// --- test utilities ---

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

func extractQueryParam(rawURL, param string) string {
	idx := fmt.Sprintf("%s=", param)
	start := 0
	for i := 0; i < len(rawURL)-len(idx)+1; i++ {
		if rawURL[i:i+len(idx)] == idx {
			start = i + len(idx)
			end := start
			for end < len(rawURL) && rawURL[end] != '&' && rawURL[end] != '#' {
				end++
			}
			return rawURL[start:end]
		}
	}
	return ""
}
