package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// RemoteRegistry discovers and downloads plugins from a remote HTTP registry.
type RemoteRegistry struct {
	baseURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]*PluginManifest
	cacheTTL   time.Duration
	lastFetch  time.Time
}

// RemoteRegistryOption configures a RemoteRegistry.
type RemoteRegistryOption func(*RemoteRegistry)

// WithHTTPClient sets the HTTP client used by the remote registry.
func WithHTTPClient(client *http.Client) RemoteRegistryOption {
	return func(r *RemoteRegistry) {
		r.httpClient = client
	}
}

// WithCacheTTL sets how long cached manifests remain valid.
func WithCacheTTL(ttl time.Duration) RemoteRegistryOption {
	return func(r *RemoteRegistry) {
		r.cacheTTL = ttl
	}
}

// NewRemoteRegistry creates a new remote registry client.
func NewRemoteRegistry(baseURL string, opts ...RemoteRegistryOption) *RemoteRegistry {
	r := &RemoteRegistry{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]*PluginManifest),
		cacheTTL:   5 * time.Minute,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Search queries the remote registry for plugins matching the given query string.
func (r *RemoteRegistry) Search(ctx context.Context, query string) ([]*PluginManifest, error) {
	u := fmt.Sprintf("%s/api/v1/plugins?q=%s", r.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search remote registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote registry search returned %d: %s", resp.StatusCode, string(body))
	}

	var manifests []*PluginManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifests); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}

	// Update cache
	r.mu.Lock()
	for _, m := range manifests {
		r.cache[m.Name] = m
	}
	r.lastFetch = time.Now()
	r.mu.Unlock()

	return manifests, nil
}

// GetManifest retrieves the manifest for a specific plugin version from the remote registry.
func (r *RemoteRegistry) GetManifest(ctx context.Context, name, version string) (*PluginManifest, error) {
	// Check cache first
	r.mu.RLock()
	if cached, ok := r.cache[name]; ok && time.Since(r.lastFetch) < r.cacheTTL {
		if cached.Version == version || version == "" {
			r.mu.RUnlock()
			return cached, nil
		}
	}
	r.mu.RUnlock()

	u := fmt.Sprintf("%s/api/v1/plugins/%s/versions/%s", r.baseURL, url.PathEscape(name), url.PathEscape(version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest from remote: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %s@%s not found in remote registry", name, version)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote registry returned %d: %s", resp.StatusCode, string(body))
	}

	var manifest PluginManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	// Update cache
	r.mu.Lock()
	r.cache[name] = &manifest
	r.lastFetch = time.Now()
	r.mu.Unlock()

	return &manifest, nil
}

// Download retrieves the plugin archive for a specific version.
func (r *RemoteRegistry) Download(ctx context.Context, name, version string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/api/v1/plugins/%s/versions/%s/download", r.baseURL, url.PathEscape(name), url.PathEscape(version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download plugin from remote: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("plugin %s@%s not found in remote registry", name, version)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("remote registry download returned %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// ListVersions retrieves available versions for a plugin from the remote registry.
func (r *RemoteRegistry) ListVersions(ctx context.Context, name string) ([]string, error) {
	u := fmt.Sprintf("%s/api/v1/plugins/%s/versions", r.baseURL, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create versions request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list versions from remote: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %s not found in remote registry", name)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote registry returned %d: %s", resp.StatusCode, string(body))
	}

	var versions []string
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("decode versions: %w", err)
	}

	return versions, nil
}

// ClearCache clears the in-memory manifest cache.
func (r *RemoteRegistry) ClearCache() {
	r.mu.Lock()
	r.cache = make(map[string]*PluginManifest)
	r.lastFetch = time.Time{}
	r.mu.Unlock()
}
