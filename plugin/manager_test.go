package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- PluginManager additional coverage tests ---

func TestPluginManager_NilDB(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	p := newSimplePlugin("test", "1.0.0", "Test plugin")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Enable("test"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !pm.IsEnabled("test") {
		t.Error("expected plugin to be enabled")
	}

	// Disable should also work without DB
	if err := pm.Disable("test"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
}

func TestPluginManager_RestoreState(t *testing.T) {
	db := openTestDB(t)

	// First manager: register, enable, and persist
	pm1 := NewPluginManager(db, nil)
	p1 := newSimplePlugin("persistent-plugin", "1.0.0", "Persistent")
	if err := pm1.Register(p1); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm1.Enable("persistent-plugin"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// Second manager: register same plugin, then restore state
	pm2 := NewPluginManager(db, nil)
	p2 := newSimplePlugin("persistent-plugin", "1.0.0", "Persistent")
	if err := pm2.Register(p2); err != nil {
		t.Fatalf("Register in pm2: %v", err)
	}

	if err := pm2.RestoreState(); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
	if !pm2.IsEnabled("persistent-plugin") {
		t.Error("expected plugin to be restored as enabled")
	}
}

func TestPluginManager_RestoreState_NilDB(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	if err := pm.RestoreState(); err != nil {
		t.Fatalf("RestoreState with nil DB should succeed: %v", err)
	}
}

func TestPluginManager_AllPlugins(t *testing.T) {
	db := openTestDB(t)

	pm := NewPluginManager(db, nil)
	p1 := newSimplePlugin("alpha", "1.0.0", "Alpha plugin")
	p2 := newSimplePlugin("beta", "2.0.0", "Beta plugin")
	if err := pm.Register(p1); err != nil {
		t.Fatalf("Register p1: %v", err)
	}
	if err := pm.Register(p2); err != nil {
		t.Fatalf("Register p2: %v", err)
	}
	if err := pm.Enable("alpha"); err != nil {
		t.Fatalf("Enable alpha: %v", err)
	}

	all := pm.AllPlugins()
	if len(all) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(all))
	}

	// Sorted by name
	if all[0].Name != "alpha" {
		t.Errorf("expected first plugin 'alpha', got %q", all[0].Name)
	}
	if !all[0].Enabled {
		t.Error("expected alpha to be enabled")
	}
	if all[1].Enabled {
		t.Error("expected beta to be disabled")
	}
}

func TestPluginManager_EnabledPlugins(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	_ = pm.Register(newSimplePlugin("a", "1.0.0", "A"))
	_ = pm.Register(newSimplePlugin("b", "1.0.0", "B"))
	_ = pm.Register(newSimplePlugin("c", "1.0.0", "C"))
	_ = pm.Enable("a")
	_ = pm.Enable("c")

	enabled := pm.EnabledPlugins()
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled, got %d", len(enabled))
	}
	if enabled[0].Name() != "a" || enabled[1].Name() != "c" {
		t.Errorf("unexpected enabled plugins: %s, %s", enabled[0].Name(), enabled[1].Name())
	}
}

func TestPluginManager_Enable_NotRegistered(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	if err := pm.Enable("ghost"); err == nil {
		t.Fatal("expected error enabling unregistered plugin")
	}
}

func TestPluginManager_Disable_NotRegistered(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	if err := pm.Disable("ghost"); err == nil {
		t.Fatal("expected error disabling unregistered plugin")
	}
}

func TestPluginManager_Disable_AlreadyDisabled(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	_ = pm.Register(newSimplePlugin("off", "1.0.0", "Off"))

	// Disable when already disabled should be no-op
	if err := pm.Disable("off"); err != nil {
		t.Fatalf("Disable already-disabled should not error: %v", err)
	}
}

func TestPluginManager_RegisterEmptyName(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	p := &testPlugin{name: "", version: "1.0.0"}
	if err := pm.Register(p); err == nil {
		t.Fatal("expected error for empty plugin name")
	}
}

func TestPluginManager_SetContext(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	ctx := PluginContext{DataDir: "/test"}
	pm.SetContext(ctx)
	// No panic means success — internal state only
}

func TestPluginManager_EnableWithVersionConstraint(t *testing.T) {
	db := openTestDB(t)

	pm := NewPluginManager(db, nil)
	base := newSimplePlugin("base-lib", "2.0.0", "Base library")
	dep := newPluginWithDeps("consumer", "1.0.0",
		PluginDependency{Name: "base-lib", MinVersion: "1.5.0"})

	_ = pm.Register(base)
	_ = pm.Register(dep)

	if err := pm.Enable("consumer"); err != nil {
		t.Fatalf("Enable with valid version constraint: %v", err)
	}
}

func TestPluginManager_EnableWithVersionConstraint_Failure(t *testing.T) {
	db := openTestDB(t)

	pm := NewPluginManager(db, nil)
	base := newSimplePlugin("base-lib", "1.0.0", "Base library")
	dep := newPluginWithDeps("consumer", "1.0.0",
		PluginDependency{Name: "base-lib", MinVersion: "2.0.0"})

	_ = pm.Register(base)
	_ = pm.Register(dep)

	if err := pm.Enable("consumer"); err == nil {
		t.Fatal("expected error: version constraint not satisfied")
	}
}

func TestPluginManager_OnEnableError(t *testing.T) {
	t.Parallel()

	pm := NewPluginManager(nil, nil)
	p := newSimplePlugin("failing", "1.0.0", "Failing plugin")
	p.onEnableFn = func(_ PluginContext) error {
		return http.ErrServerClosed // any error
	}

	_ = pm.Register(p)
	if err := pm.Enable("failing"); err == nil {
		t.Fatal("expected error from OnEnable failure")
	}
	if pm.IsEnabled("failing") {
		t.Error("plugin should not be enabled after OnEnable failure")
	}
}

// --- ServeHTTP tests ---

func TestPluginManager_ServeHTTP_ListPlugins(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	_ = pm.Register(newSimplePlugin("my-plugin", "1.0.0", "My Plugin"))
	_ = pm.Enable("my-plugin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var plugins []PluginInfo
	if err := json.NewDecoder(rec.Body).Decode(&plugins); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(plugins))
	}
}

func TestPluginManager_ServeHTTP_ListPlugins_MethodNotAllowed(t *testing.T) {
	pm := NewPluginManager(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestPluginManager_ServeHTTP_Enable(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	_ = pm.Register(newSimplePlugin("enab", "1.0.0", "Enable test"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/enab/enable", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !pm.IsEnabled("enab") {
		t.Error("expected plugin to be enabled via HTTP")
	}
}

func TestPluginManager_ServeHTTP_Disable(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	_ = pm.Register(newSimplePlugin("dis", "1.0.0", "Disable test"))
	_ = pm.Enable("dis")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/dis/disable", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if pm.IsEnabled("dis") {
		t.Error("expected plugin to be disabled via HTTP")
	}
}

func TestPluginManager_ServeHTTP_NotFound(t *testing.T) {
	pm := NewPluginManager(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/nonexistent/health", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestPluginManager_ServeHTTP_PluginRoute(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	p := newSimplePlugin("routed", "1.0.0", "Routed plugin")
	_ = pm.Register(p)
	_ = pm.Enable("routed")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/routed/health", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginManager_ServeHTTP_BadPrefix(t *testing.T) {
	pm := NewPluginManager(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/some/random/path", nil)
	rec := httptest.NewRecorder()
	pm.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- NativeHandler tests ---

func TestNativeHandler_ServeHTTP(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	_ = pm.Register(newSimplePlugin("nh-test", "1.0.0", "NativeHandler test"))
	_ = pm.Enable("nh-test")

	h := NewNativeHandler(pm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- DisableOrder with dependents ---

func TestPluginManager_DisableWithDependents(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	a := newSimplePlugin("core", "1.0.0", "Core")
	b := newPluginWithDeps("ext", "1.0.0", PluginDependency{Name: "core"})

	_ = pm.Register(a)
	_ = pm.Register(b)
	_ = pm.Enable("ext") // auto-enables "core"

	if !pm.IsEnabled("core") || !pm.IsEnabled("ext") {
		t.Fatal("both should be enabled")
	}

	// Disabling core should also disable ext
	if err := pm.Disable("core"); err != nil {
		t.Fatalf("Disable core: %v", err)
	}
	if pm.IsEnabled("ext") {
		t.Error("dependent ext should have been disabled")
	}
	if pm.IsEnabled("core") {
		t.Error("core should be disabled")
	}
}
