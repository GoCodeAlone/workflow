package handlers

import (
	"github.com/GoCodeAlone/modular"
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
func (s *ServiceHelper) Service(name string) interface{} {
	var service interface{}
	_ = s.App.GetService(name, &service)
	return service
}

// Services returns all services in the application
func (s *ServiceHelper) Services() map[string]interface{} {
	return s.App.SvcRegistry()
}

// GetService implements the GetService method
func (s *ServiceHelper) GetService(name string, dest interface{}) error {
	return s.App.GetService(name, dest)
}

// RegisterService implements the RegisterService method
func (s *ServiceHelper) RegisterService(name string, service interface{}) error {
	return s.App.RegisterService(name, service)
}

// SvcRegistry implements the SvcRegistry method
func (s *ServiceHelper) SvcRegistry() map[string]interface{} {
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
func GetEventProcessor(app modular.Application) interface{} {
	var processor interface{}
	_ = app.GetService("eventProcessor", &processor)
	return processor
}

// The functions below provide direct fixes for handler code

// FixEventHandlerGetService fixes the GetService calls in events.go
func FixEventHandlerGetService(app modular.Application, name string) interface{} {
	var service interface{}
	_ = app.GetService(name, &service)
	return service
}

// FixHTTPHandlerService fixes app.Service calls in http.go
func FixHTTPHandlerService(app modular.Application, name string) interface{} {
	var service interface{}
	_ = app.GetService(name, &service)
	return service
}

// FixMessagingHandlerServices fixes app.Services calls in messaging.go
func FixMessagingHandlerServices(app modular.Application) map[string]interface{} {
	// Create a map of known services
	services := make(map[string]interface{})

	// Add known services
	var processor interface{}
	_ = app.GetService("eventProcessor", &processor)
	if processor != nil {
		services["eventProcessor"] = processor
	}

	return services
}
