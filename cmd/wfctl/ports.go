package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/GoCodeAlone/workflow/config"
)

func runPorts(args []string) error {
	if len(args) < 1 {
		return portsUsage()
	}
	switch args[0] {
	case "list":
		return runPortsList(args[1:])
	default:
		return portsUsage()
	}
}

func portsUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl ports <action> [options] [config.yaml]

Inspect port usage declared in a workflow config.

Actions:
  list    Show all ports used by modules and services

Options:
  --config <file>    Config file (default: config.yaml or app.yaml)

Examples:
  wfctl ports list
  wfctl ports list --config config/app.yaml
`)
	return fmt.Errorf("missing or unknown action")
}

// portEntry represents a detected port binding.
type portEntry struct {
	Service  string
	Module   string
	Port     int
	Protocol string
	Exposure string
}

func runPortsList(args []string) error {
	fs := flag.NewFlagSet("ports list", flag.ContinueOnError)
	configFile := fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgPath, err := resolveConfigFile(*configFile, fs.Args())
	if err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	entries := collectPorts(cfg)
	if len(entries) == 0 {
		fmt.Println("No ports detected in config.")
		return nil
	}

	return printPortsTable(os.Stdout, entries)
}

func printPortsTable(w io.Writer, entries []portEntry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tMODULE\tPORT\tPROTOCOL\tEXPOSURE")
	fmt.Fprintln(tw, "-------\t------\t----\t--------\t--------")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", e.Service, e.Module, e.Port, e.Protocol, e.Exposure)
	}
	return tw.Flush()
}

// collectPorts scans the config and returns all detected port bindings.
func collectPorts(cfg *config.WorkflowConfig) []portEntry {
	var entries []portEntry

	// Scan top-level modules for well-known port config keys.
	for _, mod := range cfg.Modules {
		port, proto := extractModulePort(mod)
		if port > 0 {
			exposure := classifyExposure(mod.Type)
			entries = append(entries, portEntry{
				Service:  "(default)",
				Module:   mod.Name,
				Port:     port,
				Protocol: proto,
				Exposure: exposure,
			})
		}
	}

	// Scan services: section.
	for svcName, svc := range cfg.Services {
		if svc == nil {
			continue
		}
		// Ports declared in expose:
		for _, exp := range svc.Expose {
			proto := exp.Protocol
			if proto == "" {
				proto = "tcp"
			}
			entries = append(entries, portEntry{
				Service:  svcName,
				Module:   "(expose)",
				Port:     exp.Port,
				Protocol: proto,
				Exposure: "internal",
			})
		}
		// Ports from per-service modules.
		for _, mod := range svc.Modules {
			port, proto := extractModulePort(mod)
			if port > 0 {
				exposure := classifyExposure(mod.Type)
				entries = append(entries, portEntry{
					Service:  svcName,
					Module:   mod.Name,
					Port:     port,
					Protocol: proto,
					Exposure: exposure,
				})
			}
		}
	}

	// Scan networking.ingress for externally exposed ports.
	if cfg.Networking != nil {
		for _, ing := range cfg.Networking.Ingress {
			proto := ing.Protocol
			if proto == "" {
				proto = "http"
			}
			port := ing.ExternalPort
			if port == 0 {
				port = ing.Port
			}
			svc := ing.Service
			if svc == "" {
				svc = "(default)"
			}
			entries = append(entries, portEntry{
				Service:  svc,
				Module:   "(ingress)",
				Port:     port,
				Protocol: proto,
				Exposure: "public",
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Service != entries[j].Service {
			return entries[i].Service < entries[j].Service
		}
		return entries[i].Port < entries[j].Port
	})
	return entries
}

// knownPortKeys lists config keys that typically carry port values across module types.
var knownPortKeys = []string{"port", "address", "addr", "listenAddr", "listenPort"}

// extractModulePort attempts to read a port number from a module's config map.
func extractModulePort(mod config.ModuleConfig) (int, string) {
	if mod.Config == nil {
		return 0, ""
	}
	for _, key := range knownPortKeys {
		v, ok := mod.Config[key]
		if !ok {
			continue
		}
		switch val := v.(type) {
		case int:
			if val > 0 {
				return val, protocolForType(mod.Type)
			}
		case string:
			// Parse ":8080" style address strings.
			port := 0
			if _, err := fmt.Sscanf(val, ":%d", &port); err == nil && port > 0 {
				return port, protocolForType(mod.Type)
			}
			if _, err := fmt.Sscanf(val, "%d", &port); err == nil && port > 0 {
				return port, protocolForType(mod.Type)
			}
		}
	}
	return 0, ""
}

func protocolForType(modType string) string {
	switch modType {
	case "http.server", "http.router", "http.gateway":
		return "http"
	case "websocket.server":
		return "ws"
	case "messaging.nats", "messaging.broker":
		return "tcp"
	case "database.postgres", "database.workflow":
		return "tcp"
	default:
		return "tcp"
	}
}

func classifyExposure(modType string) string {
	switch modType {
	case "http.server", "http.gateway", "websocket.server":
		return "public"
	default:
		return "internal"
	}
}

// resolveConfigFile finds the config file from a flag value, positional args, or defaults.
func resolveConfigFile(flagVal string, posArgs []string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if len(posArgs) > 0 {
		return posArgs[0], nil
	}
	defaults := []string{"config.yaml", "app.yaml", "config/app.yaml", "config/config.yaml"}
	for _, d := range defaults {
		if fileExists(d) {
			return d, nil
		}
	}
	return "", fmt.Errorf("no config file found; use --config <file>")
}
