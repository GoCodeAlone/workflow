package http

import "github.com/GoCodeAlone/workflow/schema"

// moduleSchemas returns UI schema definitions for all HTTP module types.
func moduleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		httpServerSchema(),
		httpRouterSchema(),
		httpHandlerSchema(),
		httpProxySchema(),
		reverseProxySchema(),
		httpSimpleProxySchema(),
		staticFileServerSchema(),
		authMiddlewareSchema(),
		loggingMiddlewareSchema(),
		rateLimitMiddlewareSchema(),
		corsMiddlewareSchema(),
		requestIDMiddlewareSchema(),
		securityHeadersMiddlewareSchema(),
	}
}

func httpServerSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.server",
		Label:       "HTTP Server",
		Category:    "http",
		Description: "Standard HTTP server that listens on a network address",
		Outputs:     []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "Incoming HTTP requests"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "address", Label: "Listen Address", Type: schema.FieldTypeString, Required: true, Description: "Host:port to listen on (e.g. :8080, 0.0.0.0:80)", DefaultValue: ":8080", Placeholder: ":8080"},
		},
		DefaultConfig: map[string]any{"address": ":8080"},
		MaxIncoming:   intPtr(0),
	}
}

func httpRouterSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:         "http.router",
		Label:        "HTTP Router",
		Category:     "http",
		Description:  "Routes HTTP requests to handlers based on path and method",
		Inputs:       []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "Incoming HTTP request to route"}},
		Outputs:      []schema.ServiceIODef{{Name: "routed", Type: "http.Request", Description: "Routed HTTP request dispatched to handler"}},
		ConfigFields: []schema.ConfigFieldDef{},
	}
}

func httpHandlerSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.handler",
		Label:       "HTTP Handler",
		Category:    "http",
		Description: "Handles HTTP requests and produces responses",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to handle"}},
		Outputs:     []schema.ServiceIODef{{Name: "response", Type: "http.Response", Description: "HTTP response"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "contentType", Label: "Content Type", Type: schema.FieldTypeString, Description: "Response content type", DefaultValue: "application/json", Placeholder: "application/json"},
		},
		DefaultConfig: map[string]any{"contentType": "application/json"},
	}
}

func httpProxySchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:         "http.proxy",
		Label:        "HTTP Proxy",
		Category:     "http",
		Description:  "Reverse proxy using the CrisisTextLine/modular reverseproxy module",
		Inputs:       []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:      []schema.ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []schema.ConfigFieldDef{},
	}
}

func reverseProxySchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:         "reverseproxy",
		Label:        "Reverse Proxy",
		Category:     "http",
		Description:  "Reverse proxy using the CrisisTextLine/modular reverseproxy module",
		Inputs:       []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:      []schema.ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []schema.ConfigFieldDef{},
	}
}

func httpSimpleProxySchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.simple_proxy",
		Label:       "Simple Proxy",
		Category:    "http",
		Description: "Simple reverse proxy with prefix-based target routing",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:     []schema.ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "targets", Label: "Targets", Type: schema.FieldTypeMap, MapValueType: "string", Description: "Map of URL prefix to backend URL (e.g. /api -> http://localhost:3000)", Placeholder: "/api=http://backend:8080"},
		},
	}
}

func staticFileServerSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "static.fileserver",
		Label:       "Static File Server",
		Category:    "http",
		Description: "Serves static files from a directory with optional SPA fallback",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request for static file"}},
		Outputs:     []schema.ServiceIODef{{Name: "file", Type: "http.Response", Description: "Static file response"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "root", Label: "Root Directory", Type: schema.FieldTypeString, Required: true, Description: "Path to the directory containing static files", Placeholder: "./ui/dist"},
			{Key: "prefix", Label: "URL Prefix", Type: schema.FieldTypeString, DefaultValue: "/", Description: "URL path prefix to serve files under", Placeholder: "/"},
			{Key: "spaFallback", Label: "SPA Fallback", Type: schema.FieldTypeBool, DefaultValue: true, Description: "When enabled, serves index.html for unmatched paths (for single-page apps)"},
			{Key: "cacheMaxAge", Label: "Cache Max-Age (sec)", Type: schema.FieldTypeNumber, DefaultValue: 3600, Description: "Cache-Control max-age in seconds for static assets"},
			{Key: "router", Label: "Router Name", Type: schema.FieldTypeString, Description: "Explicit router module name to register on (auto-detected if omitted)", Placeholder: "my-router", InheritFrom: "dependency.name"},
		},
		DefaultConfig: map[string]any{"prefix": "/", "spaFallback": true, "cacheMaxAge": 3600},
	}
}

func authMiddlewareSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.middleware.auth",
		Label:       "Auth Middleware",
		Category:    "middleware",
		Description: "Authentication middleware that validates tokens on incoming requests",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "Unauthenticated HTTP request"}},
		Outputs:     []schema.ServiceIODef{{Name: "authed", Type: "http.Request", Description: "Authenticated HTTP request with claims"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "authType", Label: "Auth Type", Type: schema.FieldTypeSelect, Options: []string{"Bearer", "Basic", "ApiKey"}, DefaultValue: "Bearer", Description: "Authentication scheme to enforce"},
		},
		DefaultConfig: map[string]any{"authType": "Bearer"},
	}
}

func loggingMiddlewareSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.middleware.logging",
		Label:       "Logging Middleware",
		Category:    "middleware",
		Description: "HTTP request/response logging middleware",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to log"}},
		Outputs:     []schema.ServiceIODef{{Name: "logged", Type: "http.Request", Description: "HTTP request (passed through after logging)"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "logLevel", Label: "Log Level", Type: schema.FieldTypeSelect, Options: []string{"debug", "info", "warn", "error"}, DefaultValue: "info", Description: "Minimum log level for request logging"},
		},
		DefaultConfig: map[string]any{"logLevel": "info"},
	}
}

func rateLimitMiddlewareSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.middleware.ratelimit",
		Label:       "Rate Limiter",
		Category:    "middleware",
		Description: "Rate limiting middleware to control request throughput",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to rate-limit"}},
		Outputs:     []schema.ServiceIODef{{Name: "limited", Type: "http.Request", Description: "HTTP request (passed through if within limit)"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "requestsPerMinute", Label: "Requests Per Minute", Type: schema.FieldTypeNumber, DefaultValue: 60, Description: "Maximum number of requests per minute per client (mutually exclusive with requestsPerHour)"},
			{Key: "requestsPerHour", Label: "Requests Per Hour", Type: schema.FieldTypeNumber, DefaultValue: 0, Description: "Maximum number of requests per hour per client; takes precedence over requestsPerMinute when set"},
			{Key: "burstSize", Label: "Burst Size", Type: schema.FieldTypeNumber, DefaultValue: 10, Description: "Maximum burst of requests allowed above the rate limit"},
		},
		DefaultConfig: map[string]any{"requestsPerMinute": 60, "burstSize": 10},
	}
}

func corsMiddlewareSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.middleware.cors",
		Label:       "CORS Middleware",
		Category:    "middleware",
		Description: "Cross-Origin Resource Sharing (CORS) middleware",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request needing CORS headers"}},
		Outputs:     []schema.ServiceIODef{{Name: "cors", Type: "http.Request", Description: "HTTP request with CORS headers applied"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "allowedOrigins", Label: "Allowed Origins", Type: schema.FieldTypeArray, ArrayItemType: "string", DefaultValue: []string{"*"}, Description: "Allowed origins (e.g. https://example.com, http://localhost:3000)"},
			{Key: "allowedMethods", Label: "Allowed Methods", Type: schema.FieldTypeArray, ArrayItemType: "string", DefaultValue: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}, Description: "Allowed HTTP methods"},
		},
		DefaultConfig: map[string]any{
			"allowedOrigins": []string{"*"},
			"allowedMethods": []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		},
	}
}

func requestIDMiddlewareSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:         "http.middleware.requestid",
		Label:        "Request ID Middleware",
		Category:     "middleware",
		Description:  "Adds a unique request ID header to each request for tracing",
		Inputs:       []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request without request ID"}},
		Outputs:      []schema.ServiceIODef{{Name: "tagged", Type: "http.Request", Description: "HTTP request with X-Request-ID header"}},
		ConfigFields: []schema.ConfigFieldDef{},
	}
}

func securityHeadersMiddlewareSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "http.middleware.securityheaders",
		Label:       "Security Headers",
		Category:    "middleware",
		Description: "Adds security-related HTTP headers to responses",
		Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to add security headers"}},
		Outputs:     []schema.ServiceIODef{{Name: "secured", Type: "http.Request", Description: "HTTP request with security headers"}},
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "contentSecurityPolicy", Label: "Content Security Policy", Type: schema.FieldTypeString, Description: "CSP header value", Placeholder: "default-src 'self'", Group: "headers"},
			{Key: "frameOptions", Label: "X-Frame-Options", Type: schema.FieldTypeSelect, Options: []string{"DENY", "SAMEORIGIN"}, DefaultValue: "DENY", Description: "Controls whether the page can be embedded in frames", Group: "headers"},
			{Key: "contentTypeOptions", Label: "X-Content-Type-Options", Type: schema.FieldTypeString, DefaultValue: "nosniff", Description: "Prevents MIME type sniffing", Group: "headers"},
			{Key: "hstsMaxAge", Label: "HSTS Max-Age (sec)", Type: schema.FieldTypeNumber, DefaultValue: 31536000, Description: "HTTP Strict Transport Security max-age in seconds", Group: "headers"},
			{Key: "referrerPolicy", Label: "Referrer Policy", Type: schema.FieldTypeSelect, Options: []string{"no-referrer", "no-referrer-when-downgrade", "origin", "origin-when-cross-origin", "same-origin", "strict-origin", "strict-origin-when-cross-origin", "unsafe-url"}, DefaultValue: "strict-origin-when-cross-origin", Description: "Controls the Referer header sent with requests", Group: "headers"},
			{Key: "permissionsPolicy", Label: "Permissions Policy", Type: schema.FieldTypeString, DefaultValue: "camera=(), microphone=(), geolocation=()", Description: "Controls which browser features are allowed", Group: "headers"},
		},
		DefaultConfig: map[string]any{
			"frameOptions":       "DENY",
			"contentTypeOptions": "nosniff",
			"hstsMaxAge":         31536000,
			"referrerPolicy":     "strict-origin-when-cross-origin",
			"permissionsPolicy":  "camera=(), microphone=(), geolocation=()",
		},
	}
}

func intPtr(v int) *int { return &v }
