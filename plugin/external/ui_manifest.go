package external

import "github.com/GoCodeAlone/workflow/plugin/external/contract"

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
type UIManifest = contract.UIManifest

// UINavItem describes a single entry in the admin UI navigation sidebar.
type UINavItem = contract.UINavItem
