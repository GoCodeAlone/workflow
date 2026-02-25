package module

import (
	"context"
	"net/http"

	"github.com/CrisisTextLine/modular"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// OTelMiddleware instruments HTTP requests with OpenTelemetry tracing.
type OTelMiddleware struct {
	name       string
	serverName string
}

// NewOTelMiddleware creates a new OpenTelemetry HTTP tracing middleware.
func NewOTelMiddleware(name, serverName string) *OTelMiddleware {
	return &OTelMiddleware{name: name, serverName: serverName}
}

// Name returns the module name.
func (m *OTelMiddleware) Name() string { return m.name }

// Init initializes the middleware.
func (m *OTelMiddleware) Init(_ modular.Application) error { return nil }

// Process wraps the handler with OpenTelemetry HTTP instrumentation.
func (m *OTelMiddleware) Process(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, m.serverName)
}

// ProvidesServices returns the services provided by this middleware.
func (m *OTelMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "OpenTelemetry HTTP Tracing Middleware", Instance: m},
	}
}

// RequiresServices returns services required by this middleware.
func (m *OTelMiddleware) RequiresServices() []modular.ServiceDependency { return nil }

// Start is a no-op for this middleware.
func (m *OTelMiddleware) Start(_ context.Context) error { return nil }

// Stop is a no-op for this middleware.
func (m *OTelMiddleware) Stop(_ context.Context) error { return nil }
