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
	routerName  string // optional: name of the router to attach to
}

// StaticFileServerOption is a functional option for configuring a StaticFileServer.
type StaticFileServerOption func(*StaticFileServer)

// WithSPAFallback enables Single Page Application fallback: requests for
// unknown paths are served with index.html instead of a 404.
func WithSPAFallback() StaticFileServerOption {
	return func(s *StaticFileServer) {
		s.spaFallback = true
	}
}

// WithCacheMaxAge sets the Cache-Control max-age value (in seconds).
// Defaults to 3600 when not specified or when seconds <= 0.
func WithCacheMaxAge(seconds int) StaticFileServerOption {
	return func(s *StaticFileServer) {
		if seconds > 0 {
			s.cacheMaxAge = seconds
		}
	}
}

// NewStaticFileServer creates a new static file server module.
// Use WithSPAFallback() and WithCacheMaxAge() to customise behaviour.
func NewStaticFileServer(name, root, prefix string, opts ...StaticFileServerOption) *StaticFileServer {
	if prefix == "" {
		prefix = "/"
	}
	s := &StaticFileServer{
		name:        name,
		root:        root,
		prefix:      prefix,
		spaFallback: false,
		cacheMaxAge: 3600,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name returns the module name
func (s *StaticFileServer) Name() string {
	return s.name
}

// Prefix returns the URL prefix for this file server
func (s *StaticFileServer) Prefix() string {
	return s.prefix
}

// RouterName returns the optional router name this file server should attach to.
// An empty string means attach to the first available router.
func (s *StaticFileServer) RouterName() string {
	return s.routerName
}

// SetRouterName sets the router name this file server should attach to.
func (s *StaticFileServer) SetRouterName(name string) {
	s.routerName = name
}

// SPAFallbackEnabled returns whether SPA fallback is active.
func (s *StaticFileServer) SPAFallbackEnabled() bool {
	return s.spaFallback
}

// Init initializes the module
func (s *StaticFileServer) Init(app modular.Application) error {
	// Validate root path exists, creating it if needed (for build pipeline workflows
	// where the output directory is created by a build step after engine init)
	absRoot, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("invalid root path %q: %w", s.root, err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(absRoot, 0750); mkErr != nil {
				return fmt.Errorf("root path %q does not exist and could not be created: %w", absRoot, mkErr)
			}
		} else {
			return fmt.Errorf("root path %q: %w", absRoot, err)
		}
	} else if !info.IsDir() {
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
	info, err := os.Stat(fullPath) //nolint:gosec // G703: path sanitized via filepath.Join with root
	if os.IsNotExist(err) {
		if s.spaFallback {
			// Serve index.html for SPA routing
			indexPath := filepath.Join(s.root, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil { //nolint:gosec // G703: path sanitized via filepath.Join with root
				http.ServeFile(w, r, indexPath)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	// If the path is a directory, serve its index.html (SPA entry point)
	if err == nil && info.IsDir() {
		indexPath := filepath.Join(fullPath, "index.html")
		if _, indexErr := os.Stat(indexPath); indexErr == nil { //nolint:gosec // G703: path sanitized via filepath.Join with root
			http.ServeFile(w, r, indexPath)
			return
		}
		if s.spaFallback {
			rootIndex := filepath.Join(s.root, "index.html")
			if _, rootErr := os.Stat(rootIndex); rootErr == nil { //nolint:gosec // G703: path sanitized via filepath.Join with root
				http.ServeFile(w, r, rootIndex)
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
