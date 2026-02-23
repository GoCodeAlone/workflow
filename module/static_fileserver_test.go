package module

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupStaticFileServer(t *testing.T) (*StaticFileServer, string) {
	t.Helper()
	dir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('hello')"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "about.html"), []byte("<html>about</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	app := CreateIsolatedApp(t)
	srv := NewStaticFileServer("static-server", dir, "/", WithSPAFallback())
	if err := srv.Init(app); err != nil {
		t.Fatal(err)
	}
	return srv, dir
}

func TestStaticFileServer_Name(t *testing.T) {
	srv := NewStaticFileServer("my-static", "/tmp", "/")
	if srv.Name() != "my-static" {
		t.Errorf("expected name 'my-static', got '%s'", srv.Name())
	}
}

func TestStaticFileServer_ServeFile(t *testing.T) {
	srv, _ := setupStaticFileServer(t)

	req := httptest.NewRequest(http.MethodGet, "/about.html", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "<html>about</html>" {
		t.Errorf("expected about.html content, got %q", body)
	}
}

func TestStaticFileServer_ServeNestedFile(t *testing.T) {
	srv, _ := setupStaticFileServer(t)

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "console.log('hello')" {
		t.Errorf("expected app.js content, got %q", body)
	}
}

func TestStaticFileServer_SPAFallback(t *testing.T) {
	srv, _ := setupStaticFileServer(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for SPA fallback, got %d", w.Code)
	}
	if body := w.Body.String(); body != "<html>index</html>" {
		t.Errorf("expected index.html content for SPA fallback, got %q", body)
	}
}

func TestStaticFileServer_NoSPAFallback_404(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	app := CreateIsolatedApp(t)
	srv := NewStaticFileServer("static-no-spa", dir, "/")
	if err := srv.Init(app); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 without SPA fallback, got %d", w.Code)
	}
}

func TestStaticFileServer_CacheHeaders(t *testing.T) {
	srv, _ := setupStaticFileServer(t)

	req := httptest.NewRequest(http.MethodGet, "/about.html", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "public, max-age=3600" {
		t.Errorf("expected Cache-Control 'public, max-age=3600', got '%s'", cacheControl)
	}

	xContentType := w.Header().Get("X-Content-Type-Options")
	if xContentType != "nosniff" {
		t.Errorf("expected X-Content-Type-Options 'nosniff', got '%s'", xContentType)
	}
}

func TestStaticFileServer_DirectoryTraversalPrevention(t *testing.T) {
	srv, _ := setupStaticFileServer(t)

	req := httptest.NewRequest(http.MethodGet, "/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	// Should be forbidden or not found, not serving /etc/passwd
	if w.Code == http.StatusOK {
		body := w.Body.String()
		if body != "<html>index</html>" {
			// If it served anything other than the SPA fallback, that's a problem
			t.Error("directory traversal should not serve files outside root")
		}
	}
}

func TestStaticFileServer_PrefixRouting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	app := CreateIsolatedApp(t)
	srv := NewStaticFileServer("prefixed", dir, "/static/", WithCacheMaxAge(7200))
	if err := srv.Init(app); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/static/test.txt", nil)
	w := httptest.NewRecorder()
	srv.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 with prefix routing, got %d", w.Code)
	}
}

func TestStaticFileServer_InvalidRootPath(t *testing.T) {
	app := CreateIsolatedApp(t)
	srv := NewStaticFileServer("bad-root", "/nonexistent/path/that/doesnt/exist", "/", WithSPAFallback())
	err := srv.Init(app)
	if err == nil {
		t.Error("expected error for nonexistent root path")
	}
}

func TestStaticFileServer_ProvidesServices(t *testing.T) {
	srv := NewStaticFileServer("static-test", "/tmp", "/", WithSPAFallback())
	services := srv.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "static-test" {
		t.Errorf("expected service name 'static-test', got '%s'", services[0].Name)
	}
}

func TestStaticFileServer_DefaultValues(t *testing.T) {
	srv := NewStaticFileServer("defaults", "/tmp", "")
	if srv.prefix != "/" {
		t.Errorf("expected default prefix '/', got '%s'", srv.prefix)
	}
	if srv.cacheMaxAge != 3600 {
		t.Errorf("expected default cacheMaxAge 3600, got %d", srv.cacheMaxAge)
	}
}
