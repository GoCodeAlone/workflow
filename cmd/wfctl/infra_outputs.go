package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
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

	// When --env is used, module names in state are stored under their
	// env-resolved names (e.g. "bmw-database" resolves to "bmw-staging-db"
	// under --env staging). Resolve --module through the same mechanism so
	// the caller can pass the base config name and still get a match.
	resolvedModuleFilter := moduleFlag
	if moduleFlag != "" && envFlag != "" {
		if resolved := resolveModuleNameForEnv(cfgFile, moduleFlag, envFlag); resolved != "" {
			resolvedModuleFilter = resolved
		}
	}

	// Collect modules with non-empty outputs, in stable alphabetical order.
	var entries []infraOutputEntry
	for i := range states {
		s := &states[i]
		if len(s.Outputs) == 0 {
			continue
		}
		if resolvedModuleFilter != "" && s.Name != resolvedModuleFilter {
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

// resolveModuleNameForEnv returns the env-resolved name for the module whose
// base config name equals baseName. If the module has no name override for
// envName, the baseName is returned unchanged. On any load/parse error the
// baseName is returned so callers degrade gracefully.
func resolveModuleNameForEnv(cfgFile, baseName, envName string) string {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return baseName
	}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Name != baseName {
			continue
		}
		rm, ok := m.ResolveForEnv(envName)
		if !ok {
			return baseName // module disabled for this env
		}
		return rm.Name
	}
	return baseName
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
// and uppercased. Values are single-quoted so the output can be safely
// eval'd by a POSIX shell:
//
//	BMW_STAGING_DB_HOST='db.example.com'
//	BMW_STAGING_DB_URI='postgresql://...'
//
// Non-scalar values (maps/slices) are serialised as compact JSON before
// quoting.
func infraOutputsEnv(entries []infraOutputEntry) error {
	for _, e := range entries {
		prefix := infraEnvVarName(e.module)
		keys := make([]string, 0, len(e.outputs))
		for k := range e.outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s_%s=%s\n", prefix, infraEnvVarName(k), shellQuote(e.outputs[k]))
		}
	}
	return nil
}

// shellQuote returns a single-quoted POSIX shell literal for any value.
// Strings are used as-is. Maps and slices of any concrete type are
// JSON-serialized (via reflection) so callers get valid JSON rather than
// Go's %v formatting. All other scalars are formatted with %v. Single-quote
// characters inside the value are escaped using the standard POSIX sequence:
// close the literal, emit a backslash-quoted single-quote, then reopen the
// literal (the four-character sequence apostrophe backslash apostrophe
// apostrophe).
func shellQuote(v any) string {
	var s string
	switch tv := v.(type) {
	case string:
		s = tv
	default:
		rv := reflect.ValueOf(v)
		if rv.IsValid() && (rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice) {
			b, err := json.Marshal(tv)
			if err != nil {
				s = fmt.Sprint(tv)
			} else {
				s = string(b)
			}
		} else {
			s = fmt.Sprint(tv)
		}
	}
	// Escape single-quotes: end the current literal, emit an escaped ', reopen.
	escaped := strings.ReplaceAll(s, "'", `'\''`)
	return "'" + escaped + "'"
}

// infraEnvVarNameInvalidChars matches any character that is NOT alphanumeric
// or underscore — these are replaced with '_' to produce a valid POSIX shell
// environment variable name.
var infraEnvVarNameInvalidChars = regexp.MustCompile(`[^A-Z0-9_]`)

// infraEnvVarName converts a module or field name to an environment-variable-
// safe uppercase identifier. All characters that are not ASCII letters, digits,
// or underscores (including hyphens, dots, slashes, colons, etc.) are replaced
// with underscores. A leading digit is prefixed with an underscore because POSIX
// environment variable names must begin with a letter or underscore.
func infraEnvVarName(s string) string {
	upper := strings.ToUpper(s)
	sanitized := infraEnvVarNameInvalidChars.ReplaceAllString(upper, "_")
	if len(sanitized) > 0 && sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "_" + sanitized
	}
	return sanitized
}
