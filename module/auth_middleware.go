package module

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// Define a custom type for context keys to avoid collisions
type authContextKey string

const authClaimsContextKey authContextKey = "auth_claims"

// AuthMiddleware implements an HTTP authorization middleware
type AuthMiddleware struct {
	name      string
	authType  string // e.g., "Bearer", "Basic", etc.
	providers []AuthProvider
}

// AuthProvider defines methods for authentication providers
type AuthProvider interface {
	Authenticate(token string) (bool, map[string]interface{}, error)
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(name string, authType string) *AuthMiddleware {
	return &AuthMiddleware{
		name:      name,
		authType:  authType,
		providers: make([]AuthProvider, 0),
	}
}

// Name returns the module name
func (m *AuthMiddleware) Name() string {
	return m.name
}

// Init initializes the middleware with the application context
func (m *AuthMiddleware) Init(app modular.Application) error {
	return nil
}

// Process implements the HTTPMiddleware interface
func (m *AuthMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Check for correct auth type
		if !strings.HasPrefix(authHeader, m.authType+" ") {
			http.Error(w, fmt.Sprintf("%s authorization required", m.authType), http.StatusUnauthorized)
			return
		}

		// Extract token
		token := strings.TrimPrefix(authHeader, m.authType+" ")

		// Try to authenticate with each provider
		for _, provider := range m.providers {
			valid, claims, err := provider.Authenticate(token)
			if err != nil {
				// Log error but continue with other providers
				fmt.Printf("Authentication error: %v\n", err)
				continue
			}

			if valid {
				// Store claims in request context
				ctx := context.WithValue(r.Context(), authClaimsContextKey, claims)

				// Call next handler with updated context
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// If we get here, authentication failed
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
	})
}

// RegisterProvider adds an authentication provider
func (m *AuthMiddleware) RegisterProvider(provider AuthProvider) {
	m.providers = append(m.providers, provider)
}

// AddProvider creates and registers a simple token-based auth provider
func (m *AuthMiddleware) AddProvider(validTokens map[string]map[string]interface{}) {
	m.RegisterProvider(&SimpleTokenProvider{
		validTokens: validTokens,
	})
}

// Start is a no-op for this middleware
func (m *AuthMiddleware) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op for this middleware
func (m *AuthMiddleware) Stop(ctx context.Context) error {
	return nil
}

// SimpleTokenProvider implements a simple token-based auth provider
type SimpleTokenProvider struct {
	validTokens map[string]map[string]interface{}
}

// Authenticate checks if the token is valid and returns associated claims
func (p *SimpleTokenProvider) Authenticate(token string) (bool, map[string]interface{}, error) {
	if claims, ok := p.validTokens[token]; ok {
		return true, claims, nil
	}
	return false, nil, nil
}

// ProvidesServices returns the services provided by this module
func (m *AuthMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "HTTP Authentication Middleware",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this module
func (m *AuthMiddleware) RequiresServices() []modular.ServiceDependency {
	// This middleware doesn't require any services
	return []modular.ServiceDependency{}
}
