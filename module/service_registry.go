// Package module defines core interfaces for the workflow engine
package module

// ServiceRegistry defines the interface for registering and retrieving services
type ServiceRegistry interface {
	// GetService returns a service by name
	GetService(name string, out interface{}) error

	// RegisterService registers a service with the application
	RegisterService(name string, service interface{}) error
}
