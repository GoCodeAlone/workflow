package schema

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGetSchema(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/schema+json" {
		t.Errorf("expected application/schema+json, got %q", ct)
	}

	var s Schema
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if s.Title != "Workflow Configuration" {
		t.Errorf("expected title 'Workflow Configuration', got %q", s.Title)
	}
	if s.Schema != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("unexpected $schema: %q", s.Schema)
	}
	if s.Properties["modules"] == nil {
		t.Error("modules property missing from response")
	}
}

func TestHandleGetSchema_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/schema", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Go 1.22+ mux returns 405 for wrong method on pattern-matched routes
	if rec.Code == http.StatusOK {
		t.Error("POST should not succeed on schema endpoint")
	}
}
