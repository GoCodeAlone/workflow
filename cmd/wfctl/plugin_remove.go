package main

import (
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
)

// pluginExistsInManifest returns true if pluginName is listed in the manifest at manifestPath.
// Returns false when the file doesn't exist or the plugin isn't in it.
func pluginExistsInManifest(name, manifestPath string) bool {
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		return false
	}
	for _, p := range m.Plugins {
		if p.Name == name {
			return true
		}
	}
	return false
}

// removeFromManifestAndLockfile removes a plugin entry from wfctl.yaml and
// .wfctl-lock.yaml if those files exist. Silently no-ops when files are absent.
func removeFromManifestAndLockfile(name, manifestPath, lockPath string) error {
	// Remove from manifest if it exists.
	if _, err := os.Stat(manifestPath); err == nil {
		m, err := config.LoadWfctlManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("load manifest: %w", err)
		}
		filtered := m.Plugins[:0]
		for _, p := range m.Plugins {
			if p.Name != name {
				filtered = append(filtered, p)
			}
		}
		m.Plugins = filtered
		if err := config.SaveWfctlManifest(manifestPath, m); err != nil {
			return fmt.Errorf("save manifest: %w", err)
		}
	}

	// Remove from lockfile if it exists.
	if _, err := os.Stat(lockPath); err == nil {
		lf, err := config.LoadWfctlLockfile(lockPath)
		if err != nil {
			return fmt.Errorf("load lockfile: %w", err)
		}
		delete(lf.Plugins, name)
		if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
			return fmt.Errorf("save lockfile: %w", err)
		}
	}

	return nil
}
