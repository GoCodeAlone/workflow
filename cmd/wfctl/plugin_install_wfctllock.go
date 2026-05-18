package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// installFromWfctlLockfile installs all plugins recorded in a config.WfctlLockfile.
// For each plugin:
//   - If a platform URL is recorded, download from that URL and verify its archive SHA.
//   - Top-level sha256 is ignored in the new-format lockfile to avoid recording
//     or enforcing host-specific binary hashes.
//   - If no platform URL, fall back to registry lookup via the existing install path.
//
// lockPath is the on-disk path for .wfctl-lock.yaml. After each successful install
// the new-format lockfile is re-saved so that the old-format write performed by
// installFromURL/updateLockfileWithChecksum does not corrupt the new-format file.
//
// This is the authoritative install path when .wfctl-lock.yaml (new format) is present.
func installFromWfctlLockfile(pluginDirVal, lockPath string, lf *config.WfctlLockfile) error {
	if len(lf.Plugins) == 0 {
		fmt.Println("No plugins pinned in .wfctl-lock.yaml.")
		return nil
	}

	if lockPath != "" {
		scrubbed := scrubbedWfctlLockfileTopLevelSHA256(lf)
		if err := config.SaveWfctlLockfile(lockPath, scrubbed); err != nil {
			return fmt.Errorf("persist scrubbed lockfile: %w", err)
		}
		*lf = *scrubbed
	}

	// Sort plugin names for deterministic install order.
	names := make([]string, 0, len(lf.Plugins))
	for name := range lf.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	var failed []string
	for _, name := range names {
		entry := lf.Plugins[name]

		installed := false

		// If we have platform-specific URL, install from that URL.
		platKey := currentPlatformKey()
		if len(entry.Platforms) > 0 {
			plat, ok := entry.Platforms[platKey]
			if !ok {
				errMsg := fmt.Sprintf("%s (missing current platform %s in lockfile)", name, platKey)
				fmt.Fprintf(os.Stderr, "error installing %s: %s\n", name, errMsg)
				failed = append(failed, errMsg)
				continue
			}
			if plat.URL == "" {
				errMsg := fmt.Sprintf("%s (missing URL for current platform %s in lockfile)", name, platKey)
				fmt.Fprintf(os.Stderr, "error installing %s: %s\n", name, errMsg)
				failed = append(failed, errMsg)
				continue
			}
			if cached, err := installedPluginSatisfiesLock(pluginDirVal, name, entry, platKey, plat); err != nil {
				fmt.Fprintf(os.Stderr, "warning: cached install for %s is not reusable: %v\n", name, err)
			} else if cached {
				fmt.Fprintf(os.Stderr, "Using cached %s@%s from %s\n", name, entry.Version, filepath.Join(pluginDirVal, normalizePluginName(name)))
				installed = true
			}
		}

		if installed {
			continue
		}

		fmt.Fprintf(os.Stderr, "Installing %s@%s...\n", name, entry.Version)

		if len(entry.Platforms) > 0 {
			plat := entry.Platforms[platKey]
			if err := installFromURL(plat.URL, pluginDirVal, plat.SHA256, false); err != nil {
				fmt.Fprintf(os.Stderr, "error installing %s from URL: %v\n", name, err)
				failed = append(failed, fmt.Sprintf("%s (%v)", name, err))
				continue
			}
			if err := writeLockfileInstallMetadata(pluginDirVal, name, entry, platKey, plat); err != nil {
				fmt.Fprintf(os.Stderr, "error recording install metadata for %s: %v\n", name, err)
				failed = append(failed, fmt.Sprintf("%s (%v)", name, err))
				continue
			}
			installed = true
		}

		if !installed {
			// Fall back to name@version registry install.
			spec := name
			if entry.Version != "" {
				spec = name + "@" + entry.Version
			}
			if err := runPluginInstall([]string{"--plugin-dir", pluginDirVal, spec}); err != nil {
				fmt.Fprintf(os.Stderr, "error installing %s: %v\n", name, err)
				failed = append(failed, fmt.Sprintf("%s (%v)", name, err))
				continue
			}
		}

		// Re-save the scrubbed new-format lockfile after each successful install
		// so platform metadata remains authoritative even if lower-level install
		// paths attempt lockfile maintenance.
		if lockPath != "" {
			if saveErr := config.SaveWfctlLockfile(lockPath, lf); saveErr != nil {
				return fmt.Errorf("persist scrubbed lockfile after installing %s: %w", name, saveErr)
			}
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to install: %s", strings.Join(failed, ", "))
	}
	return nil
}

func scrubbedWfctlLockfileTopLevelSHA256(lf *config.WfctlLockfile) *config.WfctlLockfile {
	scrubbed := &config.WfctlLockfile{
		Version:     lf.Version,
		GeneratedAt: lf.GeneratedAt,
		Plugins:     make(map[string]config.WfctlLockPluginEntry, len(lf.Plugins)),
	}
	for name, entry := range lf.Plugins {
		entry.SHA256 = ""
		scrubbed.Plugins[name] = entry
	}
	return scrubbed
}

// currentPlatformKey returns the GOOS-GOARCH key used in WfctlLockPlatform maps.
func currentPlatformKey() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

const lockfileInstallMetadataName = ".wfctl-install.json"

type lockfileInstallMetadata struct {
	Version  string `json:"version"`
	Source   string `json:"source,omitempty"`
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

func installedPluginSatisfiesLock(pluginDir, lockName string, entry config.WfctlLockPluginEntry, platform string, plat config.WfctlLockPlatform) (bool, error) {
	installName := normalizePluginName(lockName)
	installDir := filepath.Join(pluginDir, installName)
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("stat %s: %w", installDir, err)
	}
	if err := verifyInstalledPlugin(installDir, installName); err != nil {
		return false, err
	}
	if err := verifyInstalledVersion(installDir, entry.Version); err != nil {
		return false, err
	}

	data, err := os.ReadFile(filepath.Join(installDir, lockfileInstallMetadataName))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read install metadata: %w", err)
	}
	var meta lockfileInstallMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return false, fmt.Errorf("parse install metadata: %w", err)
	}
	if !samePluginVersion(meta.Version, entry.Version) {
		return false, nil
	}
	if meta.Source != entry.Source || meta.Platform != platform || meta.URL != plat.URL || !strings.EqualFold(meta.SHA256, plat.SHA256) {
		return false, nil
	}
	return true, nil
}

func writeLockfileInstallMetadata(pluginDir, lockName string, entry config.WfctlLockPluginEntry, platform string, plat config.WfctlLockPlatform) error {
	installDir := filepath.Join(pluginDir, normalizePluginName(lockName))
	meta := lockfileInstallMetadata{
		Version:  entry.Version,
		Source:   entry.Source,
		Platform: platform,
		URL:      plat.URL,
		SHA256:   strings.ToLower(plat.SHA256),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(installDir, lockfileInstallMetadataName), append(data, '\n'), 0o600)
}
