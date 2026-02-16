package plugin

import (
	"net/http"
	"strings"
)

// nativePluginInfo is the JSON representation for the plugin list endpoint.
type nativePluginInfo struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	UIPages     []UIPageDef `json:"ui_pages"`
}

const nativePluginAPIPrefix = "/api/v1/admin/plugins"

// NativeHandler serves HTTP endpoints for native plugin discovery and route dispatch.
type NativeHandler struct {
	registry *NativeRegistry
	mux      *http.ServeMux
}

// NewNativeHandler creates a new handler for native plugin HTTP endpoints.
func NewNativeHandler(registry *NativeRegistry) *NativeHandler {
	return &NativeHandler{
		registry: registry,
	}
}

func (h *NativeHandler) init() {
	if h.mux != nil {
		return
	}
	h.mux = http.NewServeMux()

	// List all plugins
	h.mux.HandleFunc(nativePluginAPIPrefix, h.handleListPlugins)

	// Register each plugin's routes under its prefix
	for _, p := range h.registry.List() {
		prefix := nativePluginAPIPrefix + "/" + p.Name()
		pluginMux := http.NewServeMux()
		p.RegisterRoutes(pluginMux)
		h.mux.Handle(prefix+"/", http.StripPrefix(prefix, pluginMux))
	}
}

// ServeHTTP implements http.Handler.
func (h *NativeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.init()
	h.mux.ServeHTTP(w, r)
}

func (h *NativeHandler) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	// Only handle exact path match for listing
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path != strings.TrimSuffix(nativePluginAPIPrefix, "/") {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	plugins := h.registry.List()
	result := make([]nativePluginInfo, 0, len(plugins))
	for _, p := range plugins {
		uiPages := p.UIPages()
		if uiPages == nil {
			uiPages = []UIPageDef{}
		}
		result = append(result, nativePluginInfo{
			Name:        p.Name(),
			Version:     p.Version(),
			Description: p.Description(),
			UIPages:     uiPages,
		})
	}
	writeJSON(w, http.StatusOK, result)
}
