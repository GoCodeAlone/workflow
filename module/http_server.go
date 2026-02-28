package module

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/pkg/tlsutil"
	"golang.org/x/crypto/acme/autocert"
)

// HTTPServerTLSConfig holds TLS configuration for the HTTP server.
type HTTPServerTLSConfig struct {
	Mode         string               `yaml:"mode" json:"mode"` // manual | autocert | disabled
	Manual       tlsutil.TLSConfig    `yaml:"manual" json:"manual"`
	Autocert     tlsutil.AutocertConfig `yaml:"autocert" json:"autocert"`
	ClientCAFile string               `yaml:"client_ca_file" json:"client_ca_file"`
	ClientAuth   string               `yaml:"client_auth" json:"client_auth"` // require | request | none
}

// StandardHTTPServer implements the HTTPServer interface and modular.Module interfaces
type StandardHTTPServer struct {
	name         string
	server       *http.Server
	address      string
	router       HTTPRouter
	logger       modular.Logger
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	tlsCfg       HTTPServerTLSConfig
}

// NewStandardHTTPServer creates a new HTTP server with the given name and address
func NewStandardHTTPServer(name, address string) *StandardHTTPServer {
	return &StandardHTTPServer{
		name:    name,
		address: address,
	}
}

// SetTimeouts configures read, write, and idle timeouts for the HTTP server.
// Zero values will use defaults (30s read/write, 120s idle).
func (s *StandardHTTPServer) SetTimeouts(read, write, idle time.Duration) {
	s.readTimeout = read
	s.writeTimeout = write
	s.idleTimeout = idle
}

// SetTLSConfig configures TLS for the HTTP server.
func (s *StandardHTTPServer) SetTLSConfig(cfg HTTPServerTLSConfig) {
	s.tlsCfg = cfg
}

// Name returns the unique identifier for this module
func (s *StandardHTTPServer) Name() string {
	return s.name
}

// Init initializes the module with the application context
func (s *StandardHTTPServer) Init(app modular.Application) error {
	s.logger = app.Logger()
	// Get configuration if available
	configSection, err := app.GetConfigSection("http")
	if err == nil {
		if config := configSection.GetConfig(); config != nil {
			if cfg, ok := config.(map[string]any); ok {
				if addr, ok := cfg["address"].(string); ok && addr != "" {
					s.address = addr
				}
			}
		}
	}

	return nil
}

// AddRouter adds a router to the HTTP server
func (s *StandardHTTPServer) AddRouter(router HTTPRouter) {
	s.router = router
}

// Start starts the HTTP server
func (s *StandardHTTPServer) Start(ctx context.Context) error {
	if s.router == nil {
		return fmt.Errorf("no router configured for HTTP server")
	}

	// Create HTTP server with the router
	handler, ok := s.router.(http.Handler)
	if !ok {
		return fmt.Errorf("router does not implement http.Handler")
	}

	s.server = &http.Server{
		Addr:              s.address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       timeoutOrDefault(s.readTimeout, 30*time.Second),
		WriteTimeout:      timeoutOrDefault(s.writeTimeout, 30*time.Second),
		IdleTimeout:       timeoutOrDefault(s.idleTimeout, 120*time.Second),
	}

	switch s.tlsCfg.Mode {
	case "autocert":
		return s.startAutocert(ctx)
	case "manual":
		return s.startManualTLS(ctx)
	default:
		// Plain HTTP
		go func() {
			if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.logger.Error("HTTP server error", "error", err)
			}
		}()
		s.logger.Info("HTTP server started", "address", s.address)
		return nil
	}
}

// startManualTLS starts the server with manually configured TLS certificates.
func (s *StandardHTTPServer) startManualTLS(ctx context.Context) error {
	manualCfg := s.tlsCfg.Manual
	manualCfg.Enabled = true

	// Overlay mTLS settings from the top-level fields when set
	if s.tlsCfg.ClientCAFile != "" {
		manualCfg.CAFile = s.tlsCfg.ClientCAFile
	}
	if s.tlsCfg.ClientAuth != "" {
		manualCfg.ClientAuth = s.tlsCfg.ClientAuth
	}

	tlsConfig, err := tlsutil.LoadTLSConfig(manualCfg)
	if err != nil {
		return fmt.Errorf("http server TLS config: %w", err)
	}
	s.server.TLSConfig = tlsConfig

	go func() {
		if err := s.server.ListenAndServeTLS(manualCfg.CertFile, manualCfg.KeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("HTTPS server error", "error", err)
		}
	}()
	s.logger.Info("HTTPS server started (manual TLS)", "address", s.address)
	return nil
}

// startAutocert starts the server using Let's Encrypt via autocert.
func (s *StandardHTTPServer) startAutocert(ctx context.Context) error {
	ac := s.tlsCfg.Autocert
	if len(ac.Domains) == 0 {
		return fmt.Errorf("http server autocert: at least one domain is required")
	}

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(ac.Domains...),
		Email:      ac.Email,
	}
	if ac.CacheDir != "" {
		m.Cache = autocert.DirCache(ac.CacheDir)
	}

	s.server.TLSConfig = &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	// ACME HTTP-01 challenge listener on :80
	go func() {
		httpSrv := &http.Server{
			Addr:              ":80",
			Handler:           m.HTTPHandler(nil),
			ReadHeaderTimeout: 10 * time.Second,
		}
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("autocert HTTP-01 listener error", "error", err)
		}
	}()

	go func() {
		if err := s.server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("HTTPS server error (autocert)", "error", err)
		}
	}()
	s.logger.Info("HTTPS server started (autocert)", "address", s.address, "domains", ac.Domains)
	return nil
}

// Stop stops the HTTP server
func (s *StandardHTTPServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil // Nothing to stop
	}

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("error shutting down HTTP server: %w", err)
	}

	fmt.Println("HTTP server stopped")
	return nil
}

// ProvidesServices returns a list of services provided by this module
func (s *StandardHTTPServer) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        s.name,
			Description: "HTTP Server",
			Instance:    s,
		},
	}
}

// RequiresServices returns a list of services required by this module
func (s *StandardHTTPServer) RequiresServices() []modular.ServiceDependency {
	// No required services
	return nil
}

// timeoutOrDefault returns d if non-zero, otherwise returns the defaultVal.
func timeoutOrDefault(d, defaultVal time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return defaultVal
}
