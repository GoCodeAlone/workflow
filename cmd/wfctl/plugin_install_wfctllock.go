package main

import (
	"fmt"
	"os"
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
		scrubbed, changed := scrubbedWfctlLockfileTopLevelSHA256(lf)
		if changed {
			if err := config.SaveWfctlLockfile(lockPath, scrubbed); err != nil {
				return fmt.Errorf("persist scrubbed lockfile: %w", err)
			}
			*lf = *scrubbed
		}
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
		fmt.Fprintf(os.Stderr, "Installing %s@%s...\n", name, entry.Version)

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
			if err := installFromURL(plat.URL, pluginDirVal, plat.SHA256, false); err != nil {
				fmt.Fprintf(os.Stderr, "error installing %s from URL: %v\n", name, err)
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

func scrubbedWfctlLockfileTopLevelSHA256(lf *config.WfctlLockfile) (*config.WfctlLockfile, bool) {
	scrubbed := &config.WfctlLockfile{
		Version:     lf.Version,
		GeneratedAt: lf.GeneratedAt,
		Plugins:     make(map[string]config.WfctlLockPluginEntry, len(lf.Plugins)),
	}
	changed := false
	for name, entry := range lf.Plugins {
		if entry.SHA256 != "" {
			entry.SHA256 = ""
			changed = true
		}
		scrubbed.Plugins[name] = entry
	}
	return scrubbed, changed
}

// currentPlatformKey returns the GOOS-GOARCH key used in WfctlLockPlatform maps.
func currentPlatformKey() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}
