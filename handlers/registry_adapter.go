// Package handlers provides workflow handling capabilities
package handlers

import (
	"github.com/GoCodeAlone/modular"
)

// applicationServiceRegistryAdapter is an adapter for allowing an Application to be used as a service registry
type applicationServiceRegistryAdapter struct {
	application modular.Application
}

// NewServiceRegistryAdapter creates an adapter for service registry operations
func NewServiceRegistryAdapter(app modular.Application) *applicationServiceRegistryAdapter {
	return &applicationServiceRegistryAdapter{
		application: app,
	}
}

// GetService delegates to the application
func (a *applicationServiceRegistryAdapter) GetService(name string, dest interface{}) error {
	return a.application.GetService(name, dest)
}

// RegisterService delegates to the application
func (a *applicationServiceRegistryAdapter) RegisterService(name string, service interface{}) error {
	return a.application.RegisterService(name, service)
}

// SvcRegistry delegates to the application's service registry
func (a *applicationServiceRegistryAdapter) SvcRegistry() map[string]interface{} {
	return a.application.SvcRegistry()
}

// Init delegates to the application
func (a *applicationServiceRegistryAdapter) Init() error {
	return a.application.Init()
}
