package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
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

// ipLimiter holds a per-IP token bucket and the last time it was accessed.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiterStore holds per-IP limiters for a single endpoint group.
type rateLimiterStore struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	r        rate.Limit
	b        int
}

func newRateLimiterStore(requestsPerMinute int) *rateLimiterStore {
	s := &rateLimiterStore{
		limiters: make(map[string]*ipLimiter),
		r:        rate.Limit(float64(requestsPerMinute) / 60.0),
		b:        requestsPerMinute,
	}
	go s.cleanup()
	return s
}

// cleanup periodically removes stale entries.
func (s *rateLimiterStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for ip, l := range s.limiters {
			if time.Since(l.lastSeen) > 10*time.Minute {
				delete(s.limiters, ip)
			}
		}
		s.mu.Unlock()
	}
}

func (s *rateLimiterStore) get(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[ip]
	if !ok {
		l = &ipLimiter{limiter: rate.NewLimiter(s.r, s.b)}
		s.limiters[ip] = l
	}
	l.lastSeen = time.Now()
	return l.limiter
}

// RateLimit returns middleware that limits requests per IP to requestsPerMinute.
// When requestsPerMinute is zero, the default of 10 is used.
// Requests that exceed the limit receive HTTP 429 with a Retry-After header.
func (m *Middleware) RateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 10
	}
	limiters := newRateLimiterStore(requestsPerMinute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			limiter := limiters.get(ip)
			if !limiter.Allow() {
				retryAfter := int(time.Minute.Seconds() / float64(requestsPerMinute))
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realIP extracts the client IP from common proxy headers or RemoteAddr.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// Take the first address in the list.
		if idx := strings.Index(fwd, ","); idx != -1 {
			return strings.TrimSpace(fwd[:idx])
		}
		return strings.TrimSpace(fwd)
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
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
