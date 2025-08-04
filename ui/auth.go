package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AuthService handles authentication and JWT token management
type AuthService struct {
	secretKey   []byte
	dbService   *DatabaseService
	tokenExpiry time.Duration
}

// Claims represents JWT claims for the application
type Claims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// NewAuthService creates a new authentication service
func NewAuthService(secretKey string, dbService *DatabaseService) *AuthService {
	return &AuthService{
		secretKey:   []byte(secretKey),
		dbService:   dbService,
		tokenExpiry: 24 * time.Hour, // Default 24 hour expiry
	}
}

// Login authenticates a user and returns a JWT token
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	user, tenant, err := s.dbService.AuthenticateUser(ctx, req.Username, req.Password)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Generate JWT token
	expiresAt := time.Now().Add(s.tokenExpiry)
	claims := &Claims{
		UserID:   user.ID.String(),
		TenantID: user.TenantID.String(),
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "workflow-ui",
			Subject:   user.ID.String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	return &LoginResponse{
		Token:     tokenString,
		User:      *user,
		Tenant:    *tenant,
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateToken validates a JWT token and returns the claims
func (s *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token claims")
}

// AuthContext represents the authentication context for a request
type AuthContext struct {
	UserID   uuid.UUID
	TenantID uuid.UUID
	Username string
	Role     string
}

// GetAuthContext extracts authentication context from claims
func GetAuthContext(claims *Claims) (*AuthContext, error) {
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	tenantID, err := uuid.Parse(claims.TenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant ID: %w", err)
	}

	return &AuthContext{
		UserID:   userID,
		TenantID: tenantID,
		Username: claims.Username,
		Role:     claims.Role,
	}, nil
}

// HasRole checks if the user has the specified role
func (ac *AuthContext) HasRole(role string) bool {
	return ac.Role == role
}

// IsAdmin checks if the user is an admin
func (ac *AuthContext) IsAdmin() bool {
	return ac.Role == "admin"
}

// CanAccessTenant checks if the user can access the specified tenant
func (ac *AuthContext) CanAccessTenant(tenantID uuid.UUID) bool {
	return ac.TenantID == tenantID || ac.IsAdmin()
}