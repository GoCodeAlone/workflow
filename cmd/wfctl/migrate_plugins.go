package main

import (
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
)

// runMigratePlugins migrates requires.plugins[] from app.yaml into wfctl.yaml
// (manifest) and .wfctl-lock.yaml (lockfile).
// It is safe to re-run (idempotent): plugins already present in wfctl.yaml are skipped.
// Stripping the inline fields from app.yaml after migration is a manual step in v0.19.0;
// automated stripping (--auto) is deferred to v0.20.0.
func runMigratePlugins(args []string) error {
	fs := flag.NewFlagSet("migrate plugins", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow app config")
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest to create/update")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to .wfctl-lock.yaml to create/update")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl migrate plugins [options]

Migrate requires.plugins[] from app.yaml into wfctl.yaml + .wfctl-lock.yaml.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", *cfgPath, err)
	}

	if cfg.Requires == nil || len(cfg.Requires.Plugins) == 0 {
		fmt.Println("No requires.plugins[] found in config — nothing to migrate.")
		return nil
	}

	// Load or initialize manifest.
	m, err := loadOrInitManifest(*manifestPath)
	if err != nil {
		return err
	}

	// Build set of normalized names already in manifest to avoid duplicates.
	// Normalize both sides so "foo" and "workflow-plugin-foo" are treated as the same plugin.
	existing := make(map[string]bool, len(m.Plugins))
	for _, p := range m.Plugins {
		existing[normalizePluginName(p.Name)] = true
	}

	added := 0
	for _, req := range cfg.Requires.Plugins {
		if existing[normalizePluginName(req.Name)] {
			continue
		}
		entry := config.WfctlPluginEntry{
			Name:    req.Name,
			Version: req.Version,
			Source:  req.Source,
		}
		if req.Auth != nil {
			entry.Auth = &config.WfctlPluginAuth{Env: req.Auth.Env}
		}
		m.Plugins = append(m.Plugins, entry)
		existing[normalizePluginName(req.Name)] = true
		added++
	}

	if added == 0 {
		fmt.Println("All plugins already present in manifest — nothing to add.")
	} else {
		if err := config.SaveWfctlManifest(*manifestPath, m); err != nil {
			return fmt.Errorf("save manifest: %w", err)
		}
		fmt.Printf("Migrated %d plugin(s) to %s\n", added, *manifestPath)
	}

	// Re-lock to produce/update .wfctl-lock.yaml.
	return runPluginLockFromManifest(*manifestPath, *lockPath)
}
