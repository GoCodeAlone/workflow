package module

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// HTTPMiddleware defines a middleware that can process HTTP requests
type HTTPMiddleware interface {
	Process(next http.Handler) http.Handler
}

// RateLimitStrategy controls how clients are identified for rate limiting.
type RateLimitStrategy string

const (
	// RateLimitByIP identifies clients by their IP address (default).
	RateLimitByIP RateLimitStrategy = "ip"
	// RateLimitByToken identifies clients by the Authorization header token.
	RateLimitByToken RateLimitStrategy = "token"
	// RateLimitByIPAndToken uses both IP and token for identification.
	RateLimitByIPAndToken RateLimitStrategy = "ip_and_token"
)

// RateLimitMiddleware implements a rate limiting middleware
type RateLimitMiddleware struct {
	name              string
	requestsPerMinute int
	ratePerMinute     float64 // fractional rate, used when requestsPerHour is set
	burstSize         int
	strategy          RateLimitStrategy
	tokenHeader       string // HTTP header to extract token from
	clients           map[string]*client
	mu                sync.Mutex
	cleanupInterval   time.Duration
	stopCleanup       chan struct{}
}

// client tracks the rate limiting state for a single client
type client struct {
	tokens        float64
	lastTimestamp time.Time
}

// NewRateLimitMiddleware creates a new rate limiting middleware with IP-based strategy.
func NewRateLimitMiddleware(name string, requestsPerMinute, burstSize int) *RateLimitMiddleware {
	return &RateLimitMiddleware{
		name:              name,
		requestsPerMinute: requestsPerMinute,
		ratePerMinute:     float64(requestsPerMinute),
		burstSize:         burstSize,
		strategy:          RateLimitByIP,
		tokenHeader:       "Authorization",
		clients:           make(map[string]*client),
		cleanupInterval:   5 * time.Minute,
		stopCleanup:       make(chan struct{}),
	}
}

// NewRateLimitMiddlewareWithHourlyRate creates a rate limiting middleware using
// a per-hour rate. Useful for low-frequency endpoints like registration where
// fractional per-minute rates are needed.
func NewRateLimitMiddlewareWithHourlyRate(name string, requestsPerHour, burstSize int) *RateLimitMiddleware {
	m := &RateLimitMiddleware{
		name:              name,
		requestsPerMinute: 0, // not used when ratePerMinute is set
		ratePerMinute:     float64(requestsPerHour) / 60.0,
		burstSize:         burstSize,
		strategy:          RateLimitByIP,
		tokenHeader:       "Authorization",
		clients:           make(map[string]*client),
		cleanupInterval:   5 * time.Minute,
		stopCleanup:       make(chan struct{}),
	}
	return m
}

// NewRateLimitMiddlewareWithStrategy creates a rate limiting middleware with
// a specific client identification strategy.
func NewRateLimitMiddlewareWithStrategy(name string, requestsPerMinute, burstSize int, strategy RateLimitStrategy) *RateLimitMiddleware {
	m := NewRateLimitMiddleware(name, requestsPerMinute, burstSize)
	m.strategy = strategy
	return m
}

// SetTokenHeader sets a custom header name for token-based rate limiting.
func (m *RateLimitMiddleware) SetTokenHeader(header string) {
	m.tokenHeader = header
}

// Name returns the module name
func (m *RateLimitMiddleware) Name() string {
	return m.name
}

// Init initializes the middleware
func (m *RateLimitMiddleware) Init(app modular.Application) error {
	return nil
}

// clientKey derives the rate limiting key from the request based on the
// configured strategy.
func (m *RateLimitMiddleware) clientKey(r *http.Request) string {
	var ip string
	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		ip = host
	} else {
		ip = clientIP
	}

	// Check X-Forwarded-For for proxied requests
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		forwarded := strings.TrimSpace(parts[0])
		if forwarded != "" {
			ip = forwarded
		}
	}

	switch m.strategy {
	case RateLimitByToken:
		token := r.Header.Get(m.tokenHeader)
		if token != "" {
			return "token:" + token
		}
		// Fall back to IP if no token
		return "ip:" + ip
	case RateLimitByIPAndToken:
		token := r.Header.Get(m.tokenHeader)
		if token != "" {
			return "ip:" + ip + "|token:" + token
		}
		return "ip:" + ip
	default: // RateLimitByIP
		return "ip:" + ip
	}
}

// Process implements middleware processing
func (m *RateLimitMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := m.clientKey(r)

		m.mu.Lock()
		c, exists := m.clients[key]
		if !exists {
			c = &client{tokens: float64(m.burstSize), lastTimestamp: time.Now()}
			m.clients[key] = c
		} else {
			// Refill tokens based on elapsed time using fractional rate
			elapsed := time.Since(c.lastTimestamp).Minutes()
			tokensToAdd := elapsed * m.ratePerMinute
			if tokensToAdd > 0 {
				c.tokens = min(c.tokens+tokensToAdd, float64(m.burstSize))
				c.lastTimestamp = time.Now()
			}
		}

		// Check if request can proceed
		if c.tokens < 1 {
			m.mu.Unlock()
			// Compute how many seconds until 1 token refills, based on the
			// fractional per-minute rate (ratePerMinute tokens/minute).
			retryAfter := "60"
			if m.ratePerMinute > 0 {
				secondsUntilToken := 60.0 / m.ratePerMinute
				retryAfter = strconv.Itoa(int(math.Ceil(secondsUntilToken)))
			}
			w.Header().Set("Retry-After", retryAfter)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Consume token
		c.tokens--
		m.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

// cleanupStaleClients removes client entries that haven't been seen in over
// twice the refill window. This prevents unbounded memory growth.
func (m *RateLimitMiddleware) cleanupStaleClients() {
	// Use fractional ratePerMinute to compute refill window correctly
	refillWindow := 1.0
	if m.ratePerMinute > 0 {
		refillWindow = float64(m.burstSize) / m.ratePerMinute
	}
	staleThreshold := time.Duration(2*refillWindow) * time.Minute
	if staleThreshold < 10*time.Minute {
		staleThreshold = 10 * time.Minute
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for key, c := range m.clients {
		if now.Sub(c.lastTimestamp) > staleThreshold {
			delete(m.clients, key)
		}
	}
}

// Strategy returns the current rate limiting strategy.
func (m *RateLimitMiddleware) Strategy() RateLimitStrategy {
	return m.strategy
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

// Start begins the stale client cleanup goroutine.
func (m *RateLimitMiddleware) Start(_ context.Context) error {
	if m.cleanupInterval <= 0 {
		return nil
	}
	go func() {
		ticker := time.NewTicker(m.cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanupStaleClients()
			case <-m.stopCleanup:
				return
			}
		}
	}()
	return nil
}

// Stop terminates the cleanup goroutine.
func (m *RateLimitMiddleware) Stop(_ context.Context) error {
	select {
	case m.stopCleanup <- struct{}{}:
	default:
	}
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
