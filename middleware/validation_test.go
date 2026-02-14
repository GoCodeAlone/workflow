package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestInputValidation_DefaultConfig(t *testing.T) {
	cfg := DefaultValidationConfig()
	if cfg.MaxBodySize != 1<<20 {
		t.Errorf("expected default MaxBodySize 1MB, got %d", cfg.MaxBodySize)
	}
	if len(cfg.AllowedContentTypes) != 1 || cfg.AllowedContentTypes[0] != "application/json" {
		t.Errorf("expected default content type application/json, got %v", cfg.AllowedContentTypes)
	}
	if !cfg.ValidateJSON {
		t.Error("expected ValidateJSON true by default")
	}
}

func TestInputValidation_GET_Passthrough(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET request: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_ValidJSON(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	body := `{"key": "value"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("valid JSON POST: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_MalformedJSON(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	body := `{"key": broken}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed JSON: expected 400, got %d", rec.Code)
	}
}

func TestInputValidation_BodyTooLarge(t *testing.T) {
	cfg := ValidationConfig{
		MaxBodySize:         10, // 10 bytes
		AllowedContentTypes: []string{"application/json"},
		ValidateJSON:        true,
	}
	handler := InputValidation(cfg, okHandler())

	body := `{"key": "this is way too long for the limit"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: expected 413, got %d", rec.Code)
	}
}

func TestInputValidation_UnsupportedContentType(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("some text"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("unsupported content type: expected 415, got %d", rec.Code)
	}
}

func TestInputValidation_ContentTypeWithCharset(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	body := `{"ok": true}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("JSON with charset: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_EmptyBody(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("empty body: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_NoContentTypeHeader(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"a":1}`))
	// No Content-Type header set
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// No content-type header means we skip the content-type check (ct is empty)
	if rec.Code != http.StatusOK {
		t.Errorf("no content-type: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_PUTMethod(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	body := `{"update": true}`
	req := httptest.NewRequest(http.MethodPut, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("PUT with valid JSON: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_PATCHMethod(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	body := `{"patch": true}`
	req := httptest.NewRequest(http.MethodPatch, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("PATCH with valid JSON: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_JSONValidationDisabled(t *testing.T) {
	cfg := ValidationConfig{
		MaxBodySize:         1 << 20,
		AllowedContentTypes: []string{"application/json"},
		ValidateJSON:        false,
	}
	handler := InputValidation(cfg, okHandler())

	// Malformed JSON should pass when validation is disabled
	body := `not json at all`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("disabled validation: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_MultipleAllowedContentTypes(t *testing.T) {
	cfg := ValidationConfig{
		MaxBodySize:         1 << 20,
		AllowedContentTypes: []string{"application/json", "application/xml"},
		ValidateJSON:        false,
	}
	handler := InputValidation(cfg, okHandler())

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("<root/>"))
	req.Header.Set("Content-Type", "application/xml")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("XML content type: expected 200, got %d", rec.Code)
	}
}

func TestInputValidation_DELETE_Passthrough(t *testing.T) {
	handler := InputValidation(DefaultValidationConfig(), okHandler())

	req := httptest.NewRequest(http.MethodDelete, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("DELETE request: expected 200, got %d", rec.Code)
	}
}

func TestContentTypeAllowed(t *testing.T) {
	tests := []struct {
		ct      string
		allowed []string
		want    bool
	}{
		{"application/json", []string{"application/json"}, true},
		{"application/json; charset=utf-8", []string{"application/json"}, true},
		{"text/plain", []string{"application/json"}, false},
		{"APPLICATION/JSON", []string{"application/json"}, true},
		{"", []string{"application/json"}, false},
	}

	for _, tc := range tests {
		got := contentTypeAllowed(tc.ct, tc.allowed)
		if got != tc.want {
			t.Errorf("contentTypeAllowed(%q, %v) = %v, want %v", tc.ct, tc.allowed, got, tc.want)
		}
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"APPLICATION/JSON", true},
		{"text/plain", false},
		{"", false},
	}

	for _, tc := range tests {
		got := isJSONContentType(tc.ct)
		if got != tc.want {
			t.Errorf("isJSONContentType(%q) = %v, want %v", tc.ct, got, tc.want)
		}
	}
}
