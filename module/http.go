package module

import (
	"context"
	"net/http"
)

// HTTPHandler interface for handling HTTP requests
type HTTPHandler interface {
	Handle(w http.ResponseWriter, r *http.Request)
}

// HTTPRouter interface for routing HTTP requests
type HTTPRouter interface {
	AddRoute(method, path string, handler HTTPHandler)
}

// HTTPServer interface for HTTP server modules
type HTTPServer interface {
	AddRouter(router HTTPRouter)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// HTTPHandlerAdapter adapts an http.Handler to the HTTPHandler interface
type HTTPHandlerAdapter struct {
	handler http.Handler
}

// NewHTTPHandlerAdapter creates a new adapter for an http.Handler
func NewHTTPHandlerAdapter(handler http.Handler) *HTTPHandlerAdapter {
	return &HTTPHandlerAdapter{handler: handler}
}

// Handle implements the HTTPHandler interface
func (a *HTTPHandlerAdapter) Handle(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}
