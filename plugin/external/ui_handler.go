package external

import (
	"net/http"
	"sort"
	"strings"
)

// UIPluginHandler provides HTTP API endpoints for managing UI plugins.
//
// Routes registered by RegisterRoutes:
//
//	GET  /api/v1/plugins/ui                       – list loaded UI plugins
//	GET  /api/v1/plugins/ui/available             – list all discovered UI plugins
//	GET  /api/v1/plugins/ui/{name}/manifest       – get a plugin's UI manifest
//	POST /api/v1/plugins/ui/{name}/load           – load a UI plugin
//	POST /api/v1/plugins/ui/{name}/unload         – unload a UI plugin
//	POST /api/v1/plugins/ui/{name}/reload         – hot-reload a UI plugin
//	GET  /api/v1/plugins/ui/{name}/assets/{path…} – serve static assets
type UIPluginHandler struct {
	manager *UIPluginManager
}

// NewUIPluginHandler creates a new handler backed by the given UIPluginManager.
func NewUIPluginHandler(manager *UIPluginManager) *UIPluginHandler {
	return &UIPluginHandler{manager: manager}
}

// RegisterRoutes registers all UI plugin HTTP routes on mux.
func (h *UIPluginHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/plugins/ui", h.handleListLoaded)
	mux.HandleFunc("GET /api/v1/plugins/ui/available", h.handleListAvailable)
	mux.HandleFunc("GET /api/v1/plugins/ui/{name}/manifest", h.handleGetManifest)
	mux.HandleFunc("POST /api/v1/plugins/ui/{name}/load", h.handleLoad)
	mux.HandleFunc("POST /api/v1/plugins/ui/{name}/unload", h.handleUnload)
	mux.HandleFunc("POST /api/v1/plugins/ui/{name}/reload", h.handleReload)
	mux.HandleFunc("GET /api/v1/plugins/ui/{name}/assets/", h.handleServeAssets)
}

// handleListLoaded returns info about all currently loaded UI plugins.
func (h *UIPluginHandler) handleListLoaded(w http.ResponseWriter, _ *http.Request) {
	infos := h.manager.AllUIPluginInfos()
	if infos == nil {
		infos = []UIPluginInfo{}
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	writeOK(w, infos)
}

// handleListAvailable lists all discovered UI plugin directories (whether
// loaded or not).
func (h *UIPluginHandler) handleListAvailable(w http.ResponseWriter, _ *http.Request) {
	names, err := h.manager.DiscoverPlugins()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	sort.Strings(names)

	type entry struct {
		Name   string `json:"name"`
		Loaded bool   `json:"loaded"`
	}
	plugins := make([]entry, 0, len(names))
	for _, name := range names {
		plugins = append(plugins, entry{
			Name:   name,
			Loaded: h.manager.IsLoaded(name),
		})
	}
	writeOK(w, plugins)
}

// handleGetManifest returns the UI manifest for a loaded plugin.
func (h *UIPluginHandler) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	entry, ok := h.manager.GetPlugin(name)
	if !ok {
		writeError(w, http.StatusNotFound, "UI plugin not found or not loaded")
		return
	}
	writeOK(w, entry.Manifest)
}

// handleLoad loads a UI plugin by reading its ui.json from disk.
func (h *UIPluginHandler) handleLoad(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "plugin name is required")
		return
	}
	if err := h.manager.LoadPlugin(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, map[string]string{"name": name, "action": "loaded"})
}

// handleUnload removes a UI plugin from the manager.
func (h *UIPluginHandler) handleUnload(w http.ResponseWriter, r *http.Request) {
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

// handleReload hot-reloads a UI plugin by re-reading its manifest and assets
// from disk. This is the primary mechanism for hot-deploy: copy updated files
// to the plugin directory and call this endpoint.
func (h *UIPluginHandler) handleReload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "plugin name is required")
		return
	}
	if err := h.manager.ReloadPlugin(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, map[string]string{"name": name, "action": "reloaded"})
}

// handleServeAssets serves static asset files for a loaded UI plugin.
// The URL prefix /api/v1/plugins/ui/{name}/assets/ is stripped before
// delegating to an http.FileServer rooted at the plugin's assets directory.
func (h *UIPluginHandler) handleServeAssets(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	assetHandler := h.manager.ServeAssets(name)
	if assetHandler == nil {
		writeError(w, http.StatusNotFound, "UI plugin not found or not loaded")
		return
	}

	// Strip the route prefix so http.FileServer resolves paths relative to
	// the assets root.
	prefix := "/api/v1/plugins/ui/" + name + "/assets"
	stripped := strings.TrimPrefix(r.URL.Path, prefix)
	if stripped == r.URL.Path {
		// Path did not begin with the expected prefix – shouldn't happen.
		http.NotFound(w, r)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = stripped
	assetHandler.ServeHTTP(w, r2)
}
