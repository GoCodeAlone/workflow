package dynamic

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestAPI_ComponentsMethodNotAllowed(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/dynamic/components", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestAPI_ComponentByID_EmptyID(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Requesting /api/dynamic/components/ with no ID suffix
	req := httptest.NewRequest(http.MethodGet, "/api/dynamic/components/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPI_ComponentByID_MethodNotAllowed(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/dynamic/components/some-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestAPI_CreateComponent_InvalidJSON(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/dynamic/components", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPI_CreateComponent_MissingFields(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Missing source
	body := `{"id":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dynamic/components", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing source, got %d", w.Code)
	}

	// Missing id
	body = `{"source":"package component"}`
	req = httptest.NewRequest(http.MethodPost, "/api/dynamic/components", strings.NewReader(body))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing id, got %d", w.Code)
	}
}

func TestAPI_UpdateComponent_InvalidJSON(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/dynamic/components/test", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPI_UpdateComponent_EmptySource(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	body := `{"source":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/dynamic/components/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPI_UpdateComponent_ReloadError(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Try to reload with invalid Go source
	body := `{"source":"this is not valid go source"}`
	req := httptest.NewRequest(http.MethodPut, "/api/dynamic/components/nonexistent", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestAPI_DeleteComponent_NotFound(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/dynamic/components/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAPI_DeleteComponent_RunningComponent(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	// Load and start a component
	comp, err := loader.LoadFromString("running", simpleComponentSource)
	if err != nil {
		t.Fatalf("LoadFromString: %v", err)
	}
	if err := comp.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := comp.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	info := comp.Info()
	if info.Status != StatusRunning {
		t.Fatalf("expected status running, got %s", info.Status)
	}

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/dynamic/components/running", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if reg.Count() != 0 {
		t.Error("expected registry to be empty after delete")
	}
}

func TestComponent_InitStartStop_Error(t *testing.T) {
	pool := NewInterpreterPool()

	// Source with Init that returns an error
	errSource := `package component

import (
	"context"
	"fmt"
)

func Name() string { return "err-comp" }
func Init(services map[string]interface{}) error { return fmt.Errorf("init error") }
func Start(ctx context.Context) error { return fmt.Errorf("start error") }
func Stop(ctx context.Context) error { return fmt.Errorf("stop error") }
`
	comp := NewDynamicComponent("err-comp", pool)
	if err := comp.LoadFromSource(errSource); err != nil {
		t.Fatalf("LoadFromSource: %v", err)
	}

	// Init should return error
	if err := comp.Init(nil); err == nil {
		t.Error("expected Init error")
	}
	if comp.Info().Status != StatusError {
		t.Errorf("expected error status after failed Init, got %s", comp.Info().Status)
	}

	// Reset status to loaded so Start can run
	comp.mu.Lock()
	comp.info.Status = StatusLoaded
	comp.mu.Unlock()

	// Start should return error
	if err := comp.Start(context.Background()); err == nil {
		t.Error("expected Start error")
	}
	if comp.Info().Status != StatusError {
		t.Errorf("expected error status after failed Start, got %s", comp.Info().Status)
	}

	// Reset status
	comp.mu.Lock()
	comp.info.Status = StatusRunning
	comp.mu.Unlock()

	// Stop should return error
	if err := comp.Stop(context.Background()); err == nil {
		t.Error("expected Stop error")
	}
	if comp.Info().Status != StatusError {
		t.Errorf("expected error status after failed Stop, got %s", comp.Info().Status)
	}
}

func TestComponent_Execute_PanicRecovery(t *testing.T) {
	pool := NewInterpreterPool()

	// Source with Execute that panics
	panicSource := `package component

import "context"

func Name() string { return "panic-comp" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	panic("deliberate panic")
}
`
	comp := NewDynamicComponent("panic-comp", pool)
	if err := comp.LoadFromSource(panicSource); err != nil {
		t.Fatalf("LoadFromSource: %v", err)
	}

	_, err := comp.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error from panicking Execute")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected panic-related error, got: %v", err)
	}
}

func TestModuleAdapter_InitWithRequires(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("req-comp", pool)

	src := `package component

func Name() string { return "req-comp" }
func Init(services map[string]interface{}) error { return nil }
`
	if err := comp.LoadFromSource(src); err != nil {
		t.Fatalf("LoadFromSource: %v", err)
	}

	adapter := NewModuleAdapter(comp)
	adapter.SetRequires([]string{"dep-svc"})
	adapter.SetProvides([]string{"my-svc"})

	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	if err := app.Init(); err != nil {
		t.Fatalf("Init app: %v", err)
	}

	// Register a dependency service so GetService finds it
	if err := app.RegisterService("dep-svc", "dependency-value"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	if err := adapter.Init(app); err != nil {
		t.Fatalf("adapter Init: %v", err)
	}

	// Verify the provided service was registered
	var svc interface{}
	if err := app.GetService("my-svc", &svc); err != nil {
		t.Fatalf("expected 'my-svc' to be registered: %v", err)
	}
}

func TestInterpreterPoolOptions(t *testing.T) {
	// Test WithGoPath option
	pool := NewInterpreterPool(WithGoPath("/tmp/test-gopath"))
	if pool.goPath != "/tmp/test-gopath" {
		t.Errorf("expected goPath '/tmp/test-gopath', got '%s'", pool.goPath)
	}

	// Test WithAllowedPackages option
	customPkgs := map[string]bool{"fmt": true}
	pool2 := NewInterpreterPool(WithAllowedPackages(customPkgs))
	if len(pool2.allowedPackages) != 1 {
		t.Errorf("expected 1 allowed package, got %d", len(pool2.allowedPackages))
	}
}

func TestWatcher_WithLogger(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	customLogger := log.New(io.Discard, "[test] ", 0)
	watcher := NewWatcher(loader, t.TempDir(), WithLogger(customLogger))

	if watcher.logger != customLogger {
		t.Error("expected custom logger to be set")
	}
}

// Verify the API handler RegisterRoutes actually registers both patterns.
func TestAPI_RegisterRoutes_Patterns(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	api := NewAPIHandler(loader, reg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// GET /api/dynamic/components should work (200 with empty list)
	req := httptest.NewRequest(http.MethodGet, "/api/dynamic/components", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for GET /api/dynamic/components, got %d", w.Code)
	}

	var infos []ComponentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &infos); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected 0 components, got %d", len(infos))
	}
}
