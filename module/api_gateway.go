package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// GatewayRoute defines a single route in the API gateway.
type GatewayRoute struct {
	PathPrefix  string           `json:"pathPrefix"`
	Backend     string           `json:"backend"`
	StripPrefix bool             `json:"stripPrefix"`
	Methods     []string         `json:"methods"`
	RateLimit   *RateLimitConfig `json:"rateLimit,omitempty"`
	Auth        bool             `json:"auth"`
	Timeout     string           `json:"timeout"`
}

// RateLimitConfig defines rate limiting parameters.
type RateLimitConfig struct {
	RequestsPerMinute int `json:"requestsPerMinute"`
	BurstSize         int `json:"burstSize"`
}

// CORSConfig defines CORS settings for the gateway.
type CORSConfig struct {
	AllowOrigins []string `json:"allowOrigins"`
	AllowMethods []string `json:"allowMethods"`
	AllowHeaders []string `json:"allowHeaders"`
	MaxAge       int      `json:"maxAge"`
}

// AuthConfig defines authentication settings for the gateway.
type AuthConfig struct {
	Type   string `json:"type"`   // "bearer", "api_key", "basic"
	Header string `json:"header"` // header name to check
}

// APIGateway is a composable gateway module that combines routing, auth,
// rate limiting, and proxying into a single module.
type APIGateway struct {
	name   string
	routes []GatewayRoute
	cors   *CORSConfig
	auth   *AuthConfig

	// internal state
	sortedRoutes  []GatewayRoute // sorted by prefix length (longest first)
	proxies       map[string]*httputil.ReverseProxy
	rateLimiters  map[string]*gatewayRateLimiter // keyed by path prefix
	globalLimiter *gatewayRateLimiter
}

// gatewayRateLimiter is a simple per-client token bucket limiter for the gateway.
type gatewayRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rpm     int
	burst   int
}

func newGatewayRateLimiter(rpm, burst int) *gatewayRateLimiter {
	return &gatewayRateLimiter{
		buckets: make(map[string]*tokenBucket),
		rpm:     rpm,
		burst:   burst,
	}
}

func (rl *gatewayRateLimiter) allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[clientIP]
	if !exists {
		refillRate := float64(rl.rpm) / 60.0
		bucket = newTokenBucket(float64(rl.burst), refillRate)
		rl.buckets[clientIP] = bucket
	}
	return bucket.allow()
}

// NewAPIGateway creates a new APIGateway module.
func NewAPIGateway(name string) *APIGateway {
	return &APIGateway{
		name:         name,
		proxies:      make(map[string]*httputil.ReverseProxy),
		rateLimiters: make(map[string]*gatewayRateLimiter),
	}
}

// SetRoutes configures the gateway routes.
func (g *APIGateway) SetRoutes(routes []GatewayRoute) error {
	g.routes = routes

	// Sort routes by prefix length descending for correct matching
	sorted := make([]GatewayRoute, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].PathPrefix) > len(sorted[j].PathPrefix)
	})
	g.sortedRoutes = sorted

	// Build reverse proxies and per-route rate limiters
	for _, route := range routes {
		backend, err := url.Parse(route.Backend)
		if err != nil {
			return fmt.Errorf("api_gateway %q: invalid backend URL %q for prefix %q: %w",
				g.name, route.Backend, route.PathPrefix, err)
		}

		rp := httputil.NewSingleHostReverseProxy(backend)
		backendHost := backend.Host
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "backend unavailable",
				"backend": backendHost,
				"path":    r.URL.Path,
			})
		}
		g.proxies[route.PathPrefix] = rp

		if route.RateLimit != nil && route.RateLimit.RequestsPerMinute > 0 {
			burst := route.RateLimit.BurstSize
			if burst <= 0 {
				burst = route.RateLimit.RequestsPerMinute
			}
			g.rateLimiters[route.PathPrefix] = newGatewayRateLimiter(
				route.RateLimit.RequestsPerMinute, burst,
			)
		}
	}

	return nil
}

// SetGlobalRateLimit configures a global rate limit applied to all routes.
func (g *APIGateway) SetGlobalRateLimit(cfg *RateLimitConfig) {
	if cfg != nil && cfg.RequestsPerMinute > 0 {
		burst := cfg.BurstSize
		if burst <= 0 {
			burst = cfg.RequestsPerMinute
		}
		g.globalLimiter = newGatewayRateLimiter(cfg.RequestsPerMinute, burst)
	}
}

// SetCORS configures CORS settings.
func (g *APIGateway) SetCORS(cfg *CORSConfig) {
	g.cors = cfg
}

// SetAuth configures authentication settings.
func (g *APIGateway) SetAuth(cfg *AuthConfig) {
	g.auth = cfg
}

// Name returns the module name.
func (g *APIGateway) Name() string { return g.name }

// Init initializes the module.
func (g *APIGateway) Init(_ modular.Application) error { return nil }

// Start is a no-op.
func (g *APIGateway) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (g *APIGateway) Stop(_ context.Context) error { return nil }

// ProvidesServices returns the services provided by this module.
func (g *APIGateway) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        g.name,
			Description: "API Gateway",
			Instance:    g,
		},
	}
}

// RequiresServices returns no dependencies.
func (g *APIGateway) RequiresServices() []modular.ServiceDependency { return nil }

// Handle processes incoming HTTP requests through the gateway pipeline:
// CORS -> rate limiting -> auth -> method check -> proxy.
func (g *APIGateway) Handle(w http.ResponseWriter, r *http.Request) {
	// Apply CORS headers
	if g.cors != nil {
		g.applyCORS(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// Find matching route
	route, proxy := g.matchRoute(r.URL.Path)
	if route == nil {
		http.Error(w, `{"error":"no route matched"}`, http.StatusNotFound)
		return
	}

	clientIP := extractClientIP(r)

	// Global rate limiting
	if g.globalLimiter != nil {
		if !g.globalLimiter.allow(clientIP) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
			})
			return
		}
	}

	// Per-route rate limiting
	if rl, ok := g.rateLimiters[route.PathPrefix]; ok {
		if !rl.allow(clientIP) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
			})
			return
		}
	}

	// Auth check
	if route.Auth && g.auth != nil {
		if !g.checkAuth(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized",
			})
			return
		}
	}

	// Method check
	if len(route.Methods) > 0 && !g.methodAllowed(r.Method, route.Methods) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed",
		})
		return
	}

	// Strip prefix if configured
	if route.StripPrefix {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, route.PathPrefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
	}

	// Proxy to backend
	proxy.ServeHTTP(w, r) //nolint:gosec // G704: reverse proxy to configured backend
}

// matchRoute finds the first route matching the request path.
func (g *APIGateway) matchRoute(path string) (*GatewayRoute, *httputil.ReverseProxy) {
	for i, route := range g.sortedRoutes {
		if strings.HasPrefix(path, route.PathPrefix) {
			proxy := g.proxies[route.PathPrefix]
			return &g.sortedRoutes[i], proxy
		}
	}
	return nil, nil
}

// applyCORS sets CORS headers on the response.
func (g *APIGateway) applyCORS(w http.ResponseWriter, _ *http.Request) {
	if len(g.cors.AllowOrigins) > 0 {
		w.Header().Set("Access-Control-Allow-Origin", strings.Join(g.cors.AllowOrigins, ", "))
	}
	if len(g.cors.AllowMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(g.cors.AllowMethods, ", "))
	}
	if len(g.cors.AllowHeaders) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(g.cors.AllowHeaders, ", "))
	}
	if g.cors.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", g.cors.MaxAge))
	}
}

// checkAuth validates the authentication header.
func (g *APIGateway) checkAuth(r *http.Request) bool {
	header := g.auth.Header
	if header == "" {
		header = "Authorization"
	}
	val := r.Header.Get(header)
	if val == "" {
		return false
	}

	switch g.auth.Type {
	case "bearer":
		return strings.HasPrefix(val, "Bearer ")
	case "api_key":
		return len(val) > 0
	case "basic":
		return strings.HasPrefix(val, "Basic ")
	default:
		return len(val) > 0
	}
}

// methodAllowed checks if a method is in the allowed list.
func (g *APIGateway) methodAllowed(method string, allowed []string) bool {
	for _, m := range allowed {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

// extractClientIP gets the client IP from the request for rate limiting.
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Strip port from RemoteAddr
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// Routes returns the configured routes (for introspection/testing).
func (g *APIGateway) Routes() []GatewayRoute {
	return g.routes
}

// GatewayTimeout returns the parsed timeout for a route, or the default.
func GatewayTimeout(route *GatewayRoute, defaultTimeout time.Duration) time.Duration {
	if route.Timeout != "" {
		if d, err := time.ParseDuration(route.Timeout); err == nil {
			return d
		}
	}
	return defaultTimeout
}
