package module

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

// OAuth2ProviderConfig holds configuration for a single OAuth2 provider.
type OAuth2ProviderConfig struct {
	Name         string   `json:"name"         yaml:"name"`
	ClientID     string   `json:"clientId"     yaml:"clientId"`
	ClientSecret string   `json:"clientSecret" yaml:"clientSecret"` //nolint:gosec // G117: config DTO field for OAuth2 client secret
	AuthURL      string   `json:"authUrl"      yaml:"authUrl"`
	TokenURL     string   `json:"tokenUrl"     yaml:"tokenUrl"`
	UserInfoURL  string   `json:"userInfoUrl"  yaml:"userInfoUrl"`
	Scopes       []string `json:"scopes"       yaml:"scopes"`
	RedirectURL  string   `json:"redirectUrl"  yaml:"redirectUrl"`
}

// oauth2StateEntry holds a CSRF state token with its expiry time.
type oauth2StateEntry struct {
	expiry time.Time
}

// OAuth2Module implements the OAuth2 authorization code flow for multiple providers.
type OAuth2Module struct {
	name      string
	jwtAuth   *JWTAuthModule
	providers map[string]*oauth2ProviderEntry

	mu     sync.Mutex
	states map[string]oauth2StateEntry // CSRF state → expiry
}

type oauth2ProviderEntry struct {
	cfg         *oauth2.Config
	userInfoURL string
}

// NewOAuth2Module creates a new OAuth2Module.
// The jwtAuth parameter is used to issue JWT tokens after successful OAuth2 login.
func NewOAuth2Module(name string, providerCfgs []OAuth2ProviderConfig, jwtAuth *JWTAuthModule) *OAuth2Module {
	m := &OAuth2Module{
		name:      name,
		jwtAuth:   jwtAuth,
		providers: make(map[string]*oauth2ProviderEntry),
		states:    make(map[string]oauth2StateEntry),
	}

	for i := range providerCfgs {
		pc := &providerCfgs[i]
		entry := buildProviderEntry(pc)
		if entry != nil {
			m.providers[pc.Name] = entry
		}
	}

	return m
}

// buildProviderEntry converts an OAuth2ProviderConfig into an oauth2ProviderEntry,
// applying well-known defaults for "google" and "github".
func buildProviderEntry(pc *OAuth2ProviderConfig) *oauth2ProviderEntry {
	if pc.ClientID == "" || pc.ClientSecret == "" {
		return nil
	}

	var endpoint oauth2.Endpoint
	var userInfoURL string

	switch pc.Name {
	case "google":
		endpoint = google.Endpoint
		userInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
		if len(pc.Scopes) == 0 {
			pc.Scopes = []string{"openid", "email", "profile"}
		}
	case "github":
		endpoint = github.Endpoint
		userInfoURL = "https://api.github.com/user"
		if len(pc.Scopes) == 0 {
			pc.Scopes = []string{"user:email"}
		}
	default:
		if pc.AuthURL == "" || pc.TokenURL == "" {
			return nil
		}
		endpoint = oauth2.Endpoint{
			AuthURL:  pc.AuthURL,
			TokenURL: pc.TokenURL,
		}
		userInfoURL = pc.UserInfoURL
	}

	// Allow config to override well-known URLs
	if pc.AuthURL != "" {
		endpoint.AuthURL = pc.AuthURL
	}
	if pc.TokenURL != "" {
		endpoint.TokenURL = pc.TokenURL
	}
	if pc.UserInfoURL != "" {
		userInfoURL = pc.UserInfoURL
	}

	cfg := &oauth2.Config{
		ClientID:     pc.ClientID,
		ClientSecret: pc.ClientSecret,
		RedirectURL:  pc.RedirectURL,
		Scopes:       pc.Scopes,
		Endpoint:     endpoint,
	}

	return &oauth2ProviderEntry{
		cfg:         cfg,
		userInfoURL: userInfoURL,
	}
}

// SetJWTAuth sets the JWTAuthModule used to issue tokens after a successful
// OAuth2 login.  This is called by the plugin's wiring hook.
func (m *OAuth2Module) SetJWTAuth(j *JWTAuthModule) {
	m.jwtAuth = j
}

// Name returns the module name.
func (m *OAuth2Module) Name() string { return m.name }

// Init is a no-op; dependencies are injected via NewOAuth2Module.
func (m *OAuth2Module) Init(_ modular.Application) error { return nil }

// ProvidesServices returns the services provided by this module.
func (m *OAuth2Module) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "OAuth2 authorization code flow module",
			Instance:    m,
		},
	}
}

// RequiresServices returns an empty list (dependencies injected directly).
func (m *OAuth2Module) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Handle routes OAuth2 requests.
// Routes handled:
//
//	GET /auth/oauth2/{provider}/login    — redirect to provider
//	GET /auth/oauth2/{provider}/callback — exchange code, issue JWT
func (m *OAuth2Module) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path

	switch {
	case r.Method == http.MethodGet && strings.Contains(path, "/auth/oauth2/") && strings.HasSuffix(path, "/login"):
		provider := m.extractProviderFromPath(path, "/login")
		m.handleLogin(w, r, provider)
	case r.Method == http.MethodGet && strings.Contains(path, "/auth/oauth2/") && strings.HasSuffix(path, "/callback"):
		provider := m.extractProviderFromPath(path, "/callback")
		m.handleCallback(w, r, provider)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

// handleLogin generates a CSRF state token and redirects to the provider's
// authorization URL.
func (m *OAuth2Module) handleLogin(w http.ResponseWriter, r *http.Request, providerName string) {
	entry, ok := m.providers[providerName]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown provider: " + providerName})
		return
	}

	state, err := generateState()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate state"})
		return
	}

	m.storeState(state)

	// Set state in a short-lived cookie for the callback to read.
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth2_state",
		Value:    state,
		Path:     "/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := entry.cfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback validates the CSRF state, exchanges the authorization code for
// a token, fetches the user's profile, creates or looks up the user in the local
// user store, and returns a signed JWT.
func (m *OAuth2Module) handleCallback(w http.ResponseWriter, r *http.Request, providerName string) {
	entry, ok := m.providers[providerName]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown provider: " + providerName})
		return
	}

	// Validate CSRF state.
	stateParam := r.URL.Query().Get("state")
	if !m.validateAndConsumeState(stateParam) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired state parameter"})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing authorization code"})
		return
	}

	// Exchange code for access token.
	token, err := entry.cfg.Exchange(r.Context(), code)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "token exchange failed: " + err.Error()})
		return
	}

	// Fetch user profile from provider.
	userInfo, err := m.fetchUserInfo(r.Context(), entry, token.AccessToken)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch user info: " + err.Error()})
		return
	}

	// Look up or create local user.
	user, err := m.findOrCreateUser(userInfo, providerName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to provision user: " + err.Error()})
		return
	}

	// Issue JWT using the wired JWTAuthModule.
	if m.jwtAuth == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "jwt auth module not configured"})
		return
	}

	jwtToken, err := m.jwtAuth.generateToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"token": jwtToken,
		"user":  m.jwtAuth.buildUserResponse(user),
	})
}

// fetchUserInfo calls the provider's userinfo endpoint and returns a normalised
// map of user attributes.
func (m *OAuth2Module) fetchUserInfo(ctx context.Context, entry *oauth2ProviderEntry, accessToken string) (map[string]any, error) {
	if entry.userInfoURL == "" {
		return nil, fmt.Errorf("no userinfo URL configured for provider")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	return info, nil
}

// findOrCreateUser looks up or creates a local user from the OAuth2 provider
// profile data.  It delegates to the JWTAuthModule's in-memory user store.
func (m *OAuth2Module) findOrCreateUser(info map[string]any, providerName string) (*User, error) {
	if m.jwtAuth == nil {
		return nil, fmt.Errorf("jwt auth module not configured")
	}

	oauthID := extractString(info, "id", "sub")
	email := extractString(info, "email")
	name := extractString(info, "name", "login")

	if oauthID == "" {
		return nil, fmt.Errorf("provider did not return a user id")
	}
	if email == "" {
		// Use a synthetic email for providers that don't expose it (e.g. GitHub).
		email = fmt.Sprintf("%s@%s.oauth", oauthID, providerName)
	}

	// Check if the user already exists in the JWTAuthModule's store.
	oauthKey := fmt.Sprintf("oauth:%s:%s", providerName, oauthID)
	if existing, ok := m.jwtAuth.lookupUser(oauthKey); ok {
		return existing, nil
	}
	// Also try lookup by email.
	if existing, ok := m.jwtAuth.lookupUser(email); ok {
		return existing, nil
	}

	// Create a new user with no password (OAuth2 users authenticate via provider).
	meta := map[string]any{
		"role":          "user",
		"oauthProvider": providerName,
		"oauthId":       oauthID,
	}

	var newUser *User
	if m.jwtAuth.userStore != nil {
		var err error
		newUser, err = m.jwtAuth.userStore.CreateUser(oauthKey, name, "", meta)
		if err != nil {
			// If the key already exists (race), fall back to lookup.
			if u, ok := m.jwtAuth.userStore.GetUser(oauthKey); ok {
				return u, nil
			}
			return nil, err
		}
	} else {
		m.jwtAuth.mu.Lock()
		newUser = &User{
			ID:       fmt.Sprintf("%d", m.jwtAuth.nextID),
			Email:    oauthKey,
			Name:     name,
			Metadata: meta,
			CreatedAt: time.Now(),
		}
		m.jwtAuth.nextID++
		m.jwtAuth.users[oauthKey] = newUser
		// Also index by email so lookups work both ways.
		if _, exists := m.jwtAuth.users[email]; !exists && email != oauthKey {
			m.jwtAuth.users[email] = newUser
		}
		m.jwtAuth.mu.Unlock()
	}

	return newUser, nil
}

// --- CSRF state helpers ---

// generateState produces a cryptographically random base64-URL-encoded token.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const stateTTL = 10 * time.Minute

func (m *OAuth2Module) storeState(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpiredStates()
	m.states[state] = oauth2StateEntry{expiry: time.Now().Add(stateTTL)}
}

func (m *OAuth2Module) validateAndConsumeState(state string) bool {
	if state == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.states[state]
	if !ok {
		return false
	}
	delete(m.states, state)
	return time.Now().Before(entry.expiry)
}

// purgeExpiredStates removes expired state entries; must be called under m.mu.
func (m *OAuth2Module) purgeExpiredStates() {
	now := time.Now()
	for k, v := range m.states {
		if now.After(v.expiry) {
			delete(m.states, k)
		}
	}
}

// --- URL parsing helpers ---

// extractProviderFromPath extracts the provider name from a path such as
// /auth/oauth2/{provider}/login.
func (m *OAuth2Module) extractProviderFromPath(path, suffix string) string {
	path = strings.TrimSuffix(path, suffix)
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return ""
	}
	return path[idx+1:]
}

// extractString returns the first non-empty string value found for any of the
// given keys in the map.
func extractString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch s := v.(type) {
			case string:
				if s != "" {
					return s
				}
			case float64:
				return fmt.Sprintf("%.0f", s)
			}
		}
	}
	return ""
}
