package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// OAuthProviderConfig describes one OAuth2 provider.
type OAuthProviderConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	UserInfoURL  string   `json:"user_info_url"`
}

// OAuthHandler handles OAuth2 login flows.
type OAuthHandler struct {
	users      store.UserStore
	providers  map[string]*OAuthProviderConfig
	configs    map[string]*oauth2.Config
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewOAuthHandler creates a new OAuthHandler.
func NewOAuthHandler(users store.UserStore, providers map[string]*OAuthProviderConfig, secret []byte, issuer string, accessTTL, refreshTTL time.Duration) *OAuthHandler {
	configs := make(map[string]*oauth2.Config, len(providers))
	for name, p := range providers {
		configs[name] = &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  p.RedirectURL,
			Scopes:       p.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  p.AuthURL,
				TokenURL: p.TokenURL,
			},
		}
	}
	return &OAuthHandler{
		users:      users,
		providers:  providers,
		configs:    configs,
		secret:     secret,
		issuer:     issuer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// Authorize handles GET /api/v1/auth/oauth2/{provider}.
// Generates a state parameter, stores it in a cookie, and redirects.
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	cfg, ok := h.configs[provider]
	if !ok {
		WriteError(w, http.StatusNotFound, "unknown oauth provider")
		return
	}

	state, err := randomHex(16)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := cfg.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// Callback handles GET /api/v1/auth/oauth2/{provider}/callback.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	cfg, ok := h.configs[provider]
	if !ok {
		WriteError(w, http.StatusNotFound, "unknown oauth provider")
		return
	}
	prov, ok := h.providers[provider]
	if !ok {
		WriteError(w, http.StatusNotFound, "unknown oauth provider")
		return
	}

	// Verify state
	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != r.URL.Query().Get("state") {
		WriteError(w, http.StatusBadRequest, "invalid state parameter")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		WriteError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	token, err := cfg.Exchange(context.Background(), code)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "oauth2 code exchange failed")
		return
	}

	// Fetch user info
	userInfo, err := fetchUserInfo(prov.UserInfoURL, token.AccessToken)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to fetch user info")
		return
	}

	email, _ := userInfo["email"].(string)
	oauthID := extractOAuthID(userInfo)
	if email == "" || oauthID == "" {
		WriteError(w, http.StatusBadRequest, "provider did not return email or id")
		return
	}
	email = strings.TrimSpace(strings.ToLower(email))

	oauthProvider := store.OAuthProvider(provider)

	// Find or create user
	user, err := h.users.GetByOAuth(r.Context(), oauthProvider, oauthID)
	if err != nil {
		// Try by email
		user, err = h.users.GetByEmail(r.Context(), email)
		if err != nil {
			// Create new user
			now := time.Now()
			displayName, _ := userInfo["name"].(string)
			avatarURL, _ := userInfo["picture"].(string)
			user = &store.User{
				ID:            uuid.New(),
				Email:         email,
				DisplayName:   displayName,
				AvatarURL:     avatarURL,
				OAuthProvider: oauthProvider,
				OAuthID:       oauthID,
				Active:        true,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if createErr := h.users.Create(r.Context(), user); createErr != nil {
				if errors.Is(createErr, store.ErrDuplicate) {
					// Race condition: another request created the user.
					user, err = h.users.GetByEmail(r.Context(), email)
					if err != nil {
						WriteError(w, http.StatusInternalServerError, "internal error")
						return
					}
				} else {
					WriteError(w, http.StatusInternalServerError, "internal error")
					return
				}
			}
		} else {
			// Link OAuth to existing email-matched user
			user.OAuthProvider = oauthProvider
			user.OAuthID = oauthID
			user.UpdatedAt = time.Now()
			_ = h.users.Update(r.Context(), user)
		}
	}

	// Generate JWT
	ah := &AuthHandler{secret: h.secret, issuer: h.issuer, accessTTL: h.accessTTL, refreshTTL: h.refreshTTL}
	tokenPair, err := ah.generateTokenPair(user.ID, user.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusOK, tokenPair)
}

func fetchUserInfo(url, accessToken string) (map[string]interface{}, error) {
	if url == "" {
		return nil, fmt.Errorf("user info URL not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user info request returned %d: %s", resp.StatusCode, body)
	}
	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return info, nil
}

func extractOAuthID(info map[string]interface{}) string {
	for _, key := range []string{"sub", "id", "user_id"} {
		if v, ok := info[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
