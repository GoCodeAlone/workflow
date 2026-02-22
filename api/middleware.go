package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Middleware holds dependencies needed by authentication middleware.
type Middleware struct {
	jwtSecret   []byte
	users       store.UserStore
	permissions *PermissionService
}

// NewMiddleware creates a new Middleware.
func NewMiddleware(jwtSecret []byte, users store.UserStore, permissions *PermissionService) *Middleware {
	return &Middleware{
		jwtSecret:   jwtSecret,
		users:       users,
		permissions: permissions,
	}
}

// RequireAuth validates the JWT Bearer token and loads the user into context.
// Returns 401 if the token is missing, invalid, or the user cannot be found.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := m.authenticate(r)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := SetUserContext(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth is like RequireAuth but does not fail when no token is present.
func (m *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := m.authenticate(r)
		if user != nil {
			ctx := SetUserContext(r.Context(), user)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole returns middleware that checks the authenticated user has at least
// minRole on the resource identified by resourceType and the path parameter idKey.
func (m *Middleware) RequireRole(minRole store.Role, resourceType, idKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			resourceIDStr := r.PathValue(idKey)
			resourceID, err := uuid.Parse(resourceIDStr)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid resource id")
				return
			}
			if !m.permissions.CanAccess(r.Context(), user.ID, resourceType, resourceID, minRole) {
				WriteError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// authenticate extracts the Bearer token, validates it, and loads the user.
func (m *Middleware) authenticate(r *http.Request) (*store.User, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, jwt.ErrTokenMalformed
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, jwt.ErrTokenMalformed
	}
	tokenStr := parts[1]

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, jwt.ErrTokenSignatureInvalid
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, jwt.ErrTokenMalformed
	}

	sub, _ := claims["sub"].(string)
	userID, err := uuid.Parse(sub)
	if err != nil {
		return nil, jwt.ErrTokenMalformed
	}

	user, err := m.users.Get(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	if !user.Active {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return user, nil
}
