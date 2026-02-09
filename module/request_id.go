package module

import (
	"context"
	"net/http"

	"github.com/CrisisTextLine/modular"
	"github.com/google/uuid"
)

type requestIDKey struct{}

// GetRequestID extracts the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// RequestIDMiddleware reads X-Request-ID header or generates a UUID,
// sets it on the context and response header.
type RequestIDMiddleware struct {
	name       string
	headerName string
}

// NewRequestIDMiddleware creates a new RequestIDMiddleware.
func NewRequestIDMiddleware(name string) *RequestIDMiddleware {
	return &RequestIDMiddleware{
		name:       name,
		headerName: "X-Request-ID",
	}
}

// Name returns the module name.
func (m *RequestIDMiddleware) Name() string {
	return m.name
}

// Init registers the middleware as a service.
func (m *RequestIDMiddleware) Init(app modular.Application) error {
	return app.RegisterService("http.middleware.requestid", m)
}

// Middleware returns the HTTP middleware function.
func (m *RequestIDMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(m.headerName)
			if requestID == "" {
				requestID = uuid.New().String()
			}

			ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
			w.Header().Set(m.headerName, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ProvidesServices returns the services provided by this module.
func (m *RequestIDMiddleware) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        "http.middleware.requestid",
			Description: "HTTP Request ID Middleware",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this module.
func (m *RequestIDMiddleware) RequiresServices() []modular.ServiceDependency {
	return nil
}
