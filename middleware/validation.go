package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ValidationConfig holds settings for the input validation middleware.
type ValidationConfig struct {
	// MaxBodySize is the maximum allowed request body size in bytes.
	// Defaults to 1MB if zero.
	MaxBodySize int64

	// AllowedContentTypes lists the Content-Type values accepted for requests
	// with a body (POST, PUT, PATCH). If empty, defaults to ["application/json"].
	AllowedContentTypes []string

	// ValidateJSON when true requires that request bodies with content-type
	// application/json are well-formed JSON.
	ValidateJSON bool
}

// DefaultValidationConfig returns a config with sensible defaults.
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MaxBodySize:         1 << 20, // 1MB
		AllowedContentTypes: []string{"application/json"},
		ValidateJSON:        true,
	}
}

// InputValidation returns an http.Handler middleware that validates request
// body size, content-type, and JSON well-formedness.
func InputValidation(cfg ValidationConfig, next http.Handler) http.Handler {
	if cfg.MaxBodySize <= 0 {
		cfg.MaxBodySize = 1 << 20
	}
	if len(cfg.AllowedContentTypes) == 0 {
		cfg.AllowedContentTypes = []string{"application/json"}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate body-bearing methods
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			// Check Content-Type
			ct := r.Header.Get("Content-Type")
			if ct != "" && !contentTypeAllowed(ct, cfg.AllowedContentTypes) {
				http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
				return
			}

			// Enforce body size limit
			r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodySize)

			// Validate JSON well-formedness if requested
			if cfg.ValidateJSON && isJSONContentType(ct) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					// MaxBytesReader returns a specific error on overflow
					if isMaxBytesError(err) {
						http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
						return
					}
					http.Error(w, "failed to read request body", http.StatusBadRequest)
					return
				}

				if len(body) > 0 && !json.Valid(body) {
					http.Error(w, "malformed JSON in request body", http.StatusBadRequest)
					return
				}

				// Replace the body so downstream handlers can read it
				r.Body = io.NopCloser(strings.NewReader(string(body)))
			}
		}

		next.ServeHTTP(w, r)
	})
}

// contentTypeAllowed checks whether the request Content-Type matches one of
// the allowed types (prefix match to handle charset parameters).
func contentTypeAllowed(ct string, allowed []string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	for _, a := range allowed {
		if strings.HasPrefix(ct, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// isJSONContentType returns true if the Content-Type header indicates JSON.
func isJSONContentType(ct string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json")
}

// isMaxBytesError checks if the error is from http.MaxBytesReader.
func isMaxBytesError(err error) bool {
	// http.MaxBytesReader returns *http.MaxBytesError in Go 1.19+
	_, ok := err.(*http.MaxBytesError)
	return ok
}
