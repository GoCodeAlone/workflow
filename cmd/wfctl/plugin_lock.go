package main

import (
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
		// Preserve existing sha256/platforms only when both version AND source match.
		// A source change means the binary origin changed, so cached checksums are stale.
		if existing != nil {
			if prev, ok := existing.Plugins[p.Name]; ok &&
				prev.Version == p.Version &&
				prev.Source == p.Source {
				entry.SHA256 = prev.SHA256
				entry.Platforms = prev.Platforms
			}
		}
		if len(entry.Platforms) == 0 && registries != nil {
			if platforms, err := lockPlatformsFromRegistry(registries, p.Name, p.Version); err == nil {
				entry.Platforms = platforms
			} else {
				fmt.Fprintf(os.Stderr, "warning: could not enrich %s lock entry from registry: %v\n", p.Name, err)
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

func lockPlatformsFromRegistry(registries *MultiRegistry, pluginName, version string) (map[string]config.WfctlLockPlatform, error) {
	manifest, _, err := registries.FetchManifest(pluginName)
	if err != nil {
		return nil, err
	}
	if version != "" && !samePluginVersion(version, manifest.Version) {
		return nil, fmt.Errorf("registry manifest version %q does not match requested version %q", manifest.Version, version)
	}

	platforms := make(map[string]config.WfctlLockPlatform, len(manifest.Downloads))
	for _, dl := range manifest.Downloads {
		if dl.OS == "" || dl.Arch == "" || dl.URL == "" {
			continue
		}
		key := dl.OS + "-" + dl.Arch
		// Registry download SHA values are archive checksums. The lockfile
		// platform SHA is currently verified against the installed plugin binary,
		// so copying the archive checksum here would make lockfile installs fail.
		platforms[key] = config.WfctlLockPlatform{URL: dl.URL}
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
