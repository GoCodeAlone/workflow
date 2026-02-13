package module

import (
	"context"
	"encoding/json"
	"fmt"
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
	ID           string                 `json:"id"`
	Email        string                 `json:"email"`
	Name         string                 `json:"name"`
	PasswordHash string                 `json:"-"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"createdAt"`
}

// JWTAuthModule handles JWT authentication with an in-memory user store
type JWTAuthModule struct {
	name        string
	secret      string
	tokenExpiry time.Duration
	issuer      string
	seedFile    string
	users       map[string]*User // keyed by email
	mu          sync.RWMutex
	nextID      int
	app         modular.Application
	persistence *PersistenceStore // optional write-through backend
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
	var ps interface{}
	if err := app.GetService("persistence", &ps); err == nil && ps != nil {
		if store, ok := ps.(*PersistenceStore); ok {
			j.persistence = store
		}
	}

	return nil
}

// Authenticate implements AuthProvider
func (j *JWTAuthModule) Authenticate(tokenStr string) (bool, map[string]interface{}, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(j.secret), nil
	})
	if err != nil {
		return false, nil, nil
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return false, nil, nil
	}

	result := make(map[string]interface{})
	for k, v := range claims {
		result[k] = v
	}
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
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/auth/profile"):
		j.handleGetProfile(w, r)
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/auth/profile"):
		j.handleUpdateProfile(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
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
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "email and password are required"})
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// Check for duplicate email
	if _, exists := j.users[req.Email]; exists {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "email already registered"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to hash password"})
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
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user":  user,
	})
}

func (j *JWTAuthModule) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	j.mu.RLock()
	user, exists := j.users[req.Email]
	j.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := j.generateToken(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate token"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user":  j.buildUserResponse(user),
	})
}

func (j *JWTAuthModule) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	user, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(j.buildUserResponse(user))
}

func (j *JWTAuthModule) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, err := j.extractUserFromRequest(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
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

	json.NewEncoder(w).Encode(j.buildUserResponse(user))
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

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
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

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.secret))
}

// buildUserResponse creates a response map that flattens metadata fields to the
// top level, so the SPA receives role, affiliateId, programIds, etc. directly.
func (j *JWTAuthModule) buildUserResponse(user *User) map[string]interface{} {
	resp := map[string]interface{}{
		"id":        user.ID,
		"email":     user.Email,
		"name":      user.Name,
		"createdAt": user.CreatedAt,
	}
	for k, v := range user.Metadata {
		resp[k] = v
	}
	return resp
}

// loadSeedUsers reads a JSON seed file and registers users that don't already exist.
// The seed format matches the api.handler seed format: [{id, data: {email, name, password, role, ...}}]
func (j *JWTAuthModule) loadSeedUsers(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read seed file %s: %w", path, err)
	}

	var seeds []struct {
		ID   string                 `json:"id"`
		Data map[string]interface{} `json:"data"`
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
		metadata := make(map[string]interface{})
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
		var ps interface{}
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
