package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func runInfraState(args []string) error {
	if len(args) < 1 {
		return infraStateUsage()
	}
	switch args[0] {
	case "list":
		return runInfraStateList(args[1:])
	case "export":
		return runInfraStateExport(args[1:])
	case "import":
		return runInfraStateImport(args[1:])
	default:
		return infraStateUsage()
	}
}

func infraStateUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl infra state <subcommand> [options]

Manage IaC state records.

Subcommands:
  list                           List all tracked resources
  export --format tfstate        Export state as Terraform state JSON
  import --from tfstate <file>   Import Terraform state
  import --from pulumi <file>    Import Pulumi checkpoint

Options:
  --config <file>    Config file (default: infra.yaml or config/infra.yaml)
  --output <file>    Output file (export only)
`)
	return fmt.Errorf("missing or unknown subcommand")
}

func runInfraStateList(args []string) error {
	fs := flag.NewFlagSet("infra state list", flag.ContinueOnError)
	var configFlag string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs, configFlag)
		if err != nil {
			// No config found — list is empty, not an error.
			fmt.Println("No infrastructure config found. No state to list.")
			return nil //nolint:nilerr // intentionally swallowing error - no config means nothing to list
		}
	}

	states, err := loadCurrentState(cfgFile, "")
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	if len(states) == 0 {
		fmt.Println("No resources tracked in state.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Name\tType\tProvider\tProviderID")
	fmt.Fprintln(tw, "----\t----\t--------\t----------")
	for i := range states {
		s := &states[i]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Type, s.Provider, s.ProviderID)
	}
	tw.Flush()
	fmt.Printf("\n%d resource(s) tracked.\n", len(states))
	return nil
}

func runInfraStateExport(args []string) error {
	fs := flag.NewFlagSet("infra state export", flag.ContinueOnError)
	var configFlag, formatVal, outputFlag string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	fs.StringVar(&formatVal, "format", "tfstate", "Export format: tfstate")
	fs.StringVar(&formatVal, "f", "tfstate", "Export format (short for --format)")
	fs.StringVar(&outputFlag, "output", "", "Output file (default: stdout)")
	fs.StringVar(&outputFlag, "o", "", "Output file (short for --output)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgFile := configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs, configFlag)
		if err != nil {
			return err
		}
	}

	states, err := loadCurrentState(cfgFile, "")
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	var data []byte
	switch formatVal {
	case "tfstate":
		data, err = exportAsTFState(states)
	default:
		return fmt.Errorf("unknown export format %q (supported: tfstate)", formatVal)
	}
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	if outputFlag == "" {
		fmt.Println(string(data))
		return nil
	}
	if err := os.WriteFile(outputFlag, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outputFlag, err)
	}
	fmt.Printf("State exported to %s (%s format)\n", outputFlag, formatVal)
	return nil
}

func runInfraStateImport(args []string) error {
	fs := flag.NewFlagSet("infra state import", flag.ContinueOnError)
	var configFlag, fromVal string
	fs.StringVar(&configFlag, "config", "", "Config file")
	fs.StringVar(&configFlag, "c", "", "Config file (short for --config)")
	fs.StringVar(&fromVal, "from", "", "Source format: tfstate or pulumi")
	from := &fromVal
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *from == "" {
		return fmt.Errorf("--from is required (tfstate or pulumi)")
	}
	if len(fs.Args()) == 0 {
		return fmt.Errorf("state import requires a file argument")
	}
	srcFile := fs.Args()[0]

	cfgFile := configFlag
	if cfgFile == "" {
		var err error
		cfgFile, err = resolveInfraConfig(fs, configFlag)
		if err != nil {
			return err
		}
	}

	iacMods, _, _, err := discoverInfraModules(cfgFile)
	if err != nil {
		return err
	}
	var stateDir string
	for _, m := range iacMods {
		if backend, _ := m.Config["backend"].(string); backend == "filesystem" || backend == "" {
			stateDir, _ = m.Config["directory"].(string)
			if stateDir == "" {
				stateDir = "/var/lib/workflow/iac-state"
			}
			break
		}
	}
	if stateDir == "" {
		return fmt.Errorf("no filesystem iac.state module found in %s; state import only supports the filesystem backend", cfgFile)
	}

	switch *from {
	case "tfstate":
		return importFromTFState(srcFile, stateDir)
	case "pulumi":
		return importFromPulumi(srcFile, stateDir)
	default:
		return fmt.Errorf("unknown source format %q (supported: tfstate, pulumi)", *from)
	}
}

// --- tfstate export ---

type tfState struct {
	Version          int               `json:"version"`
	TerraformVersion string            `json:"terraform_version"`
	Serial           int               `json:"serial"`
	Lineage          string            `json:"lineage"`
	Outputs          map[string]any    `json:"outputs"`
	Resources        []tfStateResource `json:"resources"`
}

type tfStateResource struct {
	Mode      string            `json:"mode"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Provider  string            `json:"provider"`
	Instances []tfStateInstance `json:"instances"`
}

type tfStateInstance struct {
	SchemaVersion int            `json:"schema_version"`
	Attributes    map[string]any `json:"attributes"`
	Dependencies  []string       `json:"dependencies"`
}

// exportAsTFState converts workflow ResourceStates to Terraform state JSON format.
func exportAsTFState(states []interfaces.ResourceState) ([]byte, error) {
	tf := tfState{
		Version:          4,
		TerraformVersion: "1.0.0 (workflow-wfctl)",
		Serial:           1,
		Lineage:          generateLineage(),
		Outputs:          map[string]any{},
	}

	for i := range states {
		s := &states[i]
		attrs := map[string]any{
			"id":       s.ProviderID,
			"name":     s.Name,
			"provider": s.Provider,
		}
		for k, v := range s.Outputs {
			attrs[k] = v
		}
		tf.Resources = append(tf.Resources, tfStateResource{
			Mode:     "managed",
			Type:     strings.ReplaceAll(s.Type, ".", "_"),
			Name:     s.Name,
			Provider: fmt.Sprintf("registry.terraform.io/workflow/%s", s.Provider),
			Instances: []tfStateInstance{
				{
					SchemaVersion: 0,
					Attributes:    attrs,
					Dependencies:  s.Dependencies,
				},
			},
		})
	}

	return json.MarshalIndent(tf, "", "  ")
}

// --- tfstate import ---

func importFromTFState(srcFile, stateDir string) error {
	data, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcFile, err)
	}

	var tf tfState
	if err := json.Unmarshal(data, &tf); err != nil {
		return fmt.Errorf("parse tfstate: %w", err)
	}

	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	imported := 0
	for _, res := range tf.Resources {
		if res.Mode != "managed" || len(res.Instances) == 0 {
			continue
		}
		inst := res.Instances[0]
		id, _ := inst.Attributes["id"].(string)
		if id == "" {
			id = res.Name
		}
		record := map[string]any{
			"resource_id":   id,
			"resource_type": strings.ReplaceAll(res.Type, "_", "."),
			"provider":      res.Provider,
			"status":        "active",
			"config":        inst.Attributes,
			"outputs":       inst.Attributes,
			"created_at":    time.Now().UTC().Format(time.RFC3339),
			"updated_at":    time.Now().UTC().Format(time.RFC3339),
		}
		out, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			continue
		}
		fname := stateDir + "/" + sanitizeStateID(id) + ".json"
		if err := os.WriteFile(fname, out, 0o600); err != nil {
			return fmt.Errorf("write state record: %w", err)
		}
		imported++
	}
	fmt.Printf("Imported %d resource(s) from %s into %s\n", imported, srcFile, stateDir)
	return nil
}

// --- pulumi import ---

func importFromPulumi(srcFile, stateDir string) error {
	data, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcFile, err)
	}

	// Pulumi checkpoint JSON top-level structure.
	var checkpoint struct {
		Latest struct {
			Resources []struct {
				URN     string         `json:"urn"`
				Type    string         `json:"type"`
				ID      string         `json:"id"`
				Inputs  map[string]any `json:"inputs"`
				Outputs map[string]any `json:"outputs"`
			} `json:"resources"`
		} `json:"latest"`
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return fmt.Errorf("parse pulumi checkpoint: %w", err)
	}

	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	imported := 0
	for _, res := range checkpoint.Latest.Resources {
		if res.ID == "" {
			continue
		}
		// Extract provider from URN: urn:pulumi:stack::project::provider:module:Type::name
		parts := strings.Split(res.Type, ":")
		provider := "unknown"
		if len(parts) > 0 {
			provider = parts[0]
		}
		record := map[string]any{
			"resource_id":   res.ID,
			"resource_type": res.Type,
			"provider":      provider,
			"status":        "active",
			"config":        res.Inputs,
			"outputs":       res.Outputs,
			"created_at":    time.Now().UTC().Format(time.RFC3339),
			"updated_at":    time.Now().UTC().Format(time.RFC3339),
		}
		out, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			continue
		}
		fname := stateDir + "/" + sanitizeStateID(res.ID) + ".json"
		if err := os.WriteFile(fname, out, 0o600); err != nil {
			return fmt.Errorf("write state record: %w", err)
		}
		imported++
	}
	fmt.Printf("Imported %d resource(s) from Pulumi checkpoint %s into %s\n", imported, srcFile, stateDir)
	return nil
}

func sanitizeStateID(id string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_")
	return replacer.Replace(id)
}

func generateLineage() string {
	// Simple deterministic lineage based on time; not cryptographically random.
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
