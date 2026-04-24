package main

import (
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
)

// updateManifestVersion updates a plugin's version in wfctl.yaml and re-locks.
// Returns an error if the plugin is not found in the manifest.
func updateManifestVersion(name, newVersion, manifestPath, lockPath string) error {
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return fmt.Errorf("manifest %s not found; run wfctl plugin add first", manifestPath)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	found := false
	for i, p := range m.Plugins {
		if p.Name == name {
			m.Plugins[i].Version = newVersion
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("plugin %q not found in manifest; use wfctl plugin add first", name)
	}

	if err := config.SaveWfctlManifest(manifestPath, m); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	// Re-lock so sha256 is cleared for the new version.
	return runPluginLockFromManifest(manifestPath, lockPath)
}
