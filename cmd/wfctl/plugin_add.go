package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

func runPluginAdd(args []string) error {
	fs := flag.NewFlagSet("plugin add", flag.ContinueOnError)
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to lockfile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: wfctl plugin add <name>[@version]")
	}
	spec := fs.Args()[0]
	name, version, _ := strings.Cut(spec, "@")
	if name == "" {
		return fmt.Errorf("invalid plugin spec %q: name required", spec)
	}

	m, err := loadOrInitManifest(*manifestPath)
	if err != nil {
		return err
	}

	// Check for duplicate.
	for _, p := range m.Plugins {
		if p.Name == name {
			return fmt.Errorf("plugin %q already in manifest; use wfctl plugin update to change version", name)
		}
	}

	m.Plugins = append(m.Plugins, config.WfctlPluginEntry{
		Name:    name,
		Version: version,
	})

	if err := config.SaveWfctlManifest(*manifestPath, m); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	if version != "" {
		fmt.Printf("Added %s@%s to wfctl.yaml\n", name, version)
	} else {
		fmt.Printf("Added %s to wfctl.yaml\n", name)
	}

	// Re-lock to refresh lockfile.
	return runPluginLockFromManifest(*manifestPath, *lockPath)
}

// loadOrInitManifest loads an existing wfctl.yaml or returns an empty manifest.
// Only swallows file-not-found; parse errors and permission errors are returned.
func loadOrInitManifest(path string) (*config.WfctlManifest, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &config.WfctlManifest{Version: 1}, nil
	}
	m, err := config.LoadWfctlManifest(path)
	if err != nil {
		return nil, fmt.Errorf("load manifest %s: %w", path, err)
	}
	return m, nil
}
