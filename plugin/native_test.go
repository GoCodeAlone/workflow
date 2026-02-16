package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockNativePlugin implements NativePlugin for testing.
type mockNativePlugin struct {
	name        string
	version     string
	description string
	uiPages     []UIPageDef
}

func (m *mockNativePlugin) Name() string         { return m.name }
func (m *mockNativePlugin) Version() string      { return m.version }
func (m *mockNativePlugin) Description() string  { return m.description }
func (m *mockNativePlugin) UIPages() []UIPageDef { return m.uiPages }
func (m *mockNativePlugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/tables", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"endpoint": "tables"})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func newTestPlugin(name, version, desc string) *mockNativePlugin {
	return &mockNativePlugin{
		name:        name,
		version:     version,
		description: desc,
		uiPages: []UIPageDef{
			{ID: name, Label: desc, Icon: "database", Category: "tools"},
		},
	}
}

func TestNativeRegistryRegisterAndGet(t *testing.T) {
	reg := NewNativeRegistry()
	p := newTestPlugin("store-browser", "1.0.0", "Browse stores")

	reg.Register(p)

	got, ok := reg.Get("store-browser")
	if !ok {
		t.Fatal("expected plugin to be found")
	}
	if got.Name() != "store-browser" {
		t.Errorf("got name %q, want %q", got.Name(), "store-browser")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent plugin to not be found")
	}
}

func TestNativeRegistryList(t *testing.T) {
	reg := NewNativeRegistry()
	reg.Register(newTestPlugin("charlie", "1.0.0", "C"))
	reg.Register(newTestPlugin("alpha", "1.0.0", "A"))
	reg.Register(newTestPlugin("bravo", "1.0.0", "B"))

	plugins := reg.List()
	if len(plugins) != 3 {
		t.Fatalf("got %d plugins, want 3", len(plugins))
	}
	if plugins[0].Name() != "alpha" {
		t.Errorf("got first plugin %q, want %q", plugins[0].Name(), "alpha")
	}
	if plugins[1].Name() != "bravo" {
		t.Errorf("got second plugin %q, want %q", plugins[1].Name(), "bravo")
	}
	if plugins[2].Name() != "charlie" {
		t.Errorf("got third plugin %q, want %q", plugins[2].Name(), "charlie")
	}
}

func TestNativeRegistryUIPages(t *testing.T) {
	reg := NewNativeRegistry()
	reg.Register(&mockNativePlugin{
		name: "zz-plugin", version: "1.0.0", description: "ZZ",
		uiPages: []UIPageDef{{ID: "zz-page", Label: "ZZ Page", Icon: "star", Category: "tools"}},
	})
	reg.Register(&mockNativePlugin{
		name: "aa-plugin", version: "1.0.0", description: "AA",
		uiPages: []UIPageDef{{ID: "aa-page", Label: "AA Page", Icon: "box", Category: "admin"}},
	})

	pages := reg.UIPages()
	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(pages))
	}
	if pages[0].ID != "aa-page" {
		t.Errorf("got first page ID %q, want %q", pages[0].ID, "aa-page")
	}
	if pages[1].ID != "zz-page" {
		t.Errorf("got second page ID %q, want %q", pages[1].ID, "zz-page")
	}
}

func TestNativeHandlerListPlugins(t *testing.T) {
	reg := NewNativeRegistry()
	reg.Register(newTestPlugin("store-browser", "1.0.0", "Browse database tables"))

	handler := NewNativeHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("got Content-Type %q, want %q", ct, "application/json")
	}

	var result []nativePluginInfo
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d plugins, want 1", len(result))
	}
	if result[0].Name != "store-browser" {
		t.Errorf("got name %q, want %q", result[0].Name, "store-browser")
	}
	if result[0].Version != "1.0.0" {
		t.Errorf("got version %q, want %q", result[0].Version, "1.0.0")
	}
	if len(result[0].UIPages) != 1 {
		t.Fatalf("got %d ui_pages, want 1", len(result[0].UIPages))
	}
}

func TestNativeHandlerRouteToPlugin(t *testing.T) {
	reg := NewNativeRegistry()
	reg.Register(newTestPlugin("store-browser", "1.0.0", "Browse stores"))

	handler := NewNativeHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/store-browser/tables", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["endpoint"] != "tables" {
		t.Errorf("got endpoint %q, want %q", result["endpoint"], "tables")
	}
}

func TestNativeHandlerRouteToPluginHealth(t *testing.T) {
	reg := NewNativeRegistry()
	reg.Register(newTestPlugin("store-browser", "1.0.0", "Browse stores"))

	handler := NewNativeHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/store-browser/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("got status %q, want %q", result["status"], "ok")
	}
}

func TestNativeHandlerNotFound(t *testing.T) {
	reg := NewNativeRegistry()
	handler := NewNativeHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/nonexistent/tables", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestNativeHandlerMethodNotAllowed(t *testing.T) {
	reg := NewNativeRegistry()
	handler := NewNativeHandler(reg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
