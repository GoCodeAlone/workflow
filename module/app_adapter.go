package module

import (
	"github.com/GoCodeAlone/modular"
)

// AppAdapter wraps the modular.Application interface and adds compatibility methods
type AppAdapter struct {
	App      modular.Application
	services map[string]interface{}
}

// NewAppAdapter creates a new application adapter
func NewAppAdapter(app modular.Application) *AppAdapter {
	return &AppAdapter{
		App:      app,
		services: make(map[string]interface{}),
	}
}

// GetService provides the compatibility version for the handlers
func (a *AppAdapter) GetService(name string) interface{} {
	return a.App.GetService(name, nil)
}

// Service provides a simpler way to access services
func (a *AppAdapter) Service(name string) interface{} {
	return a.App.GetService(name, nil)
}

// Services returns all registered services
func (a *AppAdapter) Services() map[string]interface{} {
	return a.services
}

// RegisterService registers a service and also stores it in the local map
func (a *AppAdapter) RegisterService(name string, service interface{}) error {
	err := a.App.RegisterService(name, service)
	if err == nil {
		a.services[name] = service
	}
	return err
}

// WrapApplication wraps the original application with the compatibility layer
func WrapApplication(app modular.Application) *AppAdapter {
	return NewAppAdapter(app)
}
