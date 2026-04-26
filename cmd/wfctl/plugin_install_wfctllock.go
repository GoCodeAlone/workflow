package main

import (
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
//   - If a platform URL is recorded, download from that URL.
//   - After install, verify sha256 when non-empty (hard fail on mismatch).
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

		// Normalize name for filesystem paths — the install layer uses short names
		// (e.g. "digitalocean") while the manifest/lockfile stores full names
		// (e.g. "workflow-plugin-digitalocean").
		fsName := normalizePluginName(name)
		installed := false

		// If we have platform-specific URL, install from that URL.
		platKey := currentPlatformKey()
		if plat, ok := entry.Platforms[platKey]; ok && plat.URL != "" {
			destDir := filepath.Join(pluginDirVal, fsName)
			// Only skip download-level integrity enforcement when a binary hash is
			// recorded for post-install verification. Without a binary hash, allow
			// GitHub release URLs to auto-verify via checksums.txt; non-GitHub URLs
			// without a hash fail closed to prevent unverified installs.
			skipChecksum := expectedWfctlLockfileChecksum(entry) != ""
			if err := installFromURL(plat.URL, pluginDirVal, "", skipChecksum); err != nil {
				fmt.Fprintf(os.Stderr, "error installing %s from URL: %v\n", name, err)
				failed = append(failed, name)
				continue
			}
			// Verify platform-specific sha256 if present.
			if plat.SHA256 != "" {
				binary := filepath.Join(destDir, fsName)
				got, err := hashFileSHA256(binary)
				if err != nil {
					fmt.Fprintf(os.Stderr, "CHECKSUM ERROR for %s: %v\n", name, err)
					_ = os.RemoveAll(destDir)
					failed = append(failed, name)
					continue
				}
				if !strings.EqualFold(got, plat.SHA256) {
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

		// Verify the installed binary against the current platform checksum when
		// present; fall back to the top-level checksum for legacy lockfiles.
		if expectedSHA256 := expectedWfctlLockfileChecksum(entry); expectedSHA256 != "" {
			destDir := filepath.Join(pluginDirVal, fsName)
			if verifyErr := verifyInstalledChecksum(destDir, fsName, expectedSHA256); verifyErr != nil {
				fmt.Fprintf(os.Stderr, "CHECKSUM MISMATCH for %s: %v\n", name, verifyErr)
				_ = os.RemoveAll(destDir)
				failed = append(failed, name)
				continue
			}
		}

		// Re-save the new-format lockfile after each successful install.
		// installFromURL and runPluginInstall internally call updateLockfileWithChecksum,
		// which serializes the OLD PluginLockfile format and can strip source/platforms
		// fields from the on-disk .wfctl-lock.yaml. Overwrite it here with the correct
		// new format, capturing the binary sha256 while we're at it.
		if lockPath != "" {
			destDir := filepath.Join(pluginDirVal, fsName)
			binaryPath := filepath.Join(destDir, fsName)
			if sha, hashErr := hashFileSHA256(binaryPath); hashErr == nil && !strings.EqualFold(sha, expectedWfctlLockfileChecksum(entry)) {
				e := lf.Plugins[name]
				if plat, ok := e.Platforms[currentPlatformKey()]; ok {
					plat.SHA256 = sha
					e.Platforms[currentPlatformKey()] = plat
				} else {
					e.SHA256 = sha
				}
				lf.Plugins[name] = e
			}
			if saveErr := config.SaveWfctlLockfile(lockPath, lf); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not persist lockfile after installing %s: %v\n", name, saveErr)
			}
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to install: %s", strings.Join(failed, ", "))
	}
	return nil
}

// verifyWfctlLockfileChecksums checks sha256 of already-installed plugin binaries
// against the lockfile. Platform-specific checksums take precedence over the
// top-level checksum for the current OS/architecture.
// Returns an error if any mismatch is detected.
func verifyWfctlLockfileChecksums(pluginDirVal string, lf *config.WfctlLockfile) error {
	// Sort plugin names for deterministic verification order and predictable error messages.
	names := make([]string, 0, len(lf.Plugins))
	for name := range lf.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	var mismatches []string
	for _, name := range names {
		entry := lf.Plugins[name]
		expectedSHA256 := expectedWfctlLockfileChecksum(entry)
		if expectedSHA256 == "" {
			continue
		}
		// Normalize name for filesystem path — manifest stores full names,
		// install layer uses short names (strips "workflow-plugin-" prefix).
		fsName := normalizePluginName(name)
		destDir := filepath.Join(pluginDirVal, fsName)
		if err := verifyInstalledChecksum(destDir, fsName, expectedSHA256); err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("checksum mismatches:\n  %s", strings.Join(mismatches, "\n  "))
	}
	return nil
}

func expectedWfctlLockfileChecksum(entry config.WfctlLockPluginEntry) string {
	if plat, ok := entry.Platforms[currentPlatformKey()]; ok && plat.SHA256 != "" {
		return plat.SHA256
	}
	return entry.SHA256
}

// currentPlatformKey returns the GOOS-GOARCH key used in WfctlLockPlatform maps.
func currentPlatformKey() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}
