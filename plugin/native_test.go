package plugin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// testPlugin implements NativePlugin for testing.
type testPlugin struct {
	name         string
	version      string
	description  string
	uiPages      []UIPageDef
	deps         []PluginDependency
	onEnableFn   func(ctx PluginContext) error
	onDisableFn  func(ctx PluginContext) error
	enableCount  int
	disableCount int
}

func (p *testPlugin) Name() string                     { return p.name }
func (p *testPlugin) Version() string                  { return p.version }
func (p *testPlugin) Description() string              { return p.description }
func (p *testPlugin) Dependencies() []PluginDependency { return p.deps }
func (p *testPlugin) UIPages() []UIPageDef             { return p.uiPages }

func (p *testPlugin) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/tables", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"plugin": p.name, "endpoint": "tables"})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func (p *testPlugin) OnEnable(ctx PluginContext) error {
	p.enableCount++
	if p.onEnableFn != nil {
		return p.onEnableFn(ctx)
	}
	return nil
}

func (p *testPlugin) OnDisable(ctx PluginContext) error {
	p.disableCount++
	if p.onDisableFn != nil {
		return p.onDisableFn(ctx)
	}
	return nil
}

func newSimplePlugin(name, version, desc string) *testPlugin {
	return &testPlugin{
		name:        name,
		version:     version,
		description: desc,
		uiPages: []UIPageDef{
			{ID: name, Label: desc, Icon: "database", Category: "tools"},
		},
	}
}

func newPluginWithDeps(name, version string, deps ...PluginDependency) *testPlugin {
	return &testPlugin{
		name:        name,
		version:     version,
		description: name + " plugin",
		deps:        deps,
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestManager(t *testing.T) *PluginManager {
	t.Helper()
	db := openTestDB(t)
	return NewPluginManager(db, nil)
}

// --- Register + Enable Tests ---

func TestRegisterAndEnable(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("store-browser", "1.0.0", "Browse stores")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if pm.IsEnabled("store-browser") {
		t.Error("plugin should not be enabled after registration")
	}

	if err := pm.Enable("store-browser"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	if !pm.IsEnabled("store-browser") {
		t.Error("plugin should be enabled after Enable()")
	}

	if p.enableCount != 1 {
		t.Errorf("OnEnable called %d times, want 1", p.enableCount)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	pm := newTestManager(t)
	p1 := newSimplePlugin("foo", "1.0.0", "Foo")
	p2 := newSimplePlugin("foo", "2.0.0", "Foo v2")

	if err := pm.Register(p1); err != nil {
		t.Fatalf("Register p1: %v", err)
	}
	if err := pm.Register(p2); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegisterNil(t *testing.T) {
	pm := newTestManager(t)
	if err := pm.Register(nil); err == nil {
		t.Error("expected error for nil plugin")
	}
}

// --- Enable with Dependencies ---

func TestEnableWithDependencies(t *testing.T) {
	pm := newTestManager(t)

	// C depends on B, B depends on A
	a := newPluginWithDeps("a-base", "1.0.0")
	b := newPluginWithDeps("b-middle", "1.0.0", PluginDependency{Name: "a-base"})
	c := newPluginWithDeps("c-top", "1.0.0", PluginDependency{Name: "b-middle"})

	// Register all
	for _, p := range []NativePlugin{a, b, c} {
		if err := pm.Register(p); err != nil {
			t.Fatalf("Register %s: %v", p.Name(), err)
		}
	}

	// Enable C — should auto-enable A and B first
	if err := pm.Enable("c-top"); err != nil {
		t.Fatalf("Enable c-top: %v", err)
	}

	for _, name := range []string{"a-base", "b-middle", "c-top"} {
		if !pm.IsEnabled(name) {
			t.Errorf("expected %s to be enabled", name)
		}
	}

	if a.enableCount != 1 {
		t.Errorf("a.enableCount = %d, want 1", a.enableCount)
	}
	if b.enableCount != 1 {
		t.Errorf("b.enableCount = %d, want 1", b.enableCount)
	}
	if c.enableCount != 1 {
		t.Errorf("c.enableCount = %d, want 1", c.enableCount)
	}
}

func TestEnableAlreadyEnabledDep(t *testing.T) {
	pm := newTestManager(t)

	a := newPluginWithDeps("alpha", "1.0.0")
	b := newPluginWithDeps("beta", "1.0.0", PluginDependency{Name: "alpha"})

	for _, p := range []NativePlugin{a, b} {
		if err := pm.Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	// Enable alpha first
	if err := pm.Enable("alpha"); err != nil {
		t.Fatalf("Enable alpha: %v", err)
	}

	// Enable beta — alpha should not get OnEnable called again
	if err := pm.Enable("beta"); err != nil {
		t.Fatalf("Enable beta: %v", err)
	}

	if a.enableCount != 1 {
		t.Errorf("alpha.enableCount = %d, want 1 (should not be re-enabled)", a.enableCount)
	}
}

func TestEnableVersionConstraint(t *testing.T) {
	pm := newTestManager(t)

	old := newPluginWithDeps("dep-lib", "0.9.0")
	consumer := newPluginWithDeps("consumer", "1.0.0", PluginDependency{Name: "dep-lib", MinVersion: "1.0.0"})

	if err := pm.Register(old); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Register(consumer); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := pm.Enable("consumer"); err == nil {
		t.Error("expected error due to version constraint not satisfied")
	}
}

// --- Disable with Dependents ---

func TestDisableWithDependents(t *testing.T) {
	pm := newTestManager(t)

	a := newPluginWithDeps("a-base", "1.0.0")
	b := newPluginWithDeps("b-middle", "1.0.0", PluginDependency{Name: "a-base"})
	c := newPluginWithDeps("c-top", "1.0.0", PluginDependency{Name: "b-middle"})

	for _, p := range []NativePlugin{a, b, c} {
		if err := pm.Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	// Enable all
	if err := pm.Enable("c-top"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// Disable A — should cascade disable B and C first
	if err := pm.Disable("a-base"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	for _, name := range []string{"a-base", "b-middle", "c-top"} {
		if pm.IsEnabled(name) {
			t.Errorf("expected %s to be disabled", name)
		}
	}

	// C and B should have been disabled before A
	if c.disableCount != 1 {
		t.Errorf("c.disableCount = %d, want 1", c.disableCount)
	}
	if b.disableCount != 1 {
		t.Errorf("b.disableCount = %d, want 1", b.disableCount)
	}
	if a.disableCount != 1 {
		t.Errorf("a.disableCount = %d, want 1", a.disableCount)
	}
}

func TestDisableAlreadyDisabled(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("foo", "1.0.0", "Foo")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Disable when already disabled should be a no-op
	if err := pm.Disable("foo"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if p.disableCount != 0 {
		t.Errorf("disableCount = %d, want 0", p.disableCount)
	}
}

// --- Circular Dependency ---

func TestCircularDependency(t *testing.T) {
	pm := newTestManager(t)

	a := newPluginWithDeps("cycle-a", "1.0.0", PluginDependency{Name: "cycle-b"})
	b := newPluginWithDeps("cycle-b", "1.0.0", PluginDependency{Name: "cycle-a"})

	if err := pm.Register(a); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Register(b); err != nil {
		t.Fatalf("Register: %v", err)
	}

	err := pm.Enable("cycle-a")
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
	if got := err.Error(); !strings.Contains(got, "circular") {
		t.Errorf("expected error to mention 'circular', got: %s", got)
	}
}

// --- RestoreState ---

func TestRestoreState(t *testing.T) {
	db := openTestDB(t)

	// Phase 1: create manager, register and enable plugins
	pm1 := NewPluginManager(db, nil)
	a := newSimplePlugin("alpha", "1.0.0", "Alpha")
	b := newSimplePlugin("bravo", "1.0.0", "Bravo")

	for _, p := range []NativePlugin{a, b} {
		if err := pm1.Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	if err := pm1.Enable("alpha"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	// bravo is NOT enabled

	// Phase 2: simulate restart — new manager, same DB
	pm2 := NewPluginManager(db, nil)
	a2 := newSimplePlugin("alpha", "1.0.0", "Alpha")
	b2 := newSimplePlugin("bravo", "1.0.0", "Bravo")

	for _, p := range []NativePlugin{a2, b2} {
		if err := pm2.Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	if err := pm2.RestoreState(); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	if !pm2.IsEnabled("alpha") {
		t.Error("alpha should be enabled after RestoreState")
	}
	if pm2.IsEnabled("bravo") {
		t.Error("bravo should NOT be enabled after RestoreState")
	}
	if a2.enableCount != 1 {
		t.Errorf("alpha2.enableCount = %d, want 1", a2.enableCount)
	}
}

// --- HTTP Dispatch ---

func TestHTTPDispatchEnabled(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("store-browser", "1.0.0", "Browse stores")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Enable("store-browser"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/store-browser/tables", nil)
	w := httptest.NewRecorder()
	pm.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["endpoint"] != "tables" {
		t.Errorf("got endpoint %q, want %q", result["endpoint"], "tables")
	}
	if result["plugin"] != "store-browser" {
		t.Errorf("got plugin %q, want %q", result["plugin"], "store-browser")
	}
}

func TestHTTPDispatchDisabled(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("store-browser", "1.0.0", "Browse stores")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Do NOT enable the plugin

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/store-browser/tables", nil)
	w := httptest.NewRecorder()
	pm.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHTTPDispatchAfterDisable(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("store-browser", "1.0.0", "Browse stores")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Enable("store-browser"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := pm.Disable("store-browser"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/store-browser/tables", nil)
	w := httptest.NewRecorder()
	pm.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d for disabled plugin", w.Code, http.StatusNotFound)
	}
}

func TestHTTPDispatchNonExistentPlugin(t *testing.T) {
	pm := newTestManager(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/nonexistent/tables", nil)
	w := httptest.NewRecorder()
	pm.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHTTPListPlugins(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("store-browser", "1.0.0", "Browse stores")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Enable("store-browser"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
	w := httptest.NewRecorder()
	pm.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var result []PluginInfo
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d plugins, want 1", len(result))
	}
	if result[0].Name != "store-browser" {
		t.Errorf("got name %q, want %q", result[0].Name, "store-browser")
	}
	if !result[0].Enabled {
		t.Error("expected plugin to be enabled in listing")
	}
}

func TestHTTPListMethodNotAllowed(t *testing.T) {
	pm := newTestManager(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins", nil)
	w := httptest.NewRecorder()
	pm.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// --- Error Cases ---

func TestEnableNonExistent(t *testing.T) {
	pm := newTestManager(t)
	err := pm.Enable("does-not-exist")
	if err == nil {
		t.Error("expected error for non-existent plugin")
	}
}

func TestDisableNonExistent(t *testing.T) {
	pm := newTestManager(t)
	err := pm.Disable("does-not-exist")
	if err == nil {
		t.Error("expected error for non-existent plugin")
	}
}

func TestEnableMissingDependency(t *testing.T) {
	pm := newTestManager(t)
	p := newPluginWithDeps("consumer", "1.0.0", PluginDependency{Name: "missing-dep"})

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	err := pm.Enable("consumer")
	if err == nil {
		t.Error("expected error for missing dependency")
	}
}

func TestOnEnableError(t *testing.T) {
	pm := newTestManager(t)
	p := &testPlugin{
		name:    "failing",
		version: "1.0.0",
		onEnableFn: func(_ PluginContext) error {
			return fmt.Errorf("init failed")
		},
	}

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	err := pm.Enable("failing")
	if err == nil {
		t.Fatal("expected error from OnEnable")
	}

	if pm.IsEnabled("failing") {
		t.Error("plugin should not be enabled after OnEnable error")
	}
}

// --- AllPlugins and EnabledPlugins ---

func TestAllPlugins(t *testing.T) {
	pm := newTestManager(t)
	a := newSimplePlugin("alpha", "1.0.0", "Alpha")
	b := newSimplePlugin("bravo", "2.0.0", "Bravo")

	for _, p := range []NativePlugin{a, b} {
		if err := pm.Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	if err := pm.Enable("alpha"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	all := pm.AllPlugins()
	if len(all) != 2 {
		t.Fatalf("got %d plugins, want 2", len(all))
	}
	// Should be sorted by name
	if all[0].Name != "alpha" || all[1].Name != "bravo" {
		t.Errorf("unexpected order: %s, %s", all[0].Name, all[1].Name)
	}
	if !all[0].Enabled {
		t.Error("alpha should be enabled")
	}
	if all[1].Enabled {
		t.Error("bravo should not be enabled")
	}
}

func TestEnabledPlugins(t *testing.T) {
	pm := newTestManager(t)
	a := newSimplePlugin("alpha", "1.0.0", "Alpha")
	b := newSimplePlugin("bravo", "2.0.0", "Bravo")

	for _, p := range []NativePlugin{a, b} {
		if err := pm.Register(p); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	if err := pm.Enable("alpha"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	enabled := pm.EnabledPlugins()
	if len(enabled) != 1 {
		t.Fatalf("got %d enabled, want 1", len(enabled))
	}
	if enabled[0].Name() != "alpha" {
		t.Errorf("got %q, want %q", enabled[0].Name(), "alpha")
	}
}

// --- NativeHandler delegation ---

func TestNativeHandlerDelegation(t *testing.T) {
	pm := newTestManager(t)
	p := newSimplePlugin("store-browser", "1.0.0", "Browse stores")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Enable("store-browser"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	handler := NewNativeHandler(pm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins/store-browser/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("got status %q, want %q", result["status"], "ok")
	}
}

// --- SetContext ---

func TestSetContextPassedToOnEnable(t *testing.T) {
	pm := newTestManager(t)

	var receivedCtx PluginContext
	p := &testPlugin{
		name:    "ctx-test",
		version: "1.0.0",
		onEnableFn: func(ctx PluginContext) error {
			receivedCtx = ctx
			return nil
		},
	}

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	pm.SetContext(PluginContext{DataDir: "/tmp/test"})
	if err := pm.Enable("ctx-test"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	if receivedCtx.DataDir != "/tmp/test" {
		t.Errorf("got DataDir %q, want %q", receivedCtx.DataDir, "/tmp/test")
	}
}

// --- Manager without DB ---

func TestManagerWithoutDB(t *testing.T) {
	pm := NewPluginManager(nil, nil)
	p := newSimplePlugin("no-db", "1.0.0", "No DB")

	if err := pm.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := pm.Enable("no-db"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !pm.IsEnabled("no-db") {
		t.Error("plugin should be enabled")
	}
	// RestoreState with nil DB should be a no-op
	if err := pm.RestoreState(); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
}

