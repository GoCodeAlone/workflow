package sdk

import (
	"encoding/json"
	"fmt"

	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
)

// EmbedManifest parses plugin.json content (typically loaded via go:embed) into
// the canonical *plugin.PluginManifest type and runs the canonical Validate().
//
// Validate() requires ALL of: Name, Version, Author, Description (verified at
// plugin/manifest.go:183-201). A plugin.json missing any of these is rejected.
// This matches the same contract enforced by the engine's manager.go on disk
// load — there is no "minimal" path. If you cannot supply Author or
// Description at build time, the plugin should not ship a release.
//
// Plugin authors write:
//
//	//go:embed plugin.json
//	var manifestJSON []byte
//	var manifest = sdk.MustEmbedManifest(manifestJSON)
//
// The returned manifest is passed into sdk.Serve via WithManifestProvider, or
// into sdk.IaCServeOptions.ManifestProvider for ServeIaCPlugin. The SDK wires
// it into the appropriate GetManifest gRPC handler so the workflow engine sees
// a fully-populated manifest at plugin registration time.
//
// Parses via the canonical *plugin.PluginManifest (camelCase JSON tags matching
// the plugin.json authoring convention), NOT directly into *pb.Manifest (which
// has snake_case proto JSON tags and would silently drop configMutable etc.).
//
// For production code paths that need to recover from a missing/malformed
// plugin.json (e.g., plugins that ship with multiple manifest candidates),
// prefer EmbedManifest with explicit error handling over MustEmbedManifest.
// MustEmbedManifest panics at process startup, which surfaces misconfiguration
// loudly but is unrecoverable.
func EmbedManifest(content []byte) (*pluginpkg.PluginManifest, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("parse embedded plugin.json: empty content")
	}
	var m pluginpkg.PluginManifest
	if err := json.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("parse embedded plugin.json: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("validate embedded plugin.json: %w", err)
	}
	return &m, nil
}

// MustEmbedManifest panics on parse or validation error. Intended for
// package-level var initialization in plugin main packages — failure indicates
// a build-time misconfiguration that must be fixed before the binary ships.
//
// WARNING: panic semantics make this a process-startup canary. Plugin
// authors who need graceful degradation (e.g., to recover from a
// missing/malformed plugin.json in tooling-only code paths) should use
// EmbedManifest with explicit error handling instead.
func MustEmbedManifest(content []byte) *pluginpkg.PluginManifest {
	p, err := EmbedManifest(content)
	if err != nil {
		panic(err)
	}
	return p
}
