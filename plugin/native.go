package plugin

import (
	"database/sql"
	"log/slog"
	"net/http"
)

// PluginDependency declares a dependency on another plugin.
type PluginDependency struct {
	Name       string `json:"name"`       // required plugin name
	MinVersion string `json:"minVersion"` // semver constraint, empty = any version
}

// PluginContext provides shared resources to plugins during lifecycle events.
type PluginContext struct {
	App     interface{} // modular.Application — use interface{} to avoid import cycle
	DB      *sql.DB
	Logger  *slog.Logger
	DataDir string
}

// UIPageDef describes a UI page contributed by a plugin.
type UIPageDef struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Icon     string `json:"icon"`
	Category string `json:"category"` // "global", "workflow", "plugin"
	Order    int    `json:"order"`
}

// NativePlugin is a compiled-in plugin that provides HTTP handlers, UI page metadata,
// and lifecycle hooks with dependency declarations.
type NativePlugin interface {
	Name() string
	Version() string
	Description() string
	Dependencies() []PluginDependency
	UIPages() []UIPageDef
	RegisterRoutes(mux *http.ServeMux)
	OnEnable(ctx PluginContext) error
	OnDisable(ctx PluginContext) error
}
