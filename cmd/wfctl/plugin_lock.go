package main

import (
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
	"os"
)

// runPluginLock regenerates the plugin lockfile from requires.plugins[] in the
// workflow config. Pins each declared plugin at its declared version (or "latest"
// if no version is specified).
func runPluginLock(args []string) error {
	fs := flag.NewFlagSet("plugin lock", flag.ContinueOnError)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	lockPath := fs.String("lock-file", wfctlYAMLPath, "Path to lockfile to write")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	lf, _ := loadPluginLockfile(*lockPath)
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
	if err := os.WriteFile(*lockPath, data, 0600); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	fmt.Printf("Lockfile written to %s\n", *lockPath)
	return nil
}
