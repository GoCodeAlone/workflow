package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// loadPluginManifestPath resolves a --plugin-manifest argument and loads any
// type / step-schema declarations it contributes.
//
// Accepted shapes:
//   - A path to a plugin.json file: loaded directly.
//   - A path to a directory containing plugin.json: loaded as a single plugin.
//   - A path to a directory whose immediate subdirectories contain plugin.json
//     files: each subdir loaded (matches --plugin-dir semantics).
//
// A missing path is an error so a typo doesn't silently produce a half-loaded
// validation context.
func loadPluginManifestPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("--plugin-manifest %q: %w", path, err)
	}
	if !info.IsDir() {
		if err := schema.LoadPluginTypesFromManifest(path); err != nil {
			return err
		}
		schema.LoadPluginStepSchemasFromManifest(path)
		return nil
	}

	// Directory: prefer a plugin.json directly inside it; otherwise fall back to
	// the legacy "<dir>/<name>/plugin.json" layout that LoadPluginTypesFromDir
	// supports.
	manifest := filepath.Join(path, "plugin.json")
	if _, statErr := os.Stat(manifest); statErr == nil {
		if err := schema.LoadPluginTypesFromManifest(manifest); err != nil {
			return err
		}
		schema.LoadPluginStepSchemasFromManifest(manifest)
		return nil
	}
	if err := schema.LoadPluginTypesFromDir(path); err != nil {
		return fmt.Errorf("--plugin-manifest %q: %w", path, err)
	}
	schema.LoadPluginStepSchemasFromDir(path)
	return nil
}

// autoResolveRequiredPlugins walks conventional locations near cfgPath looking
// for a plugin.json that matches each entry in requires.plugins[]. The first
// match wins; failure to find a match is not an error so the existing
// "unknown type" message still fires when the operator forgot to check out the
// plugin source.
//
// Search order, per plugin name, with cfgDir = directory containing cfgPath:
//
//	cfgDir/<name>/plugin.json
//	cfgDir/plugins/<name>/plugin.json
//	cfgDir/providers/<name>/plugin.json
//	(repeat each form against cfgDir/.., cfgDir/../.., cfgDir/../../..)
//
// Hidden ("."), underscore-prefixed, node_modules, vendor, and _worktrees
// directories are skipped silently to avoid noisy false positives in larger
// workspaces.
func autoResolveRequiredPlugins(cfgPath string, plugins []config.PluginRequirement) {
	if len(plugins) == 0 {
		return
	}
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		return
	}
	cfgDir := filepath.Dir(abs)

	for _, p := range plugins {
		if p.Name == "" {
			continue
		}
		manifest := findLocalPluginManifest(cfgDir, p.Name)
		if manifest == "" {
			continue
		}
		if err := schema.LoadPluginTypesFromManifest(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "  WARN auto-resolved manifest %s for %s failed to load: %v\n", manifest, p.Name, err)
			continue
		}
		schema.LoadPluginStepSchemasFromManifest(manifest)
		fmt.Fprintf(os.Stderr, "  Auto-resolved plugin %s from %s\n", p.Name, manifest)
	}
}

// searchSubdirs is the set of conventional subdirectories under a candidate
// root that may hold plugin checkouts. Empty string represents the root itself.
var searchSubdirs = []string{"", "plugins", "providers"}

// ancestorDepth is the number of parent directories above the config file
// considered during auto-resolution. Three levels covers the common
// workspace/scenario-repo/apps/<name>/workflow.yaml layout so a config nested
// three deep can still find sibling plugin checkouts at the workspace root,
// without scanning arbitrarily far up the filesystem.
const ancestorDepth = 3

var autoResolveSkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"_worktrees":   true,
	".git":         true,
}

func findLocalPluginManifest(cfgDir, name string) string {
	root := cfgDir
	for level := 0; level <= ancestorDepth; level++ {
		if shouldSkipAutoResolveDir(root) {
			break
		}
		for _, sub := range searchSubdirs {
			candidate := filepath.Join(root, sub, name, "plugin.json")
			if sub == "" {
				candidate = filepath.Join(root, name, "plugin.json")
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return ""
}

func shouldSkipAutoResolveDir(dir string) bool {
	base := filepath.Base(dir)
	if base == "." || base == "/" || base == "" {
		return false
	}
	if autoResolveSkipDirs[base] {
		return true
	}
	if len(base) > 1 && (base[0] == '.' || base[0] == '_') {
		return true
	}
	return false
}
