package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/GoCodeAlone/workflow/config"
)

// pluginExistsInLockfile returns (true, nil) when name (or its normalized form)
// is recorded in the lockfile. Returns (false, nil) when the file doesn't exist
// or the plugin isn't in it. Returns (false, err) on parse or permission errors.
func pluginExistsInLockfile(name, lockPath string) (bool, error) {
	lf, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	normName := normalizePluginName(name)
	for k := range lf.Plugins {
		if k == name || normalizePluginName(k) == normName {
			return true, nil
		}
	}
	return false, nil
}

// pluginExistsInManifest returns (true, nil) when name (or its normalized form)
// is listed in the manifest. Returns (false, nil) when the file doesn't exist or
// the plugin isn't in it. Returns (false, err) on parse or permission errors.
func pluginExistsInManifest(name, manifestPath string) (bool, error) {
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	normName := normalizePluginName(name)
	for _, p := range m.Plugins {
		if p.Name == name || normalizePluginName(p.Name) == normName {
			return true, nil
		}
	}
	return false, nil
}

// removeFromManifestAndLockfile removes a plugin entry from wfctl.yaml and
// .wfctl-lock.yaml if those files exist. Silently no-ops when files are absent.
// Both the raw name and its normalized form are matched so that "foo" and
// "workflow-plugin-foo" refer to the same plugin.
func removeFromManifestAndLockfile(name, manifestPath, lockPath string) error {
	normName := normalizePluginName(name)

	// Remove from manifest if it exists.
	if _, err := os.Stat(manifestPath); err == nil {
		m, err := config.LoadWfctlManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("load manifest: %w", err)
		}
		filtered := make([]config.WfctlPluginEntry, 0, len(m.Plugins))
		for _, p := range m.Plugins {
			if p.Name != name && normalizePluginName(p.Name) != normName {
				filtered = append(filtered, p)
			}
		}
		m.Plugins = filtered
		if err := config.SaveWfctlManifest(manifestPath, m); err != nil {
			return fmt.Errorf("save manifest: %w", err)
		}
	}

	// Remove from lockfile if it exists. Lockfile keys may use either full names
	// or short names, so normalize both sides before comparing.
	if _, err := os.Stat(lockPath); err == nil {
		lf, err := config.LoadWfctlLockfile(lockPath)
		if err != nil {
			return fmt.Errorf("load lockfile: %w", err)
		}
		for k := range lf.Plugins {
			if k == name || normalizePluginName(k) == normName {
				delete(lf.Plugins, k)
			}
		}
		if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
			return fmt.Errorf("save lockfile: %w", err)
		}
	}

	return nil
}
