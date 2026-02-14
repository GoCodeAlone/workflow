package module

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// User represents a user in the in-memory store
type User struct {
	ID           string         `json:"id"`
	Email        string         `json:"email"`
	Name         string         `json:"name"`
	PasswordHash string         `json:"-"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}

// JWTAuthModule handles JWT authentication with an in-memory user store
type JWTAuthModule struct {
	name           string
	secret         string
	tokenExpiry    time.Duration
	issuer         string
	seedFile       string
	responseFormat string           // "standard" (default) or "v1" (access_token/refresh_token)
	users          map[string]*User // keyed by email
	mu             sync.RWMutex
	nextID         int
	app            modular.Application
	persistence    *PersistenceStore // optional write-through backend
}

// NewJWTAuthModule creates a new JWT auth module
func NewJWTAuthModule(name, secret string, tokenExpiry time.Duration, issuer string) *JWTAuthModule {
	if tokenExpiry <= 0 {
		tokenExpiry = 24 * time.Hour
	}
	if issuer == "" {
		issuer = "workflow"
	}
	return &JWTAuthModule{
		name:        name,
		secret:      secret,
		tokenExpiry: tokenExpiry,
		issuer:      issuer,
		users:       make(map[string]*User),
		nextID:      1,
	}
}

// SetSeedFile sets the path to a JSON file of seed users to load on start.
func (j *JWTAuthModule) SetSeedFile(path string) {
	j.seedFile = path
}

// SetResponseFormat sets the response format for auth endpoints.
// "v1" returns {access_token, refresh_token, expires_in, user} and adds
// /auth/refresh, /auth/me, /auth/logout handlers.
// "standard" (default) returns {token, user}.
func (j *JWTAuthModule) SetResponseFormat(format string) {
	j.responseFormat = format
}

// Name returns the module name
func (j *JWTAuthModule) Name() string {
	return j.name
}

// Init initializes the module
func (j *JWTAuthModule) Init(app modular.Application) error {
	if j.secret == "" {
		return fmt.Errorf("jwt secret is required")
	}
	j.app = app

	// Wire persistence (optional)
	var ps any
	if err := app.GetService("persistence", &ps); err == nil && ps != nil {
		if store, ok := ps.(*PersistenceStore); ok {
			j.persistence = store
		}
	}

	return nil
}

// Authenticate implements AuthProvider
func (j *JWTAuthModule) Authenticate(tokenStr string) (bool, map[string]any, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(j.secret), nil
	})
	if err != nil {
		return false, nil, nil //nolint:nilerr // Invalid token is a failed auth, not an error
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return false, nil, nil
	}

	result := make(map[string]any)
	maps.Copy(result, claims)
	return true, result, nil
}

// Handle routes auth requests
func (j *JWTAuthModule) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/auth/register"):
		j.handleRegister(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/auth/login"):
		j.handleLogin(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/auth/refresh"):
		j.handleRefresh(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/auth/logout"):
		j.handleLogout(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/auth/me"):
		j.handleGetProfile(w, r)
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/auth/me"):
		j.handleUpdateProfile(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/auth/profile"):
		j.handleGetProfile(w, r)
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/auth/profile"):
		j.handleUpdateProfile(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/auth/setup-status"):
		j.handleSetupStatus(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/auth/setup"):
		j.handleSetup(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/auth/users"):
		j.handleListUsers(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/auth/users"):
		j.handleCreateUser(w, r)
	case r.Method == http.MethodDelete && strings.Contains(path, "/auth/users/"):
		j.handleDeleteUser(w, r)
	case r.Method == http.MethodPut && strings.Contains(path, "/auth/users/") && strings.HasSuffix(path, "/role"):
		j.handleUpdateUserRole(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

func (j *JWTAuthModule) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email and password are required"})
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Check for duplicate email
	if _, exists := j.users[req.Email]; exists {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email already registered"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
		return
	}

	user := &User{
		ID:           fmt.Sprintf("%d", j.nextID),
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}
	j.nextID++
	j.users[req.Email] = user

	// Write-through to persistence
	if j.persistence != nil {
		_ = j.persistence.SaveUser(UserRecord{
			ID:           user.ID,
			Email:        user.Email,
			Name:         user.Name,
			PasswordHash: user.PasswordHash,
			Metadata:     user.Metadata,
			CreatedAt:    user.CreatedAt,
		})
	}

	token, err := j.generateToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	if j.responseFormat == "v1" {
		refreshToken, err := j.generateRefreshToken(user)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate refresh token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  token,
			"refresh_token": refreshToken,
			"expires_in":    int(j.tokenExpiry.Seconds()),
			"user":          j.buildUserResponse(user),
		})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": token,
			"user":  user,
		})
	}
}

func (j *JWTAuthModule) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	j.mu.RLock()
	user, exists := j.users[req.Email]
	j.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := j.generateToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	if j.responseFormat == "v1" {
		refreshToken, err := j.generateRefreshToken(user)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate refresh token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  token,
			"refresh_token": refreshToken,
			"expires_in":    int(j.tokenExpiry.Seconds()),
			"user":          j.buildUserResponse(user),
		})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": token,
			"user":  j.buildUserResponse(user),
		})
	}
}

func (j *JWTAuthModule) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	user, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(j.buildUserResponse(user))
}

func (j *JWTAuthModule) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	j.mu.Lock()
	if req.Name != "" {
		user.Name = req.Name
	}
	j.mu.Unlock()

	// Write-through to persistence
	if j.persistence != nil {
		_ = j.persistence.SaveUser(UserRecord{
			ID:           user.ID,
			Email:        user.Email,
			Name:         user.Name,
			PasswordHash: user.PasswordHash,
			Metadata:     user.Metadata,
			CreatedAt:    user.CreatedAt,
		})
	}

	_ = json.NewEncoder(w).Encode(j.buildUserResponse(user))
}

func (j *JWTAuthModule) extractUserFromRequest(r *http.Request) (*User, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("authorization header required")
	}

	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return nil, fmt.Errorf("bearer token required")
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(j.secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	email, ok := claims["email"].(string)
	if !ok {
		return nil, fmt.Errorf("email not found in token")
	}

	j.mu.RLock()
	user, exists := j.users[email]
	j.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	return user, nil
}

func (j *JWTAuthModule) generateToken(user *User) (string, error) {
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"email": user.Email,
		"name":  user.Name,
		"iss":   j.issuer,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(j.tokenExpiry).Unix(),
	}

	if role, ok := user.Metadata["role"].(string); ok && role != "" {
		claims["role"] = role
	}
	if affiliateId, ok := user.Metadata["affiliateId"].(string); ok && affiliateId != "" {
		claims["affiliateId"] = affiliateId
	}
	if programIds, ok := user.Metadata["programIds"].([]any); ok && len(programIds) > 0 {
		claims["programIds"] = programIds
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.secret))
}

// buildUserResponse creates a response map that flattens metadata fields to the
// top level, so the SPA receives role, affiliateId, programIds, etc. directly.
func (j *JWTAuthModule) buildUserResponse(user *User) map[string]any {
	resp := map[string]any{
		"id":        user.ID,
		"email":     user.Email,
		"name":      user.Name,
		"createdAt": user.CreatedAt,
	}
	maps.Copy(resp, user.Metadata)
	return resp
}

// generateRefreshToken creates a refresh JWT with longer expiry (7 days) and a "refresh" type claim.
func (j *JWTAuthModule) generateRefreshToken(user *User) (string, error) {
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"email": user.Email,
		"type":  "refresh",
		"iss":   j.issuer,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.secret))
}

// handleRefresh exchanges a refresh token for a new access/refresh token pair.
func (j *JWTAuthModule) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.RefreshToken == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "refresh_token is required"})
		return
	}

	token, err := jwt.Parse(req.RefreshToken, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(j.secret), nil
	})
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid refresh token"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid refresh token claims"})
		return
	}

	// Verify this is a refresh token
	if tokenType, _ := claims["type"].(string); tokenType != "refresh" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not a refresh token"})
		return
	}

	email, ok := claims["email"].(string)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email not found in token"})
		return
	}

	j.mu.RLock()
	user, exists := j.users[email]
	j.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	accessToken, err := j.generateToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	refreshToken, err := j.generateRefreshToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate refresh token"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(j.tokenExpiry.Seconds()),
	})
}

// handleLogout is a no-op that returns 200 OK (JWT tokens are stateless).
func (j *JWTAuthModule) handleLogout(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSetupStatus returns whether the system needs initial setup (no users exist).
func (j *JWTAuthModule) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	j.mu.RLock()
	userCount := len(j.users)
	j.mu.RUnlock()

	// Also check persistence if in-memory is empty
	if userCount == 0 && j.persistence != nil {
		if users, err := j.persistence.LoadUsers(); err == nil {
			userCount = len(users)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"needsSetup": userCount == 0,
		"userCount":  userCount,
	})
}

// handleSetup creates the first admin user. Only works when no users exist.
func (j *JWTAuthModule) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email and password are required"})
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Verify no users exist (in-memory)
	if len(j.users) > 0 {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "setup already completed"})
		return
	}

	// Also verify persistence has no users
	if j.persistence != nil {
		if users, err := j.persistence.LoadUsers(); err == nil && len(users) > 0 {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "setup already completed"})
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
		return
	}

	user := &User{
		ID:           fmt.Sprintf("%d", j.nextID),
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: string(hash),
		Metadata:     map[string]any{"role": "admin"},
		CreatedAt:    time.Now(),
	}
	j.nextID++
	j.users[req.Email] = user

	// Write-through to persistence
	if j.persistence != nil {
		_ = j.persistence.SaveUser(UserRecord{
			ID:           user.ID,
			Email:        user.Email,
			Name:         user.Name,
			PasswordHash: user.PasswordHash,
			Metadata:     user.Metadata,
			CreatedAt:    user.CreatedAt,
		})
	}

	token, err := j.generateToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	if j.responseFormat == "v1" {
		refreshToken, err := j.generateRefreshToken(user)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate refresh token"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  token,
			"refresh_token": refreshToken,
			"expires_in":    int(j.tokenExpiry.Seconds()),
			"user":          j.buildUserResponse(user),
		})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": token,
			"user":  j.buildUserResponse(user),
		})
	}
}

// handleListUsers returns all users. Requires admin role.
func (j *JWTAuthModule) handleListUsers(w http.ResponseWriter, r *http.Request) {
	requestor, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if !j.isAdmin(requestor) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin role required"})
		return
	}

	j.mu.RLock()
	users := make([]map[string]any, 0, len(j.users))
	for _, u := range j.users {
		users = append(users, j.buildUserResponse(u))
	}
	j.mu.RUnlock()

	_ = json.NewEncoder(w).Encode(users)
}

// handleCreateUser creates a new user. Requires admin role.
func (j *JWTAuthModule) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	requestor, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if !j.isAdmin(requestor) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin role required"})
		return
	}

	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email and password are required"})
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if _, exists := j.users[req.Email]; exists {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "email already registered"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
		return
	}

	user := &User{
		ID:           fmt.Sprintf("%d", j.nextID),
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: string(hash),
		Metadata:     map[string]any{"role": req.Role},
		CreatedAt:    time.Now(),
	}
	j.nextID++
	j.users[req.Email] = user

	// Write-through to persistence
	if j.persistence != nil {
		_ = j.persistence.SaveUser(UserRecord{
			ID:           user.ID,
			Email:        user.Email,
			Name:         user.Name,
			PasswordHash: user.PasswordHash,
			Metadata:     user.Metadata,
			CreatedAt:    user.CreatedAt,
		})
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(j.buildUserResponse(user))
}

// handleDeleteUser deletes a user by ID. Requires admin role.
func (j *JWTAuthModule) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	requestor, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if !j.isAdmin(requestor) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin role required"})
		return
	}

	// Extract user ID from URL: .../auth/users/{id}
	userID := j.extractPathParam(r.URL.Path, "/auth/users/")
	if userID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user ID required"})
		return
	}

	// Prevent self-deletion
	if userID == requestor.ID {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot delete yourself"})
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Find the user to delete
	var targetEmail string
	for email, u := range j.users {
		if u.ID == userID {
			targetEmail = email
			break
		}
	}

	if targetEmail == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	// Prevent deleting the last admin
	target := j.users[targetEmail]
	if j.isAdmin(target) && j.countAdmins() <= 1 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot delete the last admin"})
		return
	}

	delete(j.users, targetEmail)

	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateUserRole updates a user's role. Requires admin role.
func (j *JWTAuthModule) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	requestor, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if !j.isAdmin(requestor) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin role required"})
		return
	}

	// Extract user ID from URL: .../auth/users/{id}/role
	path := r.URL.Path
	// Strip trailing /role
	path = strings.TrimSuffix(path, "/role")
	userID := j.extractPathParam(path, "/auth/users/")
	if userID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user ID required"})
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Role == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "role is required"})
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Find the target user
	var target *User
	for _, u := range j.users {
		if u.ID == userID {
			target = u
			break
		}
	}

	if target == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	// Prevent demoting yourself if you're the last admin
	if target.ID == requestor.ID && j.isAdmin(target) && req.Role != "admin" && j.countAdmins() <= 1 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot demote the last admin"})
		return
	}

	if target.Metadata == nil {
		target.Metadata = make(map[string]any)
	}
	target.Metadata["role"] = req.Role

	// Write-through to persistence
	if j.persistence != nil {
		_ = j.persistence.SaveUser(UserRecord{
			ID:           target.ID,
			Email:        target.Email,
			Name:         target.Name,
			PasswordHash: target.PasswordHash,
			Metadata:     target.Metadata,
			CreatedAt:    target.CreatedAt,
		})
	}

	_ = json.NewEncoder(w).Encode(j.buildUserResponse(target))
}

// isAdmin checks if a user has the admin role.
func (j *JWTAuthModule) isAdmin(user *User) bool {
	if user.Metadata == nil {
		return false
	}
	role, ok := user.Metadata["role"].(string)
	return ok && role == "admin"
}

// countAdmins returns the number of admin users.
func (j *JWTAuthModule) countAdmins() int {
	count := 0
	for _, u := range j.users {
		if j.isAdmin(u) {
			count++
		}
	}
	return count
}

// extractPathParam extracts the value after a prefix in a URL path.
// For example, extractPathParam("/api/v1/auth/users/42", "/auth/users/") returns "42".
func (j *JWTAuthModule) extractPathParam(path, prefix string) string {
	idx := strings.Index(path, prefix)
	if idx < 0 {
		return ""
	}
	return path[idx+len(prefix):]
}

// loadSeedUsers reads a JSON seed file and registers users that don't already exist.
// The seed format matches the api.handler seed format: [{id, data: {email, name, password, role, ...}}]
func (j *JWTAuthModule) loadSeedUsers(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read seed file %s: %w", path, err)
	}

	var seeds []struct {
		ID   string         `json:"id"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &seeds); err != nil {
		return fmt.Errorf("failed to parse seed file %s: %w", path, err)
	}

	for _, seed := range seeds {
		email, _ := seed.Data["email"].(string)
		if email == "" {
			continue
		}

		// Skip if already loaded from persistence or memory
		if _, exists := j.users[email]; exists {
			continue
		}

		password, _ := seed.Data["password"].(string)
		if password == "" {
			password = "changeme" // fallback for seed users
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			continue
		}

		name, _ := seed.Data["name"].(string)

		// Build metadata from all non-auth fields
		metadata := make(map[string]any)
		for k, v := range seed.Data {
			switch k {
			case "email", "name", "password":
				// Skip auth fields
			default:
				metadata[k] = v
			}
		}

		user := &User{
			ID:           seed.ID,
			Email:        email,
			Name:         name,
			PasswordHash: string(hash),
			Metadata:     metadata,
			CreatedAt:    time.Now(),
		}
		j.users[email] = user

		// Track highest numeric ID
		var idNum int
		if _, err := fmt.Sscanf(seed.ID, "%d", &idNum); err == nil && idNum >= j.nextID {
			j.nextID = idNum + 1
		}

		// Write-through to persistence
		if j.persistence != nil {
			_ = j.persistence.SaveUser(UserRecord{
				ID:           user.ID,
				Email:        user.Email,
				Name:         user.Name,
				PasswordHash: user.PasswordHash,
				Metadata:     user.Metadata,
				CreatedAt:    user.CreatedAt,
			})
		}
	}

	return nil
}

// Start loads persisted users if available, then seed users.
func (j *JWTAuthModule) Start(ctx context.Context) error {
	// Late-bind persistence if it wasn't available during Init().
	if j.persistence == nil && j.app != nil {
		var ps any
		if err := j.app.GetService("persistence", &ps); err == nil && ps != nil {
			if store, ok := ps.(*PersistenceStore); ok {
				j.persistence = store
			}
		}
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Load persisted users first (they take priority over seeds)
	if j.persistence != nil {
		users, err := j.persistence.LoadUsers()
		if err == nil {
			for _, u := range users {
				if _, exists := j.users[u.Email]; exists {
					continue
				}
				j.users[u.Email] = &User{
					ID:           u.ID,
					Email:        u.Email,
					Name:         u.Name,
					PasswordHash: u.PasswordHash,
					Metadata:     u.Metadata,
					CreatedAt:    u.CreatedAt,
				}
				var idNum int
				if _, err := fmt.Sscanf(u.ID, "%d", &idNum); err == nil && idNum >= j.nextID {
					j.nextID = idNum + 1
				}
			}
		}
	}

	// Load seed users (skips any already loaded from persistence)
	if j.seedFile != "" {
		if err := j.loadSeedUsers(j.seedFile); err != nil {
			// Non-fatal: log but don't prevent startup
			_ = err
		}
	}

	return nil
}

// Stop is a no-op
func (j *JWTAuthModule) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices returns the services provided by this module
func (j *JWTAuthModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        j.name,
			Description: "JWT Authentication Module",
			Instance:    j,
		},
	}
}

// RequiresServices returns services required by this module
func (j *JWTAuthModule) RequiresServices() []modular.ServiceDependency {
	return []modular.ServiceDependency{
		{
			Name:     "persistence",
			Required: false, // Optional dependency
		},
	}
}
