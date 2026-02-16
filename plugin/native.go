package plugin

import "net/http"

// NativePlugin is a compiled-in plugin that provides HTTP handlers and UI page metadata.
type NativePlugin interface {
	Name() string
	Version() string
	Description() string
	UIPages() []UIPageDef
	RegisterRoutes(mux *http.ServeMux)
}

// UIPageDef describes a UI page contributed by a plugin.
type UIPageDef struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Icon     string `json:"icon"`
	Category string `json:"category"`
}
