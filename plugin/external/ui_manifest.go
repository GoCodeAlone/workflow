package external

// UIManifest describes the UI contribution of a go-plugin UI plugin.
// Place this as "ui.json" in the plugin directory alongside "plugin.json".
//
// # Directory Layout
//
//	plugins/
//	  my-ui-plugin/
//	    my-ui-plugin  (binary, required for API handlers; optional for asset-only plugins)
//	    plugin.json   (gRPC plugin manifest; required when binary is present)
//	    ui.json       (UI manifest: nav items and asset location)
//	    assets/       (static files: HTML, CSS, JS, images, etc.)
//	      index.html
//	      main.js
//
// # Asset Versioning
//
// Include a version hash in asset filenames (e.g. main.abc123.js) and
// reference them from index.html so browsers pick up new files after
// hot-deploy.
//
// # Hot-Reload
//
// After updating assets or ui.json, call:
//
//	POST /api/v1/plugins/ui/{name}/reload
//
// This re-reads the manifest and serves the new files without restarting the
// workflow engine.
//
// # Hot-Deploy
//
// Replace the plugin binary and/or assets directory with the new version,
// then call the reload endpoint above.
type UIManifest struct {
	// Name is the plugin identifier. Must match the plugin directory name.
	Name string `json:"name"`
	// Version is the plugin version string (e.g. "1.0.0").
	Version string `json:"version"`
	// Description is a short human-readable description of the plugin.
	Description string `json:"description,omitempty"`
	// NavItems declares navigation entries contributed to the admin UI.
	NavItems []UINavItem `json:"navItems,omitempty"`
	// AssetDir is the subdirectory within the plugin directory that holds
	// static assets. Defaults to "assets" when empty.
	AssetDir string `json:"assetDir,omitempty"`
}

// UINavItem describes a single entry in the admin UI navigation sidebar.
type UINavItem struct {
	// ID is the unique page identifier (e.g. "my-plugin-dashboard").
	ID string `json:"id"`
	// Label is the human-readable navigation label shown in the sidebar.
	Label string `json:"label"`
	// Icon is an emoji or icon token rendered alongside the label.
	Icon string `json:"icon,omitempty"`
	// Category groups navigation entries:
	//   "global"   – always visible top-level entries
	//   "workflow" – shown only when a workflow is open
	//   "plugin"   – plugin-contributed entries (default)
	//   "tools"    – administrative tooling entries
	Category string `json:"category,omitempty"`
	// Order controls the sort position within the category (lower = higher up).
	Order int `json:"order,omitempty"`
	// RequiredRole is the minimum role needed to see this page
	// (e.g. "viewer", "editor", "admin", "operator").
	RequiredRole string `json:"requiredRole,omitempty"`
	// RequiredPermission is a specific permission key required to see this page
	// (e.g. "plugins.manage").
	RequiredPermission string `json:"requiredPermission,omitempty"`
}
