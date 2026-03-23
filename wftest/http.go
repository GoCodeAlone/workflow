package wftest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// RequestOption configures an HTTP test request.
type RequestOption func(*http.Request)

// Header returns a RequestOption that sets a custom request header.
func Header(key, value string) RequestOption {
	return func(r *http.Request) {
		r.Header.Set(key, value)
	}
}

// GET sends a GET request to the harness HTTP handler and returns the result.
// Requires an http.router module in the config.
func (h *Harness) GET(path string, opts ...RequestOption) *Result {
	h.t.Helper()
	return h.doHTTP(http.MethodGet, path, "", opts)
}

// POST sends a POST request with a body to the harness HTTP handler.
// Content-Type is set to application/json unless overridden via Header().
// Requires an http.router module in the config.
func (h *Harness) POST(path, body string, opts ...RequestOption) *Result {
	h.t.Helper()
	return h.doHTTP(http.MethodPost, path, body, opts)
}

// PUT sends a PUT request with a body to the harness HTTP handler.
// Content-Type is set to application/json unless overridden via Header().
// Requires an http.router module in the config.
func (h *Harness) PUT(path, body string, opts ...RequestOption) *Result {
	h.t.Helper()
	return h.doHTTP(http.MethodPut, path, body, opts)
}

// DELETE sends a DELETE request to the harness HTTP handler.
// Requires an http.router module in the config.
func (h *Harness) DELETE(path string, opts ...RequestOption) *Result {
	h.t.Helper()
	return h.doHTTP(http.MethodDelete, path, "", opts)
}

func (h *Harness) doHTTP(method, path, body string, opts []RequestOption) *Result {
	h.t.Helper()
	handler := h.getHTTPHandler()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	rawBody, _ := io.ReadAll(resp.Body)

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &Result{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		RawBody:    rawBody,
	}
}
