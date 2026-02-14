package module

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// HTTPMiddleware defines a middleware that can process HTTP requests
type HTTPMiddleware interface {
	Process(next http.Handler) http.Handler
}

// RateLimitMiddleware implements a rate limiting middleware
type RateLimitMiddleware struct {
	name              string
	requestsPerMinute int
	burstSize         int
	clients           map[string]*client
	mu                sync.Mutex
}

// client tracks the rate limiting state for a single client
type client struct {
	tokens        int
	lastTimestamp time.Time
}

// NewRateLimitMiddleware creates a new rate limiting middleware
func NewRateLimitMiddleware(name string, requestsPerMinute, burstSize int) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		name:              name,
		requestsPerMinute: requestsPerMinute,
		burstSize:         burstSize,
		clients:           make(map[string]*client),
	}
}

// Name returns the module name
func (m *RateLimitMiddleware) Name() string {
	return m.name
}

// Init initializes the middleware
func (m *RateLimitMiddleware) Init(app modular.Application) error {
	return nil
}

// Process implements middleware processing
func (m *RateLimitMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip port from RemoteAddr to identify by IP only
		clientIP := r.RemoteAddr
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}

		m.mu.Lock()
		c, exists := m.clients[clientIP]
		if !exists {
			c = &client{tokens: m.burstSize, lastTimestamp: time.Now()}
			m.clients[clientIP] = c
		} else {
			// Refill tokens based on elapsed time
			elapsed := time.Since(c.lastTimestamp).Minutes()
			tokensToAdd := int(elapsed * float64(m.requestsPerMinute))
			if tokensToAdd > 0 {
				c.tokens = min(c.tokens+tokensToAdd, m.burstSize)
				c.lastTimestamp = time.Now()
			}
		}

		// Check if request can proceed
		if c.tokens <= 0 {
			m.mu.Unlock()
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Consume token
		c.tokens--
		m.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

// ProvidesServices returns the services provided by this middleware
func (m *RateLimitMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "HTTP Rate Limiting Middleware",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this middleware
func (m *RateLimitMiddleware) RequiresServices() []modular.ServiceDependency {
	// No dependencies required
	return nil
}

// Start is a no-op for this middleware
func (m *RateLimitMiddleware) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op for this middleware
func (m *RateLimitMiddleware) Stop(ctx context.Context) error {
	return nil
}

// LoggingMiddleware provides request logging
type LoggingMiddleware struct {
	name     string
	logLevel string
	logger   modular.Logger
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(name string, logLevel string) *LoggingMiddleware {
	return &LoggingMiddleware{
		name:     name,
		logLevel: logLevel,
	}
}

// Name returns the module name
func (m *LoggingMiddleware) Name() string {
	return m.name
}

// Init initializes the middleware
func (m *LoggingMiddleware) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

// Process implements middleware processing
func (m *LoggingMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call the next handler
		next.ServeHTTP(w, r)

		m.logger.Info(fmt.Sprintf("[%s] %s %s %s\n", m.logLevel, r.Method, r.URL.Path, time.Since(start)))
	})
}

// ProvidesServices returns the services provided by this middleware
func (m *LoggingMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "HTTP Logging Middleware",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this middleware
func (m *LoggingMiddleware) RequiresServices() []modular.ServiceDependency {
	// No dependencies required
	return nil
}

// CORSMiddleware provides CORS support
type CORSMiddleware struct {
	name           string
	allowedOrigins []string
	allowedMethods []string
}

// NewCORSMiddleware creates a new CORS middleware
func NewCORSMiddleware(name string, allowedOrigins, allowedMethods []string) *CORSMiddleware {
	return &CORSMiddleware{
		name:           name,
		allowedOrigins: allowedOrigins,
		allowedMethods: allowedMethods,
	}
}

// Name returns the module name
func (m *CORSMiddleware) Name() string {
	return m.name
}

// Init initializes the middleware
func (m *CORSMiddleware) Init(app modular.Application) error {
	return nil
}

// Process implements middleware processing
func (m *CORSMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range m.allowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.allowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ProvidesServices returns the services provided by this middleware
func (m *CORSMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "HTTP CORS Middleware",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this middleware
func (m *CORSMiddleware) RequiresServices() []modular.ServiceDependency {
	// No dependencies required
	return nil
}

// Start is a no-op for this middleware
func (m *CORSMiddleware) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op for this middleware
func (m *CORSMiddleware) Stop(ctx context.Context) error {
	return nil
}
