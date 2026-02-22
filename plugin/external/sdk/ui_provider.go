package sdk

import ext "github.com/GoCodeAlone/workflow/plugin/external"

// UIProvider is an optional interface that PluginProvider implementations can
// satisfy to declare UI assets and navigation contributions.
//
// If a PluginProvider implements UIProvider, the SDK Serve() function will
// write a "ui.json" file to the plugin's working directory on first start
// (if one does not already exist). Alternatively, authors can maintain
// "ui.json" manually without implementing this interface.
//
// # Type aliases
//
// The UI manifest types (UIManifest, UINavItem) are defined in the
// github.com/GoCodeAlone/workflow/plugin/external package so that both the
// host engine and plugin processes share the same type definitions without
// introducing an import cycle.
type UIProvider interface {
	// UIManifest returns the UI manifest for this plugin.
	UIManifest() ext.UIManifest
}
