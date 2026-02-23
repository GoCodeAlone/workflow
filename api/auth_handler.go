package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	users      store.UserStore
	sessions   store.SessionStore
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(users store.UserStore, sessions store.SessionStore, secret []byte, issuer string, accessTTL, refreshTTL time.Duration) *AuthHandler {
	if accessTTL == 0 {
		accessTTL = 24 * time.Hour
	}
	if refreshTTL == 0 {
		refreshTTL = 7 * 24 * time.Hour
	}
	return &AuthHandler{
		users:      users,
		sessions:   sessions,
		secret:     secret,
		issuer:     issuer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"` //nolint:gosec // G117: request DTO field
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		WriteError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	now := time.Now()
	user := &store.User{
		ID:           uuid.New(),
		Email:        req.Email,
		PasswordHash: string(hash),
		DisplayName:  req.DisplayName,
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.users.Create(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "email already registered")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tokenPair, err := h.generateTokenPair(user.ID, user.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, tokenPair)
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"` //nolint:gosec // G117: request DTO field
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.Active {
		WriteError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Update last login
	now := time.Now()
	user.LastLoginAt = &now
	user.UpdatedAt = now
	_ = h.users.Update(r.Context(), user)

	tokenPair, err := h.generateTokenPair(user.ID, user.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusOK, tokenPair)
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"` //nolint:gosec // G117: request DTO field
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token, err := jwt.Parse(req.RefreshToken, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return h.secret, nil
	})
	if err != nil || !token.Valid {
		WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	sub, _ := claims["sub"].(string)
	userID, err := uuid.Parse(sub)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	user, err := h.users.Get(r.Context(), userID)
	if err != nil || !user.Active {
		WriteError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	tokenPair, err := h.generateTokenPair(user.ID, user.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusOK, tokenPair)
}

// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Invalidate all active sessions for the user.
	active := true
	sessions, _ := h.sessions.List(r.Context(), store.SessionFilter{
		UserID: &user.ID,
		Active: &active,
	})
	for _, s := range sessions {
		s.Active = false
		_ = h.sessions.Update(r.Context(), s)
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// Me handles GET /api/v1/auth/me.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	WriteJSON(w, http.StatusOK, user)
}

// UpdateMe handles PUT /api/v1/auth/me.
func (h *AuthHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		DisplayName *string `json:"display_name"`
		AvatarURL   *string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.AvatarURL != nil {
		user.AvatarURL = *req.AvatarURL
	}
	user.UpdatedAt = time.Now()

	if err := h.users.Update(r.Context(), user); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, user)
}

// tokenResponse is the JSON shape returned to callers.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`  //nolint:gosec // G117: token response field
	RefreshToken string `json:"refresh_token"` //nolint:gosec // G117: token response field
	ExpiresIn    int64  `json:"expires_in"`
}

func (h *AuthHandler) generateTokenPair(userID uuid.UUID, email string) (*tokenResponse, error) {
	now := time.Now()

	accessClaims := jwt.MapClaims{
		"sub":   userID.String(),
		"email": email,
		"type":  "access",
		"iat":   now.Unix(),
		"exp":   now.Add(h.accessTTL).Unix(),
	}
	if h.issuer != "" {
		accessClaims["iss"] = h.issuer
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(h.secret)
	if err != nil {
		return nil, err
	}

	refreshClaims := jwt.MapClaims{
		"sub":  userID.String(),
		"type": "refresh",
		"iat":  now.Unix(),
		"exp":  now.Add(h.refreshTTL).Unix(),
	}
	if h.issuer != "" {
		refreshClaims["iss"] = h.issuer
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(h.secret)
	if err != nil {
		return nil, err
	}

	return &tokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(h.accessTTL.Seconds()),
	}, nil
}
