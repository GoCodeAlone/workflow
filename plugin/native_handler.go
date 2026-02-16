package plugin

import "net/http"

const nativePluginAPIPrefix = "/api/v1/admin/plugins"

// NativeHandler serves HTTP endpoints for native plugin discovery and route dispatch.
// It delegates all behavior to the PluginManager.
type NativeHandler struct {
	manager *PluginManager
}

// NewNativeHandler creates a new handler backed by a PluginManager.
func NewNativeHandler(manager *PluginManager) *NativeHandler {
	return &NativeHandler{manager: manager}
}

// ServeHTTP implements http.Handler by delegating to the PluginManager.
func (h *NativeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.manager.ServeHTTP(w, r)
}
