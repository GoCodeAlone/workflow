package module

import (
	"net/http"

	"github.com/CrisisTextLine/modular"
)

// SecurityHeadersMiddleware adds standard security headers to HTTP responses.
type SecurityHeadersMiddleware struct {
	name                  string
	contentSecurityPolicy string
	frameOptions          string
	contentTypeOptions    string
	hstsMaxAge            int
	referrerPolicy        string
	permissionsPolicy     string
}

// SecurityHeadersConfig holds configuration for the security headers middleware.
type SecurityHeadersConfig struct {
	ContentSecurityPolicy string `yaml:"contentSecurityPolicy" default:"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'"`
	FrameOptions          string `yaml:"frameOptions" default:"DENY"`
	ContentTypeOptions    string `yaml:"contentTypeOptions" default:"nosniff"`
	HSTSMaxAge            int    `yaml:"hstsMaxAge" default:"31536000"`
	ReferrerPolicy        string `yaml:"referrerPolicy" default:"strict-origin-when-cross-origin"`
	PermissionsPolicy     string `yaml:"permissionsPolicy" default:"camera=(), microphone=(), geolocation=()"`
}

// NewSecurityHeadersMiddleware creates a new SecurityHeadersMiddleware.
func NewSecurityHeadersMiddleware(name string, cfg SecurityHeadersConfig) *SecurityHeadersMiddleware {
	m := &SecurityHeadersMiddleware{
		name:                  name,
		contentSecurityPolicy: cfg.ContentSecurityPolicy,
		frameOptions:          cfg.FrameOptions,
		contentTypeOptions:    cfg.ContentTypeOptions,
		hstsMaxAge:            cfg.HSTSMaxAge,
		referrerPolicy:        cfg.ReferrerPolicy,
		permissionsPolicy:     cfg.PermissionsPolicy,
	}
	if m.contentSecurityPolicy == "" {
		m.contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'"
	}
	if m.frameOptions == "" {
		m.frameOptions = "DENY"
	}
	if m.contentTypeOptions == "" {
		m.contentTypeOptions = "nosniff"
	}
	if m.hstsMaxAge == 0 {
		m.hstsMaxAge = 31536000
	}
	if m.referrerPolicy == "" {
		m.referrerPolicy = "strict-origin-when-cross-origin"
	}
	if m.permissionsPolicy == "" {
		m.permissionsPolicy = "camera=(), microphone=(), geolocation=()"
	}
	return m
}

// Name returns the module name.
func (m *SecurityHeadersMiddleware) Name() string {
	return m.name
}

// Init registers the middleware as a service.
func (m *SecurityHeadersMiddleware) Init(app modular.Application) error {
	return nil
}

// Process implements the HTTPMiddleware interface.
func (m *SecurityHeadersMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", m.contentTypeOptions)
		w.Header().Set("X-Frame-Options", m.frameOptions)
		w.Header().Set("Content-Security-Policy", m.contentSecurityPolicy)
		w.Header().Set("Referrer-Policy", m.referrerPolicy)
		w.Header().Set("Permissions-Policy", m.permissionsPolicy)
		if m.hstsMaxAge > 0 {
			w.Header().Set("Strict-Transport-Security", "max-age="+itoa(m.hstsMaxAge)+"; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// Middleware returns the HTTP middleware function.
func (m *SecurityHeadersMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return m.Process(next)
	}
}

// ProvidesServices returns the services provided by this module.
func (m *SecurityHeadersMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "HTTP Security Headers Middleware",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this module.
func (m *SecurityHeadersMiddleware) RequiresServices() []modular.ServiceDependency {
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
