package plugin

import (
	"database/sql"
)

// NativePluginFactory creates a NativePlugin given a database connection and
// a set of optional dependencies keyed by name. The factory may return nil
// if its prerequisites are not met (e.g., no database available).
type NativePluginFactory func(db *sql.DB, deps map[string]any) NativePlugin

// nativePluginFactories is the global registry of built-in NativePlugin factories.
var nativePluginFactories []NativePluginFactory

// RegisterNativePluginFactory adds a factory to the global built-in NativePlugin registry.
// Call this from init() in plugin packages that provide standalone NativePlugins.
func RegisterNativePluginFactory(f NativePluginFactory) {
	nativePluginFactories = append(nativePluginFactories, f)
}

// BuiltinNativePlugins creates all registered built-in NativePlugins.
// The db parameter is the shared database connection. Additional dependencies
// are passed as key-value pairs in the deps map. Plugins that return nil
// are skipped.
func BuiltinNativePlugins(db *sql.DB, deps map[string]any) []NativePlugin {
	var plugins []NativePlugin
	for _, f := range nativePluginFactories {
		np := f(db, deps)
		if np != nil {
			plugins = append(plugins, np)
		}
	}
	return plugins
}
