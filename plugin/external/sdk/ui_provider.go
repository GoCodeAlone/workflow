package sdk

import "github.com/GoCodeAlone/workflow/plugin/external/contract"

type UIManifest = contract.UIManifest
type UINavItem = contract.UINavItem

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
// The UI manifest types are aliases for the shared contract package so plugins
// can use the SDK without importing host-side external plugin adapters.
type UIProvider interface {
	// UIManifest returns the UI manifest for this plugin.
	UIManifest() UIManifest
}
