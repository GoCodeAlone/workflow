// Package admincore provides a NativePlugin that declares the core admin UI
// pages (Dashboard, Editor, Executions, Logs, Events, Marketplace, Templates,
// Environments, Settings). Registering this plugin ensures the admin
// navigation is driven entirely by the plugin system with no static fallbacks.
package admincore

import (
	"database/sql"
	"net/http"

	"github.com/GoCodeAlone/workflow/plugin"
)

func init() {
	plugin.RegisterNativePluginFactory(func(_ *sql.DB, _ map[string]any) plugin.NativePlugin {
		return &Plugin{}
	})
}

// Compile-time interface check.
var _ plugin.NativePlugin = (*Plugin)(nil)

// Plugin declares the built-in admin UI pages. It registers no HTTP routes
// because all core views are rendered entirely in the React frontend.
type Plugin struct{}

func (p *Plugin) Name() string    { return "admin-core" }
func (p *Plugin) Version() string { return "1.0.0" }
func (p *Plugin) Description() string {
	return "Core admin UI views: Dashboard, Editor, Executions, Logs, Events, Marketplace, Templates, Environments, Settings"
}

func (p *Plugin) Dependencies() []plugin.PluginDependency { return nil }
func (p *Plugin) RegisterRoutes(_ *http.ServeMux)         {}
func (p *Plugin) OnEnable(_ plugin.PluginContext) error   { return nil }
func (p *Plugin) OnDisable(_ plugin.PluginContext) error  { return nil }

// UIPages returns the page definitions for all core admin views.
// Icon values are emoji matching the design tokens used in the React UI.
func (p *Plugin) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		// Global pages
		{ID: "dashboard", Label: "Dashboard", Icon: "\U0001F4CA", Category: "global", Order: 0},
		{ID: "editor", Label: "Editor", Icon: "\U0001F4DD", Category: "global", Order: 1},
		{ID: "marketplace", Label: "Marketplace", Icon: "\U0001F6D2", Category: "global", Order: 2},
		{ID: "templates", Label: "Templates", Icon: "\U0001F4C4", Category: "global", Order: 3},
		{ID: "environments", Label: "Environments", Icon: "\u2601\uFE0F", Category: "global", Order: 4},
		{ID: "settings", Label: "Settings", Icon: "\u2699\uFE0F", Category: "global", Order: 6},
		// Workflow-scoped pages (only shown when a workflow is open)
		{ID: "executions", Label: "Executions", Icon: "\u25B6\uFE0F", Category: "workflow", Order: 0},
		{ID: "logs", Label: "Logs", Icon: "\U0001F4C3", Category: "workflow", Order: 1},
		{ID: "events", Label: "Events", Icon: "\u26A1", Category: "workflow", Order: 2},
	}
}
