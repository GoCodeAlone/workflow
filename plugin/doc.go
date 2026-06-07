// Package plugin contains Workflow's plugin manifest, registry, loading, and
// installation primitives.
//
// Host-side code normally starts with PluginManifest values loaded from
// plugin.json files, then uses Manager, Loader, or Registry implementations to
// resolve plugin metadata and executables. Plugin authors usually consume the
// higher-level packages under plugin/external/sdk and plugin/sdk instead.
package plugin
