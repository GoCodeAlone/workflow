package module

import (
	"errors"
	"reflect"

	"github.com/GoCodeAlone/modular"
)

// ServiceAdapter provides methods for the modular framework
type ServiceAdapter struct {
	app      modular.Application
	services map[string]interface{}
}

// NewServiceAdapter creates a new adapter for service management
func NewServiceAdapter(app modular.Application) *ServiceAdapter {
	return &ServiceAdapter{
		app:      app,
		services: make(map[string]interface{}),
	}
}

// GetService is a compatibility method for handlers that expect
// a GetService(name string, out any) error signature
func (sa *ServiceAdapter) GetService(name string, out interface{}) error {
	// From error messages, it appears sa.app.GetService expects:
	// GetService(name string, out any) interface{}
	svc := sa.app.GetService(name, nil)

	if svc == nil {
		return errors.New("service not found")
	}

	// If out parameter is provided, set it using reflection
	if out != nil {
		outVal := reflect.ValueOf(out)
		if outVal.Kind() != reflect.Ptr {
			return errors.New("out parameter must be a pointer")
		}

		// Handle the specific case of EventProcessor
		if outPtr, ok := out.(**EventProcessor); ok {
			if ep, ok := svc.(*EventProcessor); ok {
				*outPtr = ep
				return nil
			}
		}

		// Generic case using reflection
		elem := outVal.Elem()
		if elem.Kind() == reflect.Ptr && elem.CanSet() {
			svcVal := reflect.ValueOf(svc)
			if svcVal.Type().AssignableTo(elem.Type()) {
				elem.Set(svcVal)
			}
		}
	}

	return nil
}

// Service is a compatibility method for handlers that expect
// a Service(name string) interface{} method
func (sa *ServiceAdapter) Service(name string) interface{} {
	return sa.app.GetService(name, nil)
}

// Services is a compatibility method for handlers that expect
// a Services() map[string]interface{} method
func (sa *ServiceAdapter) Services() map[string]interface{} {
	// Since app.Services() doesn't exist, we use our own registry
	return sa.services
}

// RegisterService adds a service to our registry
func (sa *ServiceAdapter) RegisterService(name string, service interface{}) error {
	sa.services[name] = service
	return nil
}

// ExtendApplication creates an adapter that extends the application interface
func ExtendApplication(app modular.Application) *ServiceAdapter {
	adapter := NewServiceAdapter(app)

	// Pre-populate the services map with any services we can discover
	// This could be done by scanning known service names if available

	return adapter
}
