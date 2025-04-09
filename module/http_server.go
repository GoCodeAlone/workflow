package module

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/modular"
)

// StandardHTTPServer implements the HTTPServer interface and modular.Module interfaces
type StandardHTTPServer struct {
	name    string
	server  *http.Server
	address string
	router  HTTPRouter
	logger  modular.Logger
}

// NewStandardHTTPServer creates a new HTTP server with the given name and address
func NewStandardHTTPServer(name, address string) *StandardHTTPServer {
	return &StandardHTTPServer{
		name:    name,
		address: address,
	}
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
			if cfg, ok := config.(map[string]interface{}); ok {
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
		Addr:    s.address,
		Handler: handler,
	}

	// Start the server in a goroutine
	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()

	s.logger.Info("HTTP server started", "address", s.address)
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
