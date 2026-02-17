package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// RegistryHandler provides HTTP API handlers for plugin management.
type RegistryHandler struct {
	registry *CompositeRegistry
}

// NewRegistryHandler creates a new registry handler backed by the given composite registry.
func NewRegistryHandler(registry *CompositeRegistry) *RegistryHandler {
	return &RegistryHandler{registry: registry}
}

// isSafePathComponent returns true if s does not contain path separators or parent directory references.
// This is used to ensure user-provided plugin names and versions cannot escape the plugin directory.
func isSafePathComponent(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "/") || strings.Contains(s, "\\") {
		return false
	}
	if strings.Contains(s, "..") {
		return false
	}
	return true
}

// RegisterRoutes registers plugin management HTTP routes on the given mux.
func (h *RegistryHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/plugins", h.handleListInstalled)
	mux.HandleFunc("GET /api/v1/admin/plugins/registry/search", h.handleSearch)
	mux.HandleFunc("POST /api/v1/admin/plugins/registry/install", h.handleInstall)
	mux.HandleFunc("DELETE /api/v1/admin/plugins/{name}", h.handleUninstall)
}

// handleListInstalled returns all locally installed plugins.
func (h *RegistryHandler) handleListInstalled(w http.ResponseWriter, r *http.Request) {
	entries := h.registry.List()
	type pluginInfo struct {
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Author      string   `json:"author"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Installed   bool     `json:"installed"`
	}

	plugins := make([]pluginInfo, 0, len(entries))
	for _, e := range entries {
		plugins = append(plugins, pluginInfo{
			Name:        e.Manifest.Name,
			Version:     e.Manifest.Version,
			Author:      e.Manifest.Author,
			Description: e.Manifest.Description,
			Tags:        e.Manifest.Tags,
			Installed:   true,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(plugins)
}

// handleSearch searches both local and remote registries.
func (h *RegistryHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	results, err := h.registry.Search(r.Context(), query)
	if err != nil {
		http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Build response with installed status
	type searchResult struct {
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Author      string   `json:"author"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Installed   bool     `json:"installed"`
	}

	response := make([]searchResult, 0, len(results))
	for _, m := range results {
		_, installed := h.registry.Get(m.Name)
		response = append(response, searchResult{
			Name:        m.Name,
			Version:     m.Version,
			Author:      m.Author,
			Description: m.Description,
			Tags:        m.Tags,
			Installed:   installed,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// installRequest is the JSON body for plugin installation.
type installRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// handleInstall installs a plugin from the remote registry.
func (h *RegistryHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Version == "" {
		http.Error(w, "version is required", http.StatusBadRequest)
		return
	}
	if !isSafePathComponent(req.Name) || !isSafePathComponent(req.Version) {
		http.Error(w, "invalid plugin name or version", http.StatusBadRequest)
		return
	}

	if err := h.registry.Install(r.Context(), req.Name, req.Version); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already installed") {
			status = http.StatusConflict
		}
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf("install failed: %v", err), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "installed",
		"name":    req.Name,
		"version": req.Version,
	})
}

// handleUninstall removes a plugin from the local registry.
func (h *RegistryHandler) handleUninstall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "plugin name is required", http.StatusBadRequest)
		return
	}

	if err := h.registry.Unregister(name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, fmt.Sprintf("plugin %q not found", name), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("uninstall failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "uninstalled",
		"name":   name,
	})
}
