package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// infraOutputEntry holds the outputs for a single state resource.
type infraOutputEntry struct {
	module  string
	outputs map[string]any
}

// runInfraOutputs implements `wfctl infra outputs`.
// It reads all resource outputs stored in the configured state backend and
// prints them in the requested format. The command is read-only — it never
// writes to the state backend or the secrets provider.
func runInfraOutputs(args []string) error {
	fs := flag.NewFlagSet("infra outputs", flag.ContinueOnError)
	var configFlag, envFlag, formatFlag, moduleFlag string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	fs.StringVar(&envFlag, "env", "", "Environment name (applies per-env state backend config)")
	fs.StringVar(&envFlag, "e", "", "Environment name (short for --env)")
	fs.StringVar(&formatFlag, "format", "yaml", "Output format: yaml (default), json, env")
	fs.StringVar(&formatFlag, "f", "yaml", "Output format (short for --format)")
	fs.StringVar(&moduleFlag, "module", "", "Restrict output to a single module name")
	fs.StringVar(&moduleFlag, "m", "", "Module name (short for --module)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile, err := resolveInfraConfig(fs, configFlag)
	if err != nil {
		return err
	}

	states := loadCurrentState(cfgFile, envFlag)

	// Collect modules with non-empty outputs, in stable alphabetical order.
	var entries []infraOutputEntry
	for _, s := range states {
		if len(s.Outputs) == 0 {
			continue
		}
		if moduleFlag != "" && s.Name != moduleFlag {
			continue
		}
		entries = append(entries, infraOutputEntry{module: s.Name, outputs: s.Outputs})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].module < entries[j].module })

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "No outputs found in state.")
		return nil
	}

	switch formatFlag {
	case "yaml":
		return infraOutputsYAML(entries)
	case "json":
		return infraOutputsJSON(entries)
	case "env":
		return infraOutputsEnv(entries)
	default:
		return fmt.Errorf("unknown format %q (supported: yaml, json, env)", formatFlag)
	}
}

// infraOutputsYAML prints outputs as a YAML document keyed by module name.
//
//	bmw-staging-db:
//	  host: db.example.com
//	  port: 5432
//	  uri: postgresql://...
func infraOutputsYAML(entries []infraOutputEntry) error {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, e := range entries {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: e.module},
			outputsToYAMLNode(e.outputs),
		)
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("yaml encode: %w", err)
	}
	return enc.Close()
}

// outputsToYAMLNode converts a map[string]any to a yaml.MappingNode with
// keys sorted alphabetically for stable output.
func outputsToYAMLNode(m map[string]any) *yaml.Node {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range keys {
		valNode := &yaml.Node{}
		if err := valNode.Encode(m[k]); err != nil {
			valNode = &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprint(m[k])}
		}
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			valNode,
		)
	}
	return node
}

// infraOutputsJSON prints outputs as a JSON object keyed by module name.
//
//	{
//	  "bmw-staging-db": { "host": "db.example.com", "uri": "postgresql://..." }
//	}
func infraOutputsJSON(entries []infraOutputEntry) error {
	combined := make(map[string]map[string]any, len(entries))
	for _, e := range entries {
		combined[e.module] = e.outputs
	}
	data, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// infraOutputsEnv prints outputs as shell-ready KEY=value lines, with the
// key formed from the module name and output field name joined by underscores
// and uppercased:
//
//	BMW_STAGING_DB_HOST=db.example.com
//	BMW_STAGING_DB_URI=postgresql://...
func infraOutputsEnv(entries []infraOutputEntry) error {
	for _, e := range entries {
		prefix := infraEnvVarName(e.module)
		keys := make([]string, 0, len(e.outputs))
		for k := range e.outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s_%s=%v\n", prefix, infraEnvVarName(k), e.outputs[k])
		}
	}
	return nil
}

// infraEnvVarName converts a module or field name to an environment-variable-
// safe uppercase string, replacing hyphens and dots with underscores.
func infraEnvVarName(s string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(s))
}
