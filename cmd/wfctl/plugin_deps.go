package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// installFromWorkflowConfig reads requires.plugins[] from a workflow config file
// and installs each plugin that is not already present on disk.
func installFromWorkflowConfig(workflowCfgPath, pluginDir, registryCfgPath string) error {
	cfg, err := config.LoadFromFile(workflowCfgPath)
	if err != nil {
		return fmt.Errorf("load workflow config: %w", err)
	}

	if cfg.Requires == nil || len(cfg.Requires.Plugins) == 0 {
		fmt.Println("No requires.plugins[] in config — nothing to install.")
		return nil
	}

	var failed []string
	for _, req := range cfg.Requires.Plugins {
		// Normalize the name before checking the install directory so the skip check
		// matches the actual install location. runPluginInstall normalizes names via
		// normalizePluginName (stripping "workflow-plugin-" prefix), so
		// "workflow-plugin-auth" is installed at <pluginDir>/auth, not
		// <pluginDir>/workflow-plugin-auth.
		normalizedName := normalizePluginName(req.Name)
		installDir := filepath.Join(pluginDir, normalizedName)
		if ver := readInstalledVersion(installDir); ver != "" && ver != "unknown" {
			fmt.Fprintf(os.Stderr, "%s v%s already installed, skipping.\n", req.Name, ver)
			continue
		}

		// Apply private repo auth if declared.
		var authCleanup func()
		if req.Auth != nil && req.Auth.Env != "" {
			domain := extractDomain(req.Source)
			if domain == "" {
				domain = "github.com"
			}
			cleanup, authErr := applyPrivateAuth(req.Auth.Env, domain)
			if authErr != nil {
				fmt.Fprintf(os.Stderr, "auth error for %s: %v\n", req.Name, authErr)
				failed = append(failed, req.Name)
				continue
			}
			authCleanup = cleanup
		}

		nameArg := req.Name
		if req.Version != "" {
			nameArg = req.Name + "@" + req.Version
		}

		installArgs := []string{"--plugin-dir", pluginDir}
		if registryCfgPath != "" {
			installArgs = append(installArgs, "--config", registryCfgPath)
		}
		installArgs = append(installArgs, nameArg)

		fmt.Fprintf(os.Stderr, "Installing %s...\n", nameArg)
		installErr := runPluginInstall(installArgs)
		if authCleanup != nil {
			authCleanup()
		}
		if installErr != nil {
			fmt.Fprintf(os.Stderr, "error installing %s: %v\n", req.Name, installErr)
			failed = append(failed, req.Name)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to install: %s", strings.Join(failed, ", "))
	}
	return nil
}

// runPluginDeps lists dependencies for a plugin without installing them.
func runPluginDeps(args []string) error {
	fs := flag.NewFlagSet("plugin deps", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Registry config file path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin deps [options] <name>[@<version>]\n\nList dependencies for a plugin.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	nameArg := fs.Arg(0)
	rawName, _ := parseNameVersion(nameArg)
	pluginName := normalizePluginName(rawName)

	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return fmt.Errorf("load registry config: %w", err)
	}
	mr := NewMultiRegistry(cfg)

	manifest, _, err := mr.FetchManifest(pluginName)
	if err != nil {
		return fmt.Errorf("fetch manifest for %q: %w", pluginName, err)
	}

	if len(manifest.Dependencies) == 0 {
		fmt.Printf("%s v%s has no dependencies.\n", pluginName, manifest.Version)
		return nil
	}

	fmt.Printf("%s v%s dependencies:\n", pluginName, manifest.Version)
	printDepTree(mr, manifest, "", make(map[string]bool))
	return nil
}

// printDepTree prints a dependency tree recursively with indentation.
func printDepTree(mr *MultiRegistry, manifest *RegistryManifest, prefix string, visited map[string]bool) {
	for i, dep := range manifest.Dependencies {
		isLast := i == len(manifest.Dependencies)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if isLast {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		versionConstraint := dep.Name
		if dep.MinVersion != "" {
			versionConstraint += " >=" + dep.MinVersion
		}
		if dep.MaxVersion != "" {
			versionConstraint += " <=" + dep.MaxVersion
		}

		if visited[dep.Name] {
			fmt.Printf("%s%s%s (already shown)\n", prefix, connector, versionConstraint)
			continue
		}
		fmt.Printf("%s%s%s\n", prefix, connector, versionConstraint)

		visited[dep.Name] = true
		depManifest, _, err := mr.FetchManifest(dep.Name)
		if err == nil && len(depManifest.Dependencies) > 0 {
			printDepTree(mr, depManifest, childPrefix, visited)
		}
	}
}

// resolveDependencies recursively installs all dependencies declared in manifest
// before the parent plugin is installed. It detects circular dependencies via
// chain and version conflicts via resolved.
//
// Parameters:
//   - pluginName: name of the plugin whose deps are being resolved
//   - manifest: manifest of pluginName
//   - pluginDir: root plugin install directory
//   - cfgPath: registry config path (may be empty)
//   - chain: current install chain for circular dep detection (plugin names, in order)
//   - resolved: map[pluginName]installedVersion, accumulates what was resolved this run
func resolveDependencies(
	pluginName string,
	manifest *RegistryManifest,
	pluginDir string,
	cfgPath string,
	chain []string,
	resolved map[string]string,
) error {
	if len(manifest.Dependencies) == 0 {
		return nil
	}

	cfg, err := LoadRegistryConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load registry config: %w", err)
	}
	mr := NewMultiRegistry(cfg)

	for _, dep := range manifest.Dependencies {
		// Circular dependency check.
		for _, ancestor := range chain {
			if ancestor == dep.Name {
				cycle := strings.Join(append(chain, dep.Name), " → ")
				return fmt.Errorf("circular dependency detected: %s", cycle)
			}
		}

		// Check if already installed on disk at a compatible version (avoids registry fetch).
		depInstallDir := filepath.Join(pluginDir, dep.Name)
		if installedVer := readInstalledVersion(depInstallDir); installedVer != "" && installedVer != "unknown" {
			if err := checkVersionConstraints(dep, installedVer); err == nil {
				resolved[dep.Name] = installedVer
				fmt.Fprintf(os.Stderr, "Dependency %s v%s already installed, skipping.\n", dep.Name, installedVer)
				continue
			}
			// Installed but incompatible version — will reinstall.
			fmt.Fprintf(os.Stderr, "Dependency %s installed at %s is incompatible, reinstalling...\n", dep.Name, installedVer)
		}

		// Fetch manifest from registry.
		depManifest, _, fetchErr := mr.FetchManifest(dep.Name)
		if fetchErr != nil {
			return fmt.Errorf("resolve dependency %q of %q: %w", dep.Name, pluginName, fetchErr)
		}

		// Version constraint check against registry manifest.
		if err := checkVersionConstraints(dep, depManifest.Version); err != nil {
			return fmt.Errorf("dependency %q of %q: %w", dep.Name, pluginName, err)
		}

		// Conflict check: if resolved earlier in this run at a different version.
		if prevVer, seen := resolved[dep.Name]; seen {
			if prevVer != depManifest.Version {
				return fmt.Errorf("version conflict: %q required by %q at %s, but already resolved at %s",
					dep.Name, pluginName, depManifest.Version, prevVer)
			}
			continue
		}

		// Recursively resolve this dependency's own dependencies first.
		childChain := append(chain, pluginName) //nolint:gocritic // intentional slice extension
		if err := resolveDependencies(dep.Name, depManifest, pluginDir, cfgPath, childChain, resolved); err != nil {
			return err
		}

		// Install the dependency.
		fmt.Fprintf(os.Stderr, "Installing %s v%s (dependency of %s)...\n", dep.Name, depManifest.Version, pluginName)
		if err := installPluginFromManifest(pluginDir, dep.Name, depManifest); err != nil {
			return fmt.Errorf("install dependency %q of %q: %w", dep.Name, pluginName, err)
		}
		resolved[dep.Name] = depManifest.Version
	}
	return nil
}

// checkVersionConstraints verifies that version satisfies dep's min/maxVersion bounds.
// version must be in "MAJOR.MINOR.PATCH" semver form. Uses compareSemver from registry_validate.go.
func checkVersionConstraints(dep PluginDependency, version string) error {
	if dep.MinVersion != "" {
		if compareSemver(version, dep.MinVersion) < 0 {
			return fmt.Errorf("version %s is below minimum required %s", version, dep.MinVersion)
		}
	}
	if dep.MaxVersion != "" {
		if compareSemver(version, dep.MaxVersion) > 0 {
			return fmt.Errorf("version %s exceeds maximum allowed %s", version, dep.MaxVersion)
		}
	}
	return nil
}
