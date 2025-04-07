package handlers

import (
	"github.com/GoCodeAlone/modular"
)

// ServiceAccessor provides methods for accessing services
type ServiceAccessor interface {
	Service(name string) interface{}
	Services() map[string]interface{}
}

// ApplicationHelper extends the modular.Application with useful service methods
type ApplicationHelper struct {
	app          modular.Application
	serviceCache map[string]interface{}
}

// NewApplicationHelper creates a helper for application service access
func NewApplicationHelper(app modular.Application) *ApplicationHelper {
	return &ApplicationHelper{
		app:          app,
		serviceCache: make(map[string]interface{}),
	}
}

// Service provides access to a named service
func (h *ApplicationHelper) Service(name string) interface{} {
	// Check cache first
	if svc, found := h.serviceCache[name]; found {
		return svc
	}

	// Get service from app
	var service interface{}
	_ = h.app.GetService(name, &service)

	// Cache it if found
	if service != nil {
		h.serviceCache[name] = service
	}

	return service
}

// Services returns all cached services
func (h *ApplicationHelper) Services() map[string]interface{} {
	return h.serviceCache
}

// WithHelper returns the helper or creates one if needed
func WithHelper(app modular.Application) *ApplicationHelper {
	// See if we already have a helper attached
	if helper, ok := app.(interface{ Helper() *ApplicationHelper }); ok {
		return helper.Helper()
	}
	return NewApplicationHelper(app)
}

// GetService is a utility function to get services from an application
func GetService(app modular.Application, name string) interface{} {
	// Use helper for consistent access
	helper := WithHelper(app)
	return helper.Service(name)
}

// PatchAppServiceCalls patches common calls in the application's handler functions
// This is a temporary solution until the handlers are updated to use the new API
var PatchAppServiceCalls = func(app modular.Application) {
	// We've already updated the handlers to use the correct API
	// No need for patching anymore, but keeping this function for backward compatibility
}
