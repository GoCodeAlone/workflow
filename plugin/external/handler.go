package external

import (
	"encoding/json"
	"net/http"
	"sort"
)

// PluginHandler provides HTTP API endpoints for managing external plugins.
type PluginHandler struct {
	manager *ExternalPluginManager
}

// NewPluginHandler creates a new handler backed by the given external plugin manager.
func NewPluginHandler(manager *ExternalPluginManager) *PluginHandler {
	return &PluginHandler{manager: manager}
}

// RegisterRoutes registers the external plugin management HTTP routes on the given mux.
func (h *PluginHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/plugins/external", h.handleListAvailable)
	mux.HandleFunc("GET /api/v1/plugins/external/loaded", h.handleListLoaded)
	mux.HandleFunc("POST /api/v1/plugins/external/{name}/load", h.handleLoad)
	mux.HandleFunc("POST /api/v1/plugins/external/{name}/unload", h.handleUnload)
	mux.HandleFunc("POST /api/v1/plugins/external/{name}/reload", h.handleReload)
}

// apiResponse is the standard JSON response envelope.
type apiResponse struct {
	Status string `json:"status"`
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, statusCode int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, apiResponse{Status: "ok", Data: data})
}

func writeError(w http.ResponseWriter, statusCode int, msg string) {
	writeJSON(w, statusCode, apiResponse{Status: "error", Error: msg})
}

// handleListAvailable returns all discovered external plugins.
func (h *PluginHandler) handleListAvailable(w http.ResponseWriter, _ *http.Request) {
	names, err := h.manager.DiscoverPlugins()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	sort.Strings(names)

	type pluginInfo struct {
		Name   string `json:"name"`
		Loaded bool   `json:"loaded"`
	}

	plugins := make([]pluginInfo, 0, len(names))
	for _, name := range names {
		plugins = append(plugins, pluginInfo{
			Name:   name,
			Loaded: h.manager.IsLoaded(name),
		})
	}

	writeOK(w, plugins)
}

// handleListLoaded returns all currently loaded external plugins.
func (h *PluginHandler) handleListLoaded(w http.ResponseWriter, _ *http.Request) {
	names := h.manager.LoadedPlugins()
	if names == nil {
		names = []string{}
	}
	sort.Strings(names)
	writeOK(w, names)
}

// handleLoad loads an external plugin by name.
func (h *PluginHandler) handleLoad(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "plugin name is required")
		return
	}

	_, err := h.manager.LoadPlugin(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeOK(w, map[string]string{"name": name, "action": "loaded"})
}

// handleUnload unloads an external plugin by name.
func (h *PluginHandler) handleUnload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "plugin name is required")
		return
	}

	if err := h.manager.UnloadPlugin(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeOK(w, map[string]string{"name": name, "action": "unloaded"})
}

// handleReload reloads an external plugin by name (unload + load).
func (h *PluginHandler) handleReload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "plugin name is required")
		return
	}

	_, err := h.manager.ReloadPlugin(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeOK(w, map[string]string{"name": name, "action": "reloaded"})
}
