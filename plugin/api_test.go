package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIHandlerListPlugins(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("plugin-a", "1.0.0"), nil, "")
	_ = r.Register(validManifest("plugin-b", "2.0.0"), nil, "")

	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/plugins status = %d, want %d", w.Code, http.StatusOK)
	}

	var result []pluginListEntry
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(result))
	}
}

func TestAPIHandlerGetPlugin(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("my-plugin", "1.0.0"), nil, "")

	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/my-plugin", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/plugins/my-plugin status = %d, want %d", w.Code, http.StatusOK)
	}

	var m PluginManifest
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if m.Name != "my-plugin" {
		t.Errorf("Name = %q, want %q", m.Name, "my-plugin")
	}
}

func TestAPIHandlerGetPluginNotFound(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIHandlerRegisterPlugin(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := registerPluginRequest{
		Manifest: validManifest("new-plugin", "1.0.0"),
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/plugins", bytes.NewReader(data))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("POST /api/plugins status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Verify it was registered
	_, ok := r.Get("new-plugin")
	if !ok {
		t.Error("expected plugin to be registered")
	}
}

func TestAPIHandlerRegisterPluginInvalidJSON(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/plugins", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIHandlerRegisterPluginNoManifest(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	data, _ := json.Marshal(map[string]any{"source": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/plugins", bytes.NewReader(data))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIHandlerRegisterPluginInvalidManifest(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := registerPluginRequest{
		Manifest: &PluginManifest{Name: ""}, // invalid
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/plugins", bytes.NewReader(data))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestAPIHandlerDeletePlugin(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("del-plugin", "1.0.0"), nil, "")

	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/plugins/del-plugin", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("DELETE status = %d, want %d", w.Code, http.StatusNoContent)
	}

	_, ok := r.Get("del-plugin")
	if ok {
		t.Error("expected plugin to be removed")
	}
}

func TestAPIHandlerDeletePluginNotFound(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/plugins/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIHandlerMethodNotAllowed(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/plugins", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH /api/plugins status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIHandlerPluginByNameMethodNotAllowed(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/plugins/test", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH /api/plugins/test status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIHandlerPluginByNameEmpty(t *testing.T) {
	r := NewLocalRegistry()
	h := NewAPIHandler(r, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/plugins/ status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
