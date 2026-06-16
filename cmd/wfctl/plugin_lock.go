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
	compatMode := fs.String("compat-mode", "", "Compatibility mode for registry lock resolution: enforce or warn")
	engineVersion := fs.String("engine-version", "", "Workflow engine version for compatibility resolution")
	forceCompat := fs.Bool("force", false, "Permit known-failing compatibility evidence in the lockfile")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Prefer wfctl.yaml manifest if it exists.
	if _, err := os.Stat(*manifestPath); err == nil {
		return runPluginLockFromManifestWithOptions(*manifestPath, *lockPath, pluginLockCompatibilityOptions{
			CompatMode:    *compatMode,
			EngineVersion: *engineVersion,
			Force:         *forceCompat,
		})
	}

	// Fall back to legacy workflow.yaml requires.plugins[].
	return runPluginLockLegacy(*cfgPath, *lockPath)
}

// runPluginLockFromManifest regenerates .wfctl-lock.yaml from a wfctl.yaml manifest.
// Existing platform data must be refreshed from a project-local registry so the
// lockfile records portable archive checksums instead of host-specific binary hashes.
func runPluginLockFromManifest(manifestPath, lockPath string) error {
	return runPluginLockFromManifestWithOptions(manifestPath, lockPath, pluginLockCompatibilityOptions{})
}

type pluginLockCompatibilityOptions struct {
	CompatMode    string
	EngineVersion string
	Force         bool
}

func runPluginLockFromManifestWithOptions(manifestPath, lockPath string, compatOpts pluginLockCompatibilityOptions) error {
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	// Load existing lockfile so unchanged versions with platform metadata can be
	// forced through registry refresh instead of carrying stale archive entries.
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
			if platforms, resolvedVersion, err := lockPlatformsFromRegistry(registries, registryConfig, p.Name, p.Version, compatOpts); err == nil {
				entry.Platforms = platforms
				if resolvedVersion != "" {
					entry.Version = resolvedVersion
				}
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

		newLF.Plugins[p.Name] = entry
	}

	if err := config.PopulateWfctlLockfileProvenance(m, newLF); err != nil {
		return fmt.Errorf("calculate lockfile provenance: %w", err)
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

func lockPlatformsFromRegistry(registries *MultiRegistry, registryConfig *RegistryConfig, pluginName, version string, compatOpts pluginLockCompatibilityOptions) (map[string]config.WfctlLockPlatform, string, error) {
	manifest, index, sourceName, err := registries.FetchManifestAndVersionIndex(pluginName)
	if err != nil {
		return nil, "", err
	}
	resolvedCompatMode, err := resolvePluginCompatMode(compatOpts.CompatMode, registryConfig)
	if err != nil {
		return nil, "", err
	}
	decision, err := ResolvePluginCompatibility(index, manifest, PluginCompatResolverOptions{
		RequestedVersion: version,
		EngineVersion:    compatOpts.EngineVersion,
		CompatMode:       resolvedCompatMode,
		Force:            compatOpts.Force,
		ForceReason:      PluginCompatForceLock,
		Trust:            registryTrustMode(registryConfig, sourceName),
	})
	if err != nil {
		return nil, "", err
	}
	if decision.Warning != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", decision.Warning)
	}
	if decision.Forced {
		fmt.Fprintf(os.Stderr, "warning: forcing compatibility decision (%s)\n", decision.Reason)
	}
	if version != "" && decision.Version != "" && !compatIndexHasVersion(index, decision.Version) && !samePluginVersion(version, manifest.Version) {
		return nil, "", fmt.Errorf("registry manifest version %q does not match requested version %q", manifest.Version, version)
	}
	if decision.Version != "" && decision.Version != manifest.Version {
		manifest = manifestForCompatibilityVersion(manifest, index, decision.Version)
	}

	platforms := make(map[string]config.WfctlLockPlatform, len(manifest.Downloads))
	for i, dl := range manifest.Downloads {
		if dl.OS == "" || dl.Arch == "" || dl.URL == "" {
			continue
		}
		if !sha256Regex.MatchString(dl.SHA256) {
			return nil, "", fmt.Errorf("%w for %s download %d (%s/%s): must be a 64-character hex string", errInvalidRegistrySHA256, pluginName, i, dl.OS, dl.Arch)
		}
		key := dl.OS + "-" + dl.Arch
		platforms[key] = config.WfctlLockPlatform{URL: dl.URL, SHA256: dl.SHA256}
	}
	if len(platforms) == 0 {
		return nil, "", fmt.Errorf("no usable platform downloads for %s@%s", pluginName, version)
	}
	if decision.Evidence != nil {
		key := decision.Evidence.OS + "-" + decision.Evidence.Arch
		if p, ok := platforms[key]; ok {
			p.Compatibility = lockCompatibilityFromDecision(decision)
			platforms[key] = p
		}
	} else if decision.Forced || decision.Warning != "" {
		key := currentPlatformKey()
		if p, ok := platforms[key]; ok {
			p.Compatibility = lockCompatibilityFromDecision(decision)
			platforms[key] = p
		}
	}
	return platforms, manifest.Version, nil
}

func compatIndexHasVersion(index *PluginVersionIndex, version string) bool {
	if index == nil {
		return false
	}
	for _, rec := range index.Versions {
		if samePluginVersion(rec.Version, version) {
			return true
		}
	}
	return false
}

func lockCompatibilityFromDecision(decision PluginCompatDecision) *config.WfctlLockCompatibility {
	c := &config.WfctlLockCompatibility{
		Forced: decision.Forced,
		Reason: decision.Reason,
	}
	if decision.Evidence != nil {
		c.Mode = decision.Evidence.Mode
		c.Status = decision.Evidence.Status
		c.EngineVersion = decision.Evidence.EngineVersion
		c.EvidenceDigest = decision.Evidence.EvidenceDigest
	}
	if c.Mode == "" && c.Status == "" && c.EngineVersion == "" && c.EvidenceDigest == "" && !c.Forced && c.Reason == "" {
		return nil
	}
	return c
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
