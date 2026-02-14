package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// SimpleProxy is a lightweight reverse proxy module that forwards requests
// to backend services based on path prefix matching.
type SimpleProxy struct {
	name    string
	targets map[string]*url.URL // path prefix -> backend URL
	proxies map[string]*httputil.ReverseProxy
	// sorted prefixes longest-first for matching
	sortedPrefixes []string
}

// NewSimpleProxy creates a new simple reverse proxy module.
func NewSimpleProxy(name string) *SimpleProxy {
	return &SimpleProxy{
		name:    name,
		targets: make(map[string]*url.URL),
		proxies: make(map[string]*httputil.ReverseProxy),
	}
}

// SetTargets configures the proxy targets from a map of path prefix -> backend URL strings.
func (p *SimpleProxy) SetTargets(targets map[string]string) error {
	for prefix, backendStr := range targets {
		backend, err := url.Parse(backendStr)
		if err != nil {
			return fmt.Errorf("invalid backend URL %q for prefix %q: %w", backendStr, prefix, err)
		}
		p.targets[prefix] = backend
		rp := httputil.NewSingleHostReverseProxy(backend)
		backendHost := backend.Host
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "backend unavailable",
				"backend": backendHost,
				"path":    r.URL.Path,
			})
		}
		p.proxies[prefix] = rp
	}
	p.buildSortedPrefixes()
	return nil
}

// buildSortedPrefixes sorts prefixes longest-first for correct matching.
func (p *SimpleProxy) buildSortedPrefixes() {
	p.sortedPrefixes = make([]string, 0, len(p.targets))
	for prefix := range p.targets {
		p.sortedPrefixes = append(p.sortedPrefixes, prefix)
	}
	sort.Slice(p.sortedPrefixes, func(i, j int) bool {
		return len(p.sortedPrefixes[i]) > len(p.sortedPrefixes[j])
	})
}

// Name returns the module name.
func (p *SimpleProxy) Name() string {
	return p.name
}

// Init initializes the module.
func (p *SimpleProxy) Init(_ modular.Application) error {
	return nil
}

// Start is a no-op.
func (p *SimpleProxy) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op.
func (p *SimpleProxy) Stop(_ context.Context) error {
	return nil
}

// ProvidesServices returns the services provided by this module.
func (p *SimpleProxy) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        p.name,
			Description: "Simple Reverse Proxy",
			Instance:    p,
		},
	}
}

// RequiresServices returns no dependencies.
func (p *SimpleProxy) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Handle proxies the request to the appropriate backend based on path prefix.
func (p *SimpleProxy) Handle(w http.ResponseWriter, r *http.Request) {
	for _, prefix := range p.sortedPrefixes {
		if strings.HasPrefix(r.URL.Path, prefix) {
			proxy := p.proxies[prefix]
			proxy.ServeHTTP(w, r)
			return
		}
	}

	http.Error(w, "no backend configured for path", http.StatusBadGateway)
}
