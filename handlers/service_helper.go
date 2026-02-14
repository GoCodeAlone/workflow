package handlers

import (
	"github.com/CrisisTextLine/modular"
)

// ServiceHelper helps with service access in handlers
type ServiceHelper struct {
	App modular.Application
}

// New creates a new service helper for an application
func New(app modular.Application) *ServiceHelper {
	return &ServiceHelper{App: app}
}

// Service retrieves a service by name
func (s *ServiceHelper) Service(name string) any {
	var service any
	_ = s.App.GetService(name, &service)
	return service
}

// Services returns all services in the application
func (s *ServiceHelper) Services() map[string]any {
	return s.App.SvcRegistry()
}

// GetService implements the GetService method
func (s *ServiceHelper) GetService(name string, dest any) error {
	return s.App.GetService(name, dest)
}

// RegisterService implements the RegisterService method
func (s *ServiceHelper) RegisterService(name string, service any) error {
	return s.App.RegisterService(name, service)
}

// SvcRegistry implements the SvcRegistry method
func (s *ServiceHelper) SvcRegistry() map[string]any {
	return s.App.SvcRegistry()
}

// Init initializes the service helper
func (s *ServiceHelper) Init() error {
	return s.App.Init()
}

// GetServiceHelper returns a helper for an application
func GetServiceHelper(app modular.Application) *ServiceHelper {
	return New(app)
}

// GetEventProcessor is a utility function to get the event processor service
func GetEventProcessor(app modular.Application) any {
	var processor any
	_ = app.GetService("eventProcessor", &processor)
	return processor
}

// The functions below provide direct fixes for handler code

// FixEventHandlerGetService fixes the GetService calls in events.go
func FixEventHandlerGetService(app modular.Application, name string) any {
	var service any
	_ = app.GetService(name, &service)
	return service
}

// FixHTTPHandlerService fixes app.Service calls in http.go
func FixHTTPHandlerService(app modular.Application, name string) any {
	var service any
	_ = app.GetService(name, &service)
	return service
}

// FixMessagingHandlerServices fixes app.Services calls in messaging.go
func FixMessagingHandlerServices(app modular.Application) map[string]any {
	// Create a map of known services
	services := make(map[string]any)

	// Add known services
	var processor any
	_ = app.GetService("eventProcessor", &processor)
	if processor != nil {
		services["eventProcessor"] = processor
	}

	return services
}
