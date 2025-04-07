package module

import (
	"encoding/json"
	"net/http"

	"github.com/GoCodeAlone/modular"
)

// SimpleHTTPHandler provides a basic implementation of an HTTP handler
type SimpleHTTPHandler struct {
	name        string
	handleFunc  func(w http.ResponseWriter, r *http.Request)
	contentType string
}

// NewSimpleHTTPHandler creates a new HTTP handler with the given name
func NewSimpleHTTPHandler(name string, contentType string) *SimpleHTTPHandler {
	return &SimpleHTTPHandler{
		name:        name,
		contentType: contentType,
	}
}

// Name returns the unique identifier for this module
func (h *SimpleHTTPHandler) Name() string {
	return h.name
}

// Init initializes the HTTP handler
func (h *SimpleHTTPHandler) Init(app modular.Application) error {
	// Initialize the handler if needed
	return nil
}

// Handle implements the HTTPHandler interface
func (h *SimpleHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.handleFunc != nil {
		h.handleFunc(w, r)
		return
	}

	// Default implementation if no custom handler is provided
	if h.contentType != "" {
		w.Header().Set("Content-Type", h.contentType)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}

	response := map[string]string{
		"handler": h.name,
		"status":  "success",
		"message": "Default handler response",
	}

	json.NewEncoder(w).Encode(response)
}

// ServeHTTP implements the http.Handler interface
func (h *SimpleHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Handle(w, r)
}

// SetHandleFunc sets a custom handler function
func (h *SimpleHTTPHandler) SetHandleFunc(fn func(w http.ResponseWriter, r *http.Request)) {
	h.handleFunc = fn
}

// ProvidesServices returns a list of services provided by this module
func (h *SimpleHTTPHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        h.name,
			Description: "HTTP Handler",
			Instance:    h,
		},
	}
}

// RequiresServices returns a list of services required by this module
func (h *SimpleHTTPHandler) RequiresServices() []modular.ServiceDependency {
	return nil
}
