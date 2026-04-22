package main

import (
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// corePortPaths maps well-known module types to the config key that holds their
// listen port and whether the port is public-facing.
var corePortPaths = map[string]struct {
	key    string
	public bool
	proto  string
}{
	"http.server":           {key: "port", public: true, proto: "http"},
	"http.gateway":          {key: "port", public: true, proto: "http"},
	"websocket.server":      {key: "port", public: true, proto: "ws"},
	"observability.metrics": {key: "port", public: false, proto: "http"},
	"grpc.server":           {key: "port", public: false, proto: "grpc"},
	"database.postgres":     {key: "port", public: false, proto: "tcp"},
	"database.workflow":     {key: "port", public: false, proto: "tcp"},
	"messaging.nats":        {key: "port", public: false, proto: "tcp"},
}

// IntrospectPorts aggregates port declarations from:
//  1. Core built-in module types (http.server, observability.metrics, grpc.server, …)
//  2. Installed plugins that declare portPaths in their capabilities
//  3. Explicit ci.build.containers[].expose_ports overrides
//
// When any container declares expose_ports, those entries replace auto-detected ports.
func IntrospectPorts(cfg *config.WorkflowConfig, pluginsDir string) []interfaces.PortSpec {
	if cfg == nil {
		return nil
	}

	// Collect explicit expose_ports overrides from ci.build.containers.
	var explicit []int
	if cfg.CI != nil && cfg.CI.Build != nil {
		for _, ctr := range cfg.CI.Build.Containers {
			explicit = append(explicit, ctr.ExposePorts...)
		}
	}
	if len(explicit) > 0 {
		return exposePortsToSpecs(explicit)
	}

	// Load plugin manifests for portPaths extension.
	pluginPaths := loadPluginPortPaths(pluginsDir)

	// Collect all module lists (top-level + per-service).
	allModules := collectAllModules(cfg)

	seen := make(map[int]bool)
	var specs []interfaces.PortSpec

	for _, mod := range allModules {
		portSpecs := resolveModulePorts(mod, pluginPaths)
		for _, ps := range portSpecs {
			if !seen[ps.Port] {
				seen[ps.Port] = true
				specs = append(specs, ps)
			}
		}
	}

	return specs
}

// resolveModulePorts extracts PortSpecs from a single module config.
func resolveModulePorts(mod config.ModuleConfig, pluginPaths map[string][]string) []interfaces.PortSpec {
	var specs []interfaces.PortSpec

	// Core built-ins.
	if meta, ok := corePortPaths[mod.Type]; ok {
		if port := readIntFromConfig(mod.Config, meta.key); port > 0 {
			specs = append(specs, interfaces.PortSpec{
				Name:     fmt.Sprintf("%s-%d", sanitizeName(mod.Name), port),
				Port:     port,
				Protocol: meta.proto,
				Public:   meta.public,
			})
		}
	}

	// Plugin-declared portPaths.
	for prefix, paths := range pluginPaths {
		if !strings.HasPrefix(mod.Type, prefix+".") {
			continue
		}
		for _, path := range paths {
			port := readPathFromConfig(mod.Config, path)
			if port > 0 && !portInSpecs(specs, port) {
				specs = append(specs, interfaces.PortSpec{
					Name:     fmt.Sprintf("%s-%d", sanitizeName(mod.Name), port),
					Port:     port,
					Protocol: "tcp",
					Public:   false,
				})
			}
		}
	}

	return specs
}

// loadPluginPortPaths reads installed plugin manifests and returns a map of
// plugin-name-prefix → list of dot-notation config paths that hold port values.
func loadPluginPortPaths(pluginsDir string) map[string][]string {
	result := make(map[string][]string)
	if pluginsDir == "" {
		return result
	}
	manifests, err := LoadPluginManifests(pluginsDir)
	if err != nil {
		return result
	}
	for name, manifest := range manifests {
		if len(manifest.Capabilities.PortPaths) > 0 {
			result[name] = manifest.Capabilities.PortPaths
		}
	}
	return result
}

// collectAllModules gathers modules from all services + top-level.
func collectAllModules(cfg *config.WorkflowConfig) []config.ModuleConfig {
	var all []config.ModuleConfig
	all = append(all, cfg.Modules...)
	for _, svc := range cfg.Services {
		if svc != nil {
			all = append(all, svc.Modules...)
		}
	}
	return all
}

// exposePortsToSpecs converts a plain int slice into PortSpec slice.
func exposePortsToSpecs(ports []int) []interfaces.PortSpec {
	specs := make([]interfaces.PortSpec, 0, len(ports))
	for _, p := range ports {
		specs = append(specs, interfaces.PortSpec{
			Name:     fmt.Sprintf("port-%d", p),
			Port:     p,
			Protocol: "tcp",
			Public:   true,
		})
	}
	return specs
}

// readIntFromConfig reads an integer value from a flat config map by key.
func readIntFromConfig(cfg map[string]any, key string) int {
	if cfg == nil {
		return 0
	}
	v, ok := cfg[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

// readPathFromConfig reads a port from a dot-notation path like "config.api_port".
// For simplicity, only single-level "config.<key>" paths are supported.
func readPathFromConfig(cfg map[string]any, path string) int {
	// Strip leading "config." prefix which is common in path declarations.
	key := strings.TrimPrefix(path, "config.")
	return readIntFromConfig(cfg, key)
}

func sanitizeName(s string) string {
	return strings.NewReplacer(" ", "-", ".", "-", "_", "-").Replace(s)
}

func portInSpecs(specs []interfaces.PortSpec, port int) bool {
	for _, s := range specs {
		if s.Port == port {
			return true
		}
	}
	return false
}
