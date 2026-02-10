package dynamic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A minimal dynamic component source that implements all five functions.
const simpleComponentSource = `package component

import (
	"context"
	"fmt"
)

func Name() string {
	return "test-component"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing name")
	}
	return map[string]interface{}{
		"greeting": "hello " + name,
	}, nil
}
`

// Source with only Name and Execute (no Init/Start/Stop).
const minimalComponentSource = `package component

import (
	"context"
)

func Name() string {
	return "minimal"
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"ok": true}, nil
}
`

// Source that imports a blocked package.
const blockedImportSource = `package component

import (
	"os/exec"
)

func Name() string {
	return "bad"
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	_ = exec.Command("echo", "hello")
	return nil, nil
}
`

// Source with a syntax error in imports (will fail parser.ParseFile with ImportsOnly).
const badSyntaxSource = `package component

import (
	"fmt
)

func Name() string { return "broken" }
`

// --- Interpreter tests ---

func TestNewInterpreterPool(t *testing.T) {
	pool := NewInterpreterPool()
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}

	interp, err := pool.NewInterpreter()
	if err != nil {
		t.Fatalf("NewInterpreter failed: %v", err)
	}
	if interp == nil {
		t.Fatal("expected non-nil interpreter")
	}
}

// --- Component tests ---

func TestLoadFromSource_Simple(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	err := comp.LoadFromSource(simpleComponentSource)
	if err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	if comp.Name() != "test-component" {
		t.Errorf("expected Name()=%q, got %q", "test-component", comp.Name())
	}

	info := comp.Info()
	if info.Status != StatusLoaded {
		t.Errorf("expected status %q, got %q", StatusLoaded, info.Status)
	}
}

func TestLoadFromSource_Minimal(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("minimal", pool)

	err := comp.LoadFromSource(minimalComponentSource)
	if err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	if comp.Name() != "minimal" {
		t.Errorf("expected Name()=%q, got %q", "minimal", comp.Name())
	}
}

func TestComponentLifecycle(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(simpleComponentSource); err != nil {
		t.Fatal(err)
	}

	// Init
	if err := comp.Init(nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if comp.Info().Status != StatusInitialized {
		t.Errorf("expected %q, got %q", StatusInitialized, comp.Info().Status)
	}

	// Start
	ctx := context.Background()
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if comp.Info().Status != StatusRunning {
		t.Errorf("expected %q, got %q", StatusRunning, comp.Info().Status)
	}

	// Execute
	result, err := comp.Execute(ctx, map[string]interface{}{"name": "world"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["greeting"] != "hello world" {
		t.Errorf("expected greeting=%q, got %q", "hello world", result["greeting"])
	}

	// Stop
	if err := comp.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if comp.Info().Status != StatusStopped {
		t.Errorf("expected %q, got %q", StatusStopped, comp.Info().Status)
	}
}

func TestExecuteWithoutFunction(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("empty", pool)

	// Load source with no Execute function
	src := `package component
func Name() string { return "empty" }
`
	if err := comp.LoadFromSource(src); err != nil {
		t.Fatal(err)
	}

	_, err := comp.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error when Execute is missing")
	}
}

// --- Sandbox / Validation tests ---

func TestValidateSource_AllowedImports(t *testing.T) {
	src := `package component

import (
	"fmt"
	"strings"
	"context"
	"encoding/json"
)

func Name() string { return "valid" }
`
	if err := ValidateSource(src); err != nil {
		t.Errorf("expected valid source, got error: %v", err)
	}
}

func TestValidateSource_BlockedImport(t *testing.T) {
	err := ValidateSource(blockedImportSource)
	if err == nil {
		t.Error("expected error for blocked import, got nil")
	}
	if !strings.Contains(err.Error(), "os/exec") {
		t.Errorf("error should mention os/exec, got: %v", err)
	}
}

func TestValidateSource_SyntaxError(t *testing.T) {
	err := ValidateSource(badSyntaxSource)
	if err == nil {
		t.Error("expected error for bad syntax, got nil")
	}
}

func TestIsPackageAllowed(t *testing.T) {
	tests := []struct {
		pkg     string
		allowed bool
	}{
		{"fmt", true},
		{"context", true},
		{"os/exec", false},
		{"syscall", false},
		{"unsafe", false},
		{"os", false},
		{"net/http", true},
		{"encoding/json", true},
		{"unknown/pkg", false},
	}

	for _, tt := range tests {
		got := IsPackageAllowed(tt.pkg)
		if got != tt.allowed {
			t.Errorf("IsPackageAllowed(%q) = %v, want %v", tt.pkg, got, tt.allowed)
		}
	}
}

// --- Registry tests ---

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewComponentRegistry()
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("c1", pool)

	if err := reg.Register("c1", comp); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := reg.Get("c1")
	if !ok {
		t.Fatal("expected component to be found")
	}
	if got != comp {
		t.Error("got different component than registered")
	}

	if reg.Count() != 1 {
		t.Errorf("expected count=1, got %d", reg.Count())
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewComponentRegistry()
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("c1", pool)

	_ = reg.Register("c1", comp)
	if err := reg.Unregister("c1"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	if _, ok := reg.Get("c1"); ok {
		t.Error("expected component to be gone")
	}
}

func TestRegistry_UnregisterNotFound(t *testing.T) {
	reg := NewComponentRegistry()
	err := reg.Unregister("nonexistent")
	if err == nil {
		t.Error("expected error for unregistering nonexistent component")
	}
}

func TestRegistry_RegisterEmpty(t *testing.T) {
	reg := NewComponentRegistry()
	err := reg.Register("", nil)
	if err == nil {
		t.Error("expected error for empty id")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewComponentRegistry()
	pool := NewInterpreterPool()

	for _, id := range []string{"a", "b", "c"} {
		comp := NewDynamicComponent(id, pool)
		_ = reg.Register(id, comp)
	}

	infos := reg.List()
	if len(infos) != 3 {
		t.Errorf("expected 3 infos, got %d", len(infos))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewComponentRegistry()
	pool := NewInterpreterPool()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := strings.Repeat("x", n%10+1)
			comp := NewDynamicComponent(id, pool)
			_ = reg.Register(id, comp)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = reg.List()
			id := strings.Repeat("x", n%10+1)
			reg.Get(id)
		}(i)
	}

	wg.Wait()
}

// --- Loader tests ---

func TestLoader_LoadFromString(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	comp, err := loader.LoadFromString("test", simpleComponentSource)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}
	if comp.Name() != "test-component" {
		t.Errorf("unexpected name: %s", comp.Name())
	}

	// Should be in registry
	got, ok := reg.Get("test")
	if !ok {
		t.Error("component not in registry")
	}
	if got != comp {
		t.Error("registry returned different component")
	}
}

func TestLoader_LoadFromString_BlockedImport(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	_, err := loader.LoadFromString("bad", blockedImportSource)
	if err == nil {
		t.Error("expected error for blocked import")
	}
}

func TestLoader_LoadFromFile(t *testing.T) {
	// Write a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "greet.go")
	if err := os.WriteFile(path, []byte(simpleComponentSource), 0644); err != nil {
		t.Fatal(err)
	}

	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	comp, err := loader.LoadFromFile("", path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// ID should be derived from filename
	if comp.Info().ID != "greet" {
		t.Errorf("expected id=%q, got %q", "greet", comp.Info().ID)
	}
}

func TestLoader_LoadFromDirectory(t *testing.T) {
	dir := t.TempDir()

	// Write two component files
	if err := os.WriteFile(filepath.Join(dir, "comp1.go"), []byte(simpleComponentSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comp2.go"), []byte(minimalComponentSource), 0644); err != nil {
		t.Fatal(err)
	}
	// Write a test file that should be skipped
	if err := os.WriteFile(filepath.Join(dir, "comp_test.go"), []byte(`package component`), 0644); err != nil {
		t.Fatal(err)
	}

	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	comps, err := loader.LoadFromDirectory(dir)
	if err != nil {
		t.Fatalf("LoadFromDirectory failed: %v", err)
	}
	if len(comps) != 2 {
		t.Errorf("expected 2 components, got %d", len(comps))
	}
	if reg.Count() != 2 {
		t.Errorf("expected 2 in registry, got %d", reg.Count())
	}
}

func TestLoader_Reload(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	// Load initial version
	_, err := loader.LoadFromString("test", simpleComponentSource)
	if err != nil {
		t.Fatal(err)
	}

	// Reload with new source that has a different name
	updatedSource := strings.Replace(simpleComponentSource, "test-component", "updated-component", 1)
	comp, err := loader.Reload("test", updatedSource)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if comp.Name() != "updated-component" {
		t.Errorf("expected name=%q, got %q", "updated-component", comp.Name())
	}

	// Registry should have the updated component
	got, ok := reg.Get("test")
	if !ok {
		t.Error("component not in registry after reload")
	}
	if got.Name() != "updated-component" {
		t.Errorf("registry has wrong component after reload")
	}
}

// --- API handler tests ---

func TestAPI_ListComponents(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	// Load one component
	_, _ = loader.LoadFromString("test", simpleComponentSource)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/dynamic/components", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var infos []ComponentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &infos); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(infos) != 1 {
		t.Errorf("expected 1 component, got %d", len(infos))
	}
}

func TestAPI_CreateComponent(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	payload := map[string]string{
		"id":     "new-comp",
		"source": minimalComponentSource,
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/dynamic/components", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if reg.Count() != 1 {
		t.Errorf("expected 1 in registry, got %d", reg.Count())
	}
}

func TestAPI_CreateComponent_BadSource(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	body := `{"id":"bad","source":"this is not valid go"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dynamic/components", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestAPI_GetComponent(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)
	_, _ = loader.LoadFromString("mycomp", simpleComponentSource)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/dynamic/components/mycomp", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAPI_GetComponent_NotFound(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/dynamic/components/nope", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAPI_DeleteComponent(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)
	_, _ = loader.LoadFromString("del", simpleComponentSource)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/dynamic/components/del", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}

	if reg.Count() != 0 {
		t.Error("expected registry to be empty")
	}
}

func TestAPI_UpdateComponent(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)
	_, _ = loader.LoadFromString("upd", simpleComponentSource)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	newSource := strings.Replace(simpleComponentSource, "test-component", "updated-component", 1)
	payload := map[string]string{"source": newSource}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/dynamic/components/upd", strings.NewReader(string(bodyBytes)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	comp, ok := reg.Get("upd")
	if !ok {
		t.Fatal("component not found after update")
	}
	if comp.Name() != "updated-component" {
		t.Errorf("expected updated name, got %q", comp.Name())
	}
}

// --- Watcher tests ---

func TestWatcher_FileChange(t *testing.T) {
	dir := t.TempDir()
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	watcher := NewWatcher(loader, dir, WithDebounce(100*time.Millisecond))
	if err := watcher.Start(); err != nil {
		t.Fatalf("Start watcher failed: %v", err)
	}
	defer func() { _ = watcher.Stop() }()

	// Write a component file
	path := filepath.Join(dir, "mycomp.go")
	if err := os.WriteFile(path, []byte(minimalComponentSource), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + processing
	time.Sleep(800 * time.Millisecond)

	if _, ok := reg.Get("mycomp"); !ok {
		t.Error("expected component to be auto-loaded by watcher")
	}
}

func TestWatcher_FileRemoval(t *testing.T) {
	dir := t.TempDir()
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	// Pre-load a component
	path := filepath.Join(dir, "removeme.go")
	if err := os.WriteFile(path, []byte(minimalComponentSource), 0644); err != nil {
		t.Fatal(err)
	}
	_, _ = loader.LoadFromFile("removeme", path)

	watcher := NewWatcher(loader, dir, WithDebounce(100*time.Millisecond))
	if err := watcher.Start(); err != nil {
		t.Fatalf("Start watcher failed: %v", err)
	}
	defer func() { _ = watcher.Stop() }()

	// Remove the file
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	// Wait for processing
	time.Sleep(800 * time.Millisecond)

	if _, ok := reg.Get("removeme"); ok {
		t.Error("expected component to be unregistered after file removal")
	}
}
