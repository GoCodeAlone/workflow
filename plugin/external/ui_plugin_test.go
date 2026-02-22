package external

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

// ---- helpers ----

// setupPluginDir creates a temporary plugins root and returns its path.
func setupPluginDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// makeUIPlugin creates a subdirectory under root for the named plugin with
// the given ui.json content, and optionally writes asset files.
func makeUIPlugin(t *testing.T, root, name string, manifest UIManifest, assets map[string]string) {
	t.Helper()
	pluginDir := filepath.Join(root, name)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin dir: %v", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "ui.json"), data, 0o600); err != nil {
		t.Fatalf("write ui.json: %v", err)
	}

	if len(assets) > 0 {
		assetDir := manifest.AssetDir
		if assetDir == "" {
			assetDir = "assets"
		}
		assetsPath := filepath.Join(pluginDir, assetDir)
		if err := os.MkdirAll(assetsPath, 0o755); err != nil {
			t.Fatalf("create assets dir: %v", err)
		}
		for filename, content := range assets {
			if err := os.WriteFile(filepath.Join(assetsPath, filename), []byte(content), 0o600); err != nil {
				t.Fatalf("write asset %s: %v", filename, err)
			}
		}
	}
}

// ---- UIPluginManager tests ----

func TestUIPluginManager_DiscoverPlugins(t *testing.T) {
	root := setupPluginDir(t)

	// Plugin with ui.json
	makeUIPlugin(t, root, "alpha", UIManifest{Name: "alpha", Version: "1.0.0"}, nil)
	// Plugin without ui.json (should not be discovered)
	if err := os.MkdirAll(filepath.Join(root, "no-ui"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Another valid plugin
	makeUIPlugin(t, root, "beta", UIManifest{Name: "beta", Version: "2.0.0"}, nil)

	mgr := NewUIPluginManager(root, nil)
	names, err := mgr.DiscoverPlugins()
	if err != nil {
		t.Fatalf("DiscoverPlugins: %v", err)
	}
	sort.Strings(names)

	want := []string{"alpha", "beta"}
	if len(names) != len(want) {
		t.Fatalf("expected plugins %v, got %v", want, names)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("names[%d]: want %q, got %q", i, want[i], n)
		}
	}
}

func TestUIPluginManager_DiscoverPlugins_EmptyDir(t *testing.T) {
	root := setupPluginDir(t)
	mgr := NewUIPluginManager(root, nil)
	names, err := mgr.DiscoverPlugins()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected no plugins, got %v", names)
	}
}

func TestUIPluginManager_DiscoverPlugins_NonexistentDir(t *testing.T) {
	mgr := NewUIPluginManager("/nonexistent/path", nil)
	names, err := mgr.DiscoverPlugins()
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if names != nil {
		t.Errorf("expected nil names, got %v", names)
	}
}

func TestUIPluginManager_LoadPlugin(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "my-plugin", UIManifest{
		Name:        "my-plugin",
		Version:     "1.2.3",
		Description: "A test plugin",
		NavItems: []UINavItem{
			{ID: "my-page", Label: "My Page", Icon: "🔌", Category: "plugin", Order: 5},
		},
	}, nil)

	mgr := NewUIPluginManager(root, nil)
	if err := mgr.LoadPlugin("my-plugin"); err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	if !mgr.IsLoaded("my-plugin") {
		t.Error("plugin should be loaded")
	}

	entry, ok := mgr.GetPlugin("my-plugin")
	if !ok {
		t.Fatal("GetPlugin returned false")
	}
	if entry.Manifest.Version != "1.2.3" {
		t.Errorf("version: want 1.2.3, got %s", entry.Manifest.Version)
	}
	if len(entry.Manifest.NavItems) != 1 {
		t.Errorf("nav items: want 1, got %d", len(entry.Manifest.NavItems))
	}
}

func TestUIPluginManager_LoadPlugin_DefaultsAssetDir(t *testing.T) {
	root := setupPluginDir(t)
	// Manifest with no AssetDir set
	makeUIPlugin(t, root, "plugin-a", UIManifest{Name: "plugin-a", Version: "1.0.0"}, nil)

	mgr := NewUIPluginManager(root, nil)
	if err := mgr.LoadPlugin("plugin-a"); err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}

	entry, ok := mgr.GetPlugin("plugin-a")
	if !ok {
		t.Fatal("plugin not found")
	}
	wantAssetsDir := filepath.Join(root, "plugin-a", "assets")
	if entry.AssetsDir != wantAssetsDir {
		t.Errorf("AssetsDir: want %q, got %q", wantAssetsDir, entry.AssetsDir)
	}
}

func TestUIPluginManager_LoadPlugin_NotFound(t *testing.T) {
	root := setupPluginDir(t)
	mgr := NewUIPluginManager(root, nil)
	err := mgr.LoadPlugin("nonexistent")
	if err == nil {
		t.Error("expected error for missing ui.json, got nil")
	}
}

func TestUIPluginManager_UnloadPlugin(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "remove-me", UIManifest{Name: "remove-me", Version: "1.0.0"}, nil)

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("remove-me")

	if err := mgr.UnloadPlugin("remove-me"); err != nil {
		t.Fatalf("UnloadPlugin: %v", err)
	}
	if mgr.IsLoaded("remove-me") {
		t.Error("plugin should no longer be loaded")
	}
}

func TestUIPluginManager_UnloadPlugin_NotLoaded(t *testing.T) {
	root := setupPluginDir(t)
	mgr := NewUIPluginManager(root, nil)
	if err := mgr.UnloadPlugin("ghost"); err == nil {
		t.Error("expected error when unloading a non-loaded plugin")
	}
}

func TestUIPluginManager_ReloadPlugin(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "reload-me", UIManifest{Name: "reload-me", Version: "1.0.0"}, nil)

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("reload-me")

	// Update the manifest on disk.
	makeUIPlugin(t, root, "reload-me", UIManifest{Name: "reload-me", Version: "2.0.0"}, nil)

	if err := mgr.ReloadPlugin("reload-me"); err != nil {
		t.Fatalf("ReloadPlugin: %v", err)
	}

	entry, _ := mgr.GetPlugin("reload-me")
	if entry.Manifest.Version != "2.0.0" {
		t.Errorf("after reload version should be 2.0.0, got %s", entry.Manifest.Version)
	}
}

func TestUIPluginManager_LoadedPlugins(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "a", UIManifest{Name: "a", Version: "1.0.0"}, nil)
	makeUIPlugin(t, root, "b", UIManifest{Name: "b", Version: "1.0.0"}, nil)

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("a")
	_ = mgr.LoadPlugin("b")

	loaded := mgr.LoadedPlugins()
	sort.Strings(loaded)
	if len(loaded) != 2 || loaded[0] != "a" || loaded[1] != "b" {
		t.Errorf("LoadedPlugins: want [a b], got %v", loaded)
	}
}

func TestUIPluginManager_ServeAssets(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "static-plugin", UIManifest{Name: "static-plugin", Version: "1.0.0"},
		map[string]string{
			"hello.txt": "world",
		})

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("static-plugin")

	h := mgr.ServeAssets("static-plugin")
	if h == nil {
		t.Fatal("ServeAssets returned nil for loaded plugin")
	}

	req := httptest.NewRequest(http.MethodGet, "/hello.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("serve asset: want 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "world" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestUIPluginManager_ServeAssets_NotLoaded(t *testing.T) {
	mgr := NewUIPluginManager(t.TempDir(), nil)
	if h := mgr.ServeAssets("nonexistent"); h != nil {
		t.Error("expected nil handler for unloaded plugin")
	}
}

func TestUIPluginManager_UIPages(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "nav-plugin", UIManifest{
		Name:    "nav-plugin",
		Version: "1.0.0",
		NavItems: []UINavItem{
			{ID: "nav1", Label: "Nav1", Icon: "🌍", Category: "global", Order: 1},
			{ID: "nav2", Label: "Nav2", Order: 2}, // no category → defaults to "plugin"
		},
	}, nil)

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("nav-plugin")

	pages := mgr.UIPages("nav-plugin")
	if len(pages) != 2 {
		t.Fatalf("want 2 UIPageDef, got %d", len(pages))
	}
	if pages[0].Category != "global" {
		t.Errorf("page 0 category: want global, got %s", pages[0].Category)
	}
	if pages[1].Category != "plugin" {
		t.Errorf("page 1 category: want plugin (default), got %s", pages[1].Category)
	}
}

func TestUIPluginManager_UIPages_NotLoaded(t *testing.T) {
	mgr := NewUIPluginManager(t.TempDir(), nil)
	if pages := mgr.UIPages("ghost"); pages != nil {
		t.Errorf("expected nil for unloaded plugin, got %v", pages)
	}
}

func TestUIPluginManager_AsNativePlugin(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "native-wrap", UIManifest{
		Name:        "native-wrap",
		Version:     "3.0.0",
		Description: "wrapped",
		NavItems: []UINavItem{
			{ID: "nw-page", Label: "NW Page", Category: "tools"},
		},
	}, nil)

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("native-wrap")

	np := mgr.AsNativePlugin("native-wrap")
	if np == nil {
		t.Fatal("AsNativePlugin returned nil")
	}
	if np.Name() != "native-wrap" {
		t.Errorf("Name: want native-wrap, got %s", np.Name())
	}
	if np.Version() != "3.0.0" {
		t.Errorf("Version: want 3.0.0, got %s", np.Version())
	}
	pages := np.UIPages()
	if len(pages) != 1 {
		t.Fatalf("UIPages: want 1, got %d", len(pages))
	}
	if pages[0].Category != "tools" {
		t.Errorf("category: want tools, got %s", pages[0].Category)
	}
}

func TestUIPluginManager_AsNativePlugin_NotLoaded(t *testing.T) {
	mgr := NewUIPluginManager(t.TempDir(), nil)
	if np := mgr.AsNativePlugin("ghost"); np != nil {
		t.Error("expected nil for unloaded plugin")
	}
}

// UIPluginNativePlugin hot-reload: updating the plugin on disk and reloading
// should be reflected through the NativePlugin.UIPages() without re-registering.
func TestUIPluginNativePlugin_HotReload_UpdatesUIPages(t *testing.T) {
	root := setupPluginDir(t)
	makeUIPlugin(t, root, "hot-plugin", UIManifest{
		Name:    "hot-plugin",
		Version: "1.0.0",
		NavItems: []UINavItem{
			{ID: "old-page", Label: "Old Page"},
		},
	}, nil)

	mgr := NewUIPluginManager(root, nil)
	_ = mgr.LoadPlugin("hot-plugin")
	np := mgr.AsNativePlugin("hot-plugin")

	// Before reload: one page
	if len(np.UIPages()) != 1 {
		t.Fatalf("initial UIPages: want 1, got %d", len(np.UIPages()))
	}

	// Update manifest on disk with two nav items.
	makeUIPlugin(t, root, "hot-plugin", UIManifest{
		Name:    "hot-plugin",
		Version: "1.1.0",
		NavItems: []UINavItem{
			{ID: "old-page", Label: "Old Page"},
			{ID: "new-page", Label: "New Page"},
		},
	}, nil)
	_ = mgr.ReloadPlugin("hot-plugin")

	// After reload: two pages — no re-registration needed.
	if len(np.UIPages()) != 2 {
		t.Errorf("after reload UIPages: want 2, got %d", len(np.UIPages()))
	}
}

// ---- UIPluginHandler HTTP tests ----

func newTestUIPluginHandler(t *testing.T) (*UIPluginHandler, *UIPluginManager, string) {
	t.Helper()
	root := setupPluginDir(t)
	mgr := NewUIPluginManager(root, nil)
	return NewUIPluginHandler(mgr), mgr, root
}

func TestUIPluginHandler_ListLoaded_Empty(t *testing.T) {
	h, _, _ := newTestUIPluginHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ui", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status: want ok, got %s", resp.Status)
	}
}

func TestUIPluginHandler_ListAvailable(t *testing.T) {
	h, _, root := newTestUIPluginHandler(t)
	makeUIPlugin(t, root, "discovered", UIManifest{Name: "discovered", Version: "1.0.0"}, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ui/available", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestUIPluginHandler_LoadAndReload(t *testing.T) {
	h, mgr, root := newTestUIPluginHandler(t)
	makeUIPlugin(t, root, "loadable", UIManifest{Name: "loadable", Version: "1.0.0"}, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Load
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/ui/loadable/load", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("load: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if !mgr.IsLoaded("loadable") {
		t.Error("plugin should be loaded after load request")
	}

	// Update manifest and reload
	makeUIPlugin(t, root, "loadable", UIManifest{Name: "loadable", Version: "2.0.0"}, nil)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plugins/ui/loadable/reload", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reload: want 200, got %d: %s", w.Code, w.Body.String())
	}

	entry, _ := mgr.GetPlugin("loadable")
	if entry.Manifest.Version != "2.0.0" {
		t.Errorf("after reload version should be 2.0.0, got %s", entry.Manifest.Version)
	}
}

func TestUIPluginHandler_Unload(t *testing.T) {
	h, mgr, root := newTestUIPluginHandler(t)
	makeUIPlugin(t, root, "removable", UIManifest{Name: "removable", Version: "1.0.0"}, nil)
	_ = mgr.LoadPlugin("removable")

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/ui/removable/unload", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unload: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if mgr.IsLoaded("removable") {
		t.Error("plugin should not be loaded after unload request")
	}
}

func TestUIPluginHandler_GetManifest(t *testing.T) {
	h, mgr, root := newTestUIPluginHandler(t)
	makeUIPlugin(t, root, "manifest-plugin", UIManifest{
		Name: "manifest-plugin", Version: "9.9.9",
	}, nil)
	_ = mgr.LoadPlugin("manifest-plugin")

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ui/manifest-plugin/manifest", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestUIPluginHandler_GetManifest_NotFound(t *testing.T) {
	h, _, _ := newTestUIPluginHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ui/ghost/manifest", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestUIPluginHandler_ServeAssets(t *testing.T) {
	h, mgr, root := newTestUIPluginHandler(t)
	makeUIPlugin(t, root, "asset-plugin", UIManifest{Name: "asset-plugin", Version: "1.0.0"},
		map[string]string{"hello.txt": "world"})
	_ = mgr.LoadPlugin("asset-plugin")

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ui/asset-plugin/assets/hello.txt", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "world" {
		t.Errorf("asset body: want %q, got %q", "world", got)
	}
}

func TestUIPluginHandler_ServeAssets_NotLoaded(t *testing.T) {
	h, _, _ := newTestUIPluginHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ui/ghost/assets/file.txt", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

// ---- NativePlugin compile-time interface check ----

var _ plugin.NativePlugin = (*UIPluginNativePlugin)(nil)
