package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// runPluginLock regenerates the plugin lockfile.
// If wfctl.yaml (manifest) exists, it reads from there.
// Otherwise it falls back to requires.plugins[] in the workflow config.
func runPluginLock(args []string) error {
	fs := flag.NewFlagSet("plugin lock", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to lockfile to write")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Prefer wfctl.yaml manifest if it exists.
	if _, err := os.Stat(*manifestPath); err == nil {
		return runPluginLockFromManifest(*manifestPath, *lockPath)
	}

	// Fall back to legacy workflow.yaml requires.plugins[].
	return runPluginLockLegacy(*cfgPath, *lockPath)
}

// runPluginLockFromManifest regenerates .wfctl-lock.yaml from a wfctl.yaml manifest.
// Existing sha256/platform data is preserved for plugins that are already locked
// at the same version.
func runPluginLockFromManifest(manifestPath, lockPath string) error {
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	// Load existing lockfile so we can preserve sha256 for unchanged versions.
	var existing *config.WfctlLockfile
	if lf, err := config.LoadWfctlLockfile(lockPath); err == nil {
		existing = lf
	}

	newLF := &config.WfctlLockfile{
		Version:     1,
		GeneratedAt: time.Now().UTC(),
		Plugins:     make(map[string]config.WfctlLockPluginEntry),
	}

	registryConfig, registryErr := loadPluginLockRegistryConfig(manifestPath, lockPath)
	if registryErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load registry config while enriching lockfile: %v\n", registryErr)
	}
	var registries *MultiRegistry
	if registryConfig != nil {
		registries = NewMultiRegistry(registryConfig)
	}

	for _, p := range m.Plugins {
		entry := config.WfctlLockPluginEntry{
			Version: p.Version,
			Source:  p.Source,
		}
		var previous *config.WfctlLockPluginEntry
		if existing != nil {
			if prev, ok := existing.Plugins[p.Name]; ok &&
				prev.Version == p.Version &&
				prev.Source == p.Source {
				previous = &prev
			}
		}
		previousHasPlatforms := previous != nil && len(previous.Platforms) > 0

		if registries != nil {
			if platforms, err := lockPlatformsFromRegistry(registries, p.Name, p.Version); err == nil {
				entry.Platforms = platforms
			} else {
				switch {
				case errors.Is(err, errInvalidRegistrySHA256):
					return fmt.Errorf("lock platform metadata for %s@%s: %w", p.Name, p.Version, err)
				case previousHasPlatforms:
					return fmt.Errorf("refresh platform metadata for %s@%s: %w", p.Name, p.Version, err)
				default:
					fmt.Fprintf(os.Stderr, "warning: could not enrich %s lock entry from registry: %v\n", p.Name, err)
				}
			}
		} else if previousHasPlatforms {
			return fmt.Errorf("refresh platform metadata for %s@%s: no project-local registry config available", p.Name, p.Version)
		}

		// Preserve existing top-level checksums only for legacy entries. Platform
		// metadata must be refreshed from the registry instead of copied forward.
		if len(entry.Platforms) == 0 && previous != nil {
			if len(previous.Platforms) == 0 {
				entry.SHA256 = previous.SHA256
			}
		}
		newLF.Plugins[p.Name] = entry
	}

	if err := config.SaveWfctlLockfile(lockPath, newLF); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	fmt.Printf("Lockfile written to %s\n", lockPath)
	return nil
}

func loadPluginLockRegistryConfig(manifestPath, lockPath string) (*RegistryConfig, error) {
	seen := make(map[string]bool)
	for _, basePath := range []string{manifestPath, lockPath} {
		dir := filepath.Dir(basePath)
		if dir == "" {
			dir = "."
		}
		cfgPath := filepath.Join(dir, ".wfctl.yaml")
		if seen[cfgPath] {
			continue
		}
		seen[cfgPath] = true

		cfg, ok, err := loadRegistryConfigFile(cfgPath)
		if err != nil {
			return nil, err
		}
		if ok {
			return cfg, nil
		}
	}
	return nil, nil
}

var errInvalidRegistrySHA256 = errors.New("invalid sha256")

func lockPlatformsFromRegistry(registries *MultiRegistry, pluginName, version string) (map[string]config.WfctlLockPlatform, error) {
	manifest, _, err := registries.FetchManifest(pluginName)
	if err != nil {
		return nil, err
	}
	if version != "" && !samePluginVersion(version, manifest.Version) {
		return nil, fmt.Errorf("registry manifest version %q does not match requested version %q", manifest.Version, version)
	}

	platforms := make(map[string]config.WfctlLockPlatform, len(manifest.Downloads))
	for i, dl := range manifest.Downloads {
		if dl.OS == "" || dl.Arch == "" || dl.URL == "" {
			continue
		}
		if !sha256Regex.MatchString(dl.SHA256) {
			return nil, fmt.Errorf("%w for %s download %d (%s/%s): must be a 64-character hex string", errInvalidRegistrySHA256, pluginName, i, dl.OS, dl.Arch)
		}
		key := dl.OS + "-" + dl.Arch
		platforms[key] = config.WfctlLockPlatform{URL: dl.URL, SHA256: dl.SHA256}
	}
	if len(platforms) == 0 {
		return nil, fmt.Errorf("no usable platform downloads for %s@%s", pluginName, version)
	}
	return platforms, nil
}

func samePluginVersion(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

// runPluginLockLegacy is the pre-v0.19.0 behavior: read from workflow.yaml requires.plugins[].
func runPluginLockLegacy(cfgPath, lockPath string) error {
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	lf, _ := loadPluginLockfile(lockPath)
	if lf == nil {
		lf = &PluginLockfile{}
	}
	if lf.Plugins == nil {
		lf.Plugins = make(map[string]PluginLockEntry)
	}

	if cfg.Requires != nil {
		for _, req := range cfg.Requires.Plugins {
			if _, exists := lf.Plugins[req.Name]; !exists {
				lf.Plugins[req.Name] = PluginLockEntry{Version: req.Version}
			}
		}
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	fmt.Printf("Lockfile written to %s\n", lockPath)
	return nil
}
