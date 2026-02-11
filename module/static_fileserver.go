package module

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// StaticFileServer serves static files from a directory with optional SPA fallback
type StaticFileServer struct {
	name        string
	root        string
	prefix      string
	spaFallback bool
	cacheMaxAge int
}

// NewStaticFileServer creates a new static file server module
func NewStaticFileServer(name, root, prefix string, spaFallback bool, cacheMaxAge int) *StaticFileServer {
	if prefix == "" {
		prefix = "/"
	}
	if cacheMaxAge <= 0 {
		cacheMaxAge = 3600
	}
	return &StaticFileServer{
		name:        name,
		root:        root,
		prefix:      prefix,
		spaFallback: spaFallback,
		cacheMaxAge: cacheMaxAge,
	}
}

// Name returns the module name
func (s *StaticFileServer) Name() string {
	return s.name
}

// Prefix returns the URL prefix for this file server
func (s *StaticFileServer) Prefix() string {
	return s.prefix
}

// Init initializes the module
func (s *StaticFileServer) Init(app modular.Application) error {
	// Validate root path exists
	absRoot, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("invalid root path %q: %w", s.root, err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return fmt.Errorf("root path %q does not exist: %w", absRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root path %q is not a directory", absRoot)
	}
	s.root = absRoot
	return nil
}

// Handle serves static files
func (s *StaticFileServer) Handle(w http.ResponseWriter, r *http.Request) {
	// Strip prefix to get the file path
	path := r.URL.Path
	if s.prefix != "/" {
		path = strings.TrimPrefix(path, strings.TrimSuffix(s.prefix, "/"))
	}
	if path == "" {
		path = "/"
	}

	// Prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	fullPath := filepath.Join(s.root, cleanPath)

	// Check that the resolved path is within root
	absPath, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(absPath, s.root) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Set security and cache headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", s.cacheMaxAge))

	// Check if file exists
	_, err = os.Stat(fullPath)
	if os.IsNotExist(err) {
		if s.spaFallback {
			// Serve index.html for SPA routing
			indexPath := filepath.Join(s.root, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, fullPath)
}

// Start is a no-op
func (s *StaticFileServer) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op
func (s *StaticFileServer) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices returns the services provided by this module
func (s *StaticFileServer) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        s.name,
			Description: "Static File Server",
			Instance:    s,
		},
	}
}

// RequiresServices returns services required by this module
func (s *StaticFileServer) RequiresServices() []modular.ServiceDependency {
	return nil
}
