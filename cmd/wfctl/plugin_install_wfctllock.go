package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// installFromWfctlLockfile installs all plugins recorded in a config.WfctlLockfile.
// For each plugin:
//   - If a platform URL is recorded, download from that URL.
//   - After install, verify sha256 when non-empty (hard fail on mismatch).
//   - If no platform URL, fall back to registry lookup via the existing install path.
//
// This is the authoritative install path when .wfctl-lock.yaml (new format) is present.
func installFromWfctlLockfile(pluginDirVal string, lf *config.WfctlLockfile) error {
	if len(lf.Plugins) == 0 {
		fmt.Println("No plugins pinned in .wfctl-lock.yaml.")
		return nil
	}

	var failed []string
	for name, entry := range lf.Plugins {
		fmt.Fprintf(os.Stderr, "Installing %s@%s...\n", name, entry.Version)

		installed := false
		// If we have platform-specific URL, install from that URL.
		platKey := currentPlatformKey()
		if plat, ok := entry.Platforms[platKey]; ok && plat.URL != "" {
			destDir := filepath.Join(pluginDirVal, name)
			if err := installFromURL(plat.URL, pluginDirVal); err != nil {
				fmt.Fprintf(os.Stderr, "error installing %s from URL: %v\n", name, err)
				failed = append(failed, name)
				continue
			}
			// Verify platform-specific sha256 if present.
			if plat.SHA256 != "" {
				binary := filepath.Join(destDir, name)
				got, err := hashFileSHA256(binary)
				if err != nil {
					fmt.Fprintf(os.Stderr, "CHECKSUM ERROR for %s: %v\n", name, err)
					_ = os.RemoveAll(destDir)
					failed = append(failed, name)
					continue
				}
				if got != plat.SHA256 {
					fmt.Fprintf(os.Stderr, "CHECKSUM MISMATCH for %s: got %s, want %s\n", name, got, plat.SHA256)
					_ = os.RemoveAll(destDir)
					failed = append(failed, name)
					continue
				}
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
				failed = append(failed, name)
				continue
			}
		}

		// Verify top-level binary sha256 if present.
		if entry.SHA256 != "" {
			destDir := filepath.Join(pluginDirVal, name)
			if verifyErr := verifyInstalledChecksum(destDir, name, entry.SHA256); verifyErr != nil {
				fmt.Fprintf(os.Stderr, "CHECKSUM MISMATCH for %s: %v\n", name, verifyErr)
				_ = os.RemoveAll(destDir)
				failed = append(failed, name)
				continue
			}
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to install: %s", strings.Join(failed, ", "))
	}
	return nil
}

// verifyWfctlLockfileChecksums checks sha256 of already-installed plugin binaries
// against the lockfile. Only checks plugins with non-empty sha256 entries.
// Returns an error if any mismatch is detected.
func verifyWfctlLockfileChecksums(pluginDirVal string, lf *config.WfctlLockfile) error {
	var mismatches []string
	for name, entry := range lf.Plugins {
		if entry.SHA256 == "" {
			continue
		}
		destDir := filepath.Join(pluginDirVal, name)
		if err := verifyInstalledChecksum(destDir, name, entry.SHA256); err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("checksum mismatches:\n  %s", strings.Join(mismatches, "\n  "))
	}
	return nil
}

// currentPlatformKey returns the GOOS-GOARCH key used in WfctlLockPlatform maps.
func currentPlatformKey() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}
