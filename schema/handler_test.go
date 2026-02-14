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

func TestHandleGetModuleSchemas_All(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/module-schemas", nil)
	rec := httptest.NewRecorder()
	HandleGetModuleSchemas(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var schemas map[string]*ModuleSchema
	if err := json.NewDecoder(rec.Body).Decode(&schemas); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(schemas) == 0 {
		t.Error("expected non-empty schemas map")
	}
	// Verify a specific module type is present
	if s, ok := schemas["http.server"]; !ok {
		t.Error("expected http.server schema in response")
	} else if s.Label != "HTTP Server" {
		t.Errorf("expected label 'HTTP Server', got %q", s.Label)
	}
}

func TestHandleGetModuleSchemas_ByType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/module-schemas?type=http.middleware.ratelimit", nil)
	rec := httptest.NewRecorder()
	HandleGetModuleSchemas(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var s ModuleSchema
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if s.Type != "http.middleware.ratelimit" {
		t.Errorf("expected type 'http.middleware.ratelimit', got %q", s.Type)
	}
	// Verify the correct field names (not the old wrong ones)
	fieldKeys := make(map[string]bool)
	for _, f := range s.ConfigFields {
		fieldKeys[f.Key] = true
	}
	if !fieldKeys["requestsPerMinute"] {
		t.Error("expected 'requestsPerMinute' field, not 'rps'")
	}
	if !fieldKeys["burstSize"] {
		t.Error("expected 'burstSize' field, not 'burst'")
	}
}

func TestHandleGetModuleSchemas_UnknownType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/module-schemas?type=nonexistent.module", nil)
	rec := httptest.NewRecorder()
	HandleGetModuleSchemas(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleSchemaAPI_Dispatch(t *testing.T) {
	// Test dispatching to /api/schema
	req := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	rec := httptest.NewRecorder()
	HandleSchemaAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/schema, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/schema+json" {
		t.Errorf("expected application/schema+json, got %q", ct)
	}

	// Test dispatching to /api/v1/module-schemas
	req = httptest.NewRequest(http.MethodGet, "/api/v1/module-schemas", nil)
	rec = httptest.NewRecorder()
	HandleSchemaAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/v1/module-schemas, got %d", rec.Code)
	}
	ct = rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
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
