package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const wfctlYAMLPath = ".wfctl.yaml"

// PluginLockEntry records a pinned plugin version in the lockfile.
type PluginLockEntry struct {
	Version    string `yaml:"version"`
	Repository string `yaml:"repository,omitempty"`
	SHA256     string `yaml:"sha256,omitempty"`
	Registry   string `yaml:"registry,omitempty"`
}

// PluginLockfile represents the plugins section of .wfctl.yaml.
// It preserves all other keys in the file for safe round-trip writes.
type PluginLockfile struct {
	Plugins map[string]PluginLockEntry
	raw     map[string]any // preserved for round-trip writes
}

// loadPluginLockfile reads path and returns the plugins section.
// If the file does not exist, an empty lockfile is returned without error.
func loadPluginLockfile(path string) (*PluginLockfile, error) {
	lf := &PluginLockfile{
		Plugins: make(map[string]PluginLockEntry),
		raw:     make(map[string]any),
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return lf, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &lf.raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// Extract and parse the plugins section if present.
	if pluginsRaw, ok := lf.raw["plugins"]; ok && pluginsRaw != nil {
		pluginsData, err := yaml.Marshal(pluginsRaw)
		if err != nil {
			return nil, fmt.Errorf("re-marshal plugins section: %w", err)
		}
		if err := yaml.Unmarshal(pluginsData, &lf.Plugins); err != nil {
			return nil, fmt.Errorf("parse plugins section: %w", err)
		}
	}
	return lf, nil
}

// installFromLockfile reads .wfctl.yaml and installs all plugins in the
// plugins section. If no lockfile is found, it prints a helpful message.
func installFromLockfile(pluginDir, cfgPath string) error {
	lf, err := loadPluginLockfile(wfctlYAMLPath)
	if err != nil {
		return fmt.Errorf("load lockfile: %w", err)
	}
	if len(lf.Plugins) == 0 {
		fmt.Println("No plugins pinned in .wfctl.yaml.")
		fmt.Println("Run 'wfctl plugin install <name>@<version>' to install and pin a plugin.")
		return nil
	}
	var failed []string
	for name, entry := range lf.Plugins {
		fmt.Fprintf(os.Stderr, "Installing %s %s...\n", name, entry.Version)
		installArgs := []string{"--plugin-dir", pluginDir}
		if cfgPath != "" {
			installArgs = append(installArgs, "--config", cfgPath)
		}
		if entry.Registry != "" {
			installArgs = append(installArgs, "--registry", entry.Registry)
		}
		// Pass just the name (no @version) so runPluginInstall does not
		// trigger lockfile updates that would overwrite the pinned entry
		// before we verify the checksum.
		installArgs = append(installArgs, name)
		if err := runPluginInstall(installArgs); err != nil {
			fmt.Fprintf(os.Stderr, "error installing %s: %v\n", name, err)
			failed = append(failed, name)
			continue
		}
		if entry.SHA256 != "" {
			pluginInstallDir := filepath.Join(pluginDir, name)
			if verifyErr := verifyInstalledChecksum(pluginInstallDir, name, entry.SHA256); verifyErr != nil {
				fmt.Fprintf(os.Stderr, "CHECKSUM MISMATCH for %s: %v — removing plugin\n", name, verifyErr)
				_ = os.RemoveAll(pluginInstallDir)
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

// updateLockfileWithChecksum adds or updates a plugin entry in .wfctl.yaml with SHA-256 checksum.
// The sha256Hash must be the hash of the installed binary, not the download archive.
// Silently no-ops if the lockfile cannot be read or written (install still succeeds).
func updateLockfileWithChecksum(pluginName, version, repository, registry, sha256Hash string) {
	lf, err := loadPluginLockfile(wfctlYAMLPath)
	if err != nil {
		return
	}
	if lf.Plugins == nil {
		lf.Plugins = make(map[string]PluginLockEntry)
	}
	lf.Plugins[pluginName] = PluginLockEntry{
		Version:    version,
		Repository: repository,
		Registry:   registry,
		SHA256:     sha256Hash,
	}
	_ = lf.Save(wfctlYAMLPath)
}

// Save writes the lockfile back to path, updating the plugins section while
// preserving all other fields (project, git, deploy, etc.).
func (lf *PluginLockfile) Save(path string) error {
	if lf.raw == nil {
		lf.raw = make(map[string]any)
	}
	// Re-encode the typed plugins map into a yaml-compatible representation.
	pluginsData, err := yaml.Marshal(lf.Plugins)
	if err != nil {
		return fmt.Errorf("marshal plugins: %w", err)
	}
	var pluginsRaw any
	if err := yaml.Unmarshal(pluginsData, &pluginsRaw); err != nil {
		return fmt.Errorf("re-unmarshal plugins: %w", err)
	}
	lf.raw["plugins"] = pluginsRaw

	data, err := yaml.Marshal(lf.raw)
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	return os.WriteFile(path, data, 0600) //nolint:gosec // G306: .wfctl.yaml is user-owned project config
}
