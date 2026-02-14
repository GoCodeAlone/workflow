package plugin

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// APIHandler serves HTTP endpoints for the plugin registry.
type APIHandler struct {
	registry *LocalRegistry
	loader   *dynamic.Loader
}

// NewAPIHandler creates a new plugin API handler.
func NewAPIHandler(registry *LocalRegistry, loader *dynamic.Loader) *APIHandler {
	return &APIHandler{
		registry: registry,
		loader:   loader,
	}
}

// RegisterRoutes registers the plugin API routes on the given mux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/plugins", h.handlePlugins)
	mux.HandleFunc("/api/plugins/", h.handlePluginByName)
}

func (h *APIHandler) handlePlugins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listPlugins(w)
	case http.MethodPost:
		h.registerPlugin(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handlePluginByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	if name == "" {
		http.Error(w, "plugin name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getPlugin(w, name)
	case http.MethodDelete:
		h.deletePlugin(w, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// pluginListEntry is the JSON representation for the list endpoint.
type pluginListEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
}

func (h *APIHandler) listPlugins(w http.ResponseWriter) {
	entries := h.registry.List()
	result := make([]pluginListEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, pluginListEntry{
			Name:        e.Manifest.Name,
			Version:     e.Manifest.Version,
			Author:      e.Manifest.Author,
			Description: e.Manifest.Description,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *APIHandler) getPlugin(w http.ResponseWriter, name string) {
	entry, ok := h.registry.Get(name)
	if !ok {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, entry.Manifest)
}

// registerPluginRequest is the JSON body for POST /api/plugins.
type registerPluginRequest struct {
	Manifest *PluginManifest `json:"manifest"`
	Source   string          `json:"source,omitempty"`
}

func (h *APIHandler) registerPlugin(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req registerPluginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Manifest == nil {
		http.Error(w, "manifest is required", http.StatusBadRequest)
		return
	}

	var comp *dynamic.DynamicComponent
	if req.Source != "" && h.loader != nil {
		c, loadErr := h.loader.LoadFromString(req.Manifest.Name, req.Source)
		if loadErr != nil {
			http.Error(w, "failed to load component: "+loadErr.Error(), http.StatusUnprocessableEntity)
			return
		}
		comp = c
	}

	if err := h.registry.Register(req.Manifest, comp, ""); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, http.StatusCreated, req.Manifest)
}

func (h *APIHandler) deletePlugin(w http.ResponseWriter, name string) {
	if err := h.registry.Unregister(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
